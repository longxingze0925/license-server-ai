// Package license 数据同步功能
// 支持将本地 SQLite 数据库的表数据同步到云端服务器
package license

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SyncRecord 同步记录
type SyncRecord struct {
	ID        string                 `json:"id"`
	Data      map[string]interface{} `json:"data"`
	Version   int64                  `json:"version"`
	IsDeleted bool                   `json:"is_deleted"`
	UpdatedAt int64                  `json:"updated_at"`
}

// SyncResult 同步结果
type SyncResult struct {
	RecordID      string `json:"record_id"`
	DataKey       string `json:"data_key,omitempty"`
	Status        string `json:"status"` // success, conflict, error
	Version       int64  `json:"version"`
	ServerVersion int64  `json:"server_version,omitempty"`
	ConflictID    string `json:"conflict_id,omitempty"`
	Error         string `json:"error,omitempty"`
	ConflictData  any    `json:"conflict_data,omitempty"`
}

func validateDataSyncIdentifier(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s不能为空", field)
	}
	if len(value) > 100 {
		return fmt.Errorf("%s长度不能超过100字符", field)
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return fmt.Errorf("%s只能包含字母、数字、下划线、点和横线", field)
	}
	return nil
}

// TableInfo 表信息
type TableInfo struct {
	TableName   string `json:"table_name"`
	RecordCount int64  `json:"record_count"`
	LastUpdated string `json:"last_updated"`
}

// 数据类型常量
const (
	DataTypeConfig             = "config"
	DataTypeWorkflow           = "workflow"
	DataTypeBatchTask          = "batch_task"
	DataTypeMaterial           = "material"
	DataTypePost               = "post"
	DataTypeComment            = "comment"
	DataTypeCommentScript      = "comment_script"
	DataTypeVoiceConfig        = "voice_config"
	DataTypeScripts            = "scripts"               // 话术管理
	DataTypeDanmakuGroups      = "danmaku_groups"        // 互动规则
	DataTypeAIConfig           = "ai_config"             // AI配置
	DataTypeRandomWordAIConfig = "random_word_ai_config" // 随机词AI配置
)

// BackupData 备份数据
type BackupData struct {
	ID         string `json:"id"`
	DataType   string `json:"data_type"`
	DataJSON   string `json:"data_json"`
	Version    int    `json:"version"`
	DeviceName string `json:"device_name"`
	MachineID  string `json:"machine_id"`
	IsCurrent  bool   `json:"is_current"`
	DataSize   int64  `json:"data_size"`
	ItemCount  int    `json:"item_count"`
	Checksum   string `json:"checksum"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// DataSyncClient 数据同步客户端
type DataSyncClient struct {
	client       *Client
	lastSyncTime map[string]int64 // 每个表的最后同步时间
}

// NewDataSyncClient 创建数据同步客户端
func (c *Client) NewDataSyncClient() *DataSyncClient {
	return &DataSyncClient{
		client:       c,
		lastSyncTime: make(map[string]int64),
	}
}

// GetTableList 获取服务器上的所有表名
func (d *DataSyncClient) GetTableList() ([]TableInfo, error) {
	data, err := d.getSyncAPIData("/api/client/sync/tables", nil)
	if err != nil {
		return nil, err
	}
	var tables []TableInfo
	if err := json.Unmarshal(data, &tables); err != nil {
		return nil, err
	}
	return tables, nil
}

// PullTable 从服务器拉取指定表的数据
// tableName: 表名
// since: 增量同步时间戳（0表示全量）
func (d *DataSyncClient) PullTable(tableName string, since int64) ([]SyncRecord, int64, error) {
	tableName = strings.TrimSpace(tableName)
	if err := validateDataSyncIdentifier("tableName", tableName); err != nil {
		return nil, 0, err
	}
	params := url.Values{}
	params.Set("table", tableName)
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}

	data, err := d.getSyncAPIData("/api/client/sync/table", params)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Table      string       `json:"table"`
		Records    []SyncRecord `json:"records"`
		Count      int          `json:"count"`
		ServerTime int64        `json:"server_time"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, err
	}

	// 更新最后同步时间
	d.lastSyncTime[tableName] = result.ServerTime

	return result.Records, result.ServerTime, nil
}

// PullAllTables 从服务器拉取所有表的数据
// since: 增量同步时间戳（0表示全量）
func (d *DataSyncClient) PullAllTables(since int64) (map[string][]SyncRecord, int64, error) {
	params := url.Values{}
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}

	data, err := d.getSyncAPIData("/api/client/sync/tables/all", params)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Tables     map[string][]SyncRecord `json:"tables"`
		ServerTime int64                   `json:"server_time"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, err
	}

	return result.Tables, result.ServerTime, nil
}

// PushRecord 推送单条记录到服务器
func (d *DataSyncClient) PushRecord(tableName, recordID string, data map[string]interface{}, version int64) (*SyncResult, error) {
	tableName = strings.TrimSpace(tableName)
	recordID = strings.TrimSpace(recordID)
	if err := validateDataSyncIdentifier("tableName", tableName); err != nil {
		return nil, err
	}
	if err := validateDataSyncIdentifier("recordID", recordID); err != nil {
		return nil, err
	}
	reqBody := map[string]interface{}{
		"table":     tableName,
		"record_id": recordID,
		"data":      data,
		"version":   version,
	}

	resultData, err := d.postSyncAPIData("/api/client/sync/table", reqBody)
	if err != nil {
		return nil, err
	}
	var result struct {
		Status        string `json:"status"`
		Version       int64  `json:"version"`
		ServerVersion int64  `json:"server_version,omitempty"`
		ServerData    string `json:"server_data,omitempty"`
	}
	if err := json.Unmarshal(resultData, &result); err != nil {
		return nil, err
	}

	return &SyncResult{
		RecordID:      recordID,
		Status:        result.Status,
		Version:       result.Version,
		ServerVersion: result.ServerVersion,
	}, nil
}

// PushRecordBatch 批量推送记录到服务器
type PushRecordItem struct {
	RecordID string                 `json:"record_id"`
	Data     map[string]interface{} `json:"data"`
	Version  int64                  `json:"version"`
	Deleted  bool                   `json:"deleted"`
}

func (d *DataSyncClient) PushRecordBatch(tableName string, records []PushRecordItem) ([]SyncResult, error) {
	tableName = strings.TrimSpace(tableName)
	if err := validateDataSyncIdentifier("tableName", tableName); err != nil {
		return nil, err
	}
	for i := range records {
		records[i].RecordID = strings.TrimSpace(records[i].RecordID)
		if err := validateDataSyncIdentifier("recordID", records[i].RecordID); err != nil {
			return nil, err
		}
	}
	reqBody := map[string]interface{}{
		"table":   tableName,
		"records": records,
	}

	resultData, err := d.postSyncAPIData("/api/client/sync/table/batch", reqBody)
	if err != nil {
		return nil, err
	}
	var result struct {
		Table      string       `json:"table"`
		Results    []SyncResult `json:"results"`
		Count      int          `json:"count"`
		ServerTime int64        `json:"server_time"`
	}
	if err := json.Unmarshal(resultData, &result); err != nil {
		return nil, err
	}

	return result.Results, nil
}

// DeleteRecord 删除服务器上的记录
func (d *DataSyncClient) DeleteRecord(tableName, recordID string) error {
	tableName = strings.TrimSpace(tableName)
	recordID = strings.TrimSpace(recordID)
	if err := validateDataSyncIdentifier("tableName", tableName); err != nil {
		return err
	}
	if err := validateDataSyncIdentifier("recordID", recordID); err != nil {
		return err
	}
	reqBody := map[string]interface{}{
		"table":     tableName,
		"record_id": recordID,
	}

	_, err := d.requestSyncAPIData(http.MethodDelete, "/api/client/sync/table", nil, reqBody)
	if err != nil {
		return err
	}
	return nil
}

// GetLastSyncTime 获取指定表的最后同步时间
func (d *DataSyncClient) GetLastSyncTime(tableName string) int64 {
	tableName = strings.TrimSpace(tableName)
	if err := validateDataSyncIdentifier("tableName", tableName); err != nil {
		return 0
	}
	return d.lastSyncTime[tableName]
}

// SetLastSyncTime 设置指定表的最后同步时间
func (d *DataSyncClient) SetLastSyncTime(tableName string, t int64) {
	tableName = strings.TrimSpace(tableName)
	if err := validateDataSyncIdentifier("tableName", tableName); err != nil {
		return
	}
	d.lastSyncTime[tableName] = t
}

// ==================== 便捷同步方法 ====================

// SyncTableToServer 将本地表数据同步到服务器
// 传入表名和记录列表，自动处理推送
func (d *DataSyncClient) SyncTableToServer(tableName string, records []map[string]interface{}, idField string) ([]SyncResult, error) {
	items := make([]PushRecordItem, 0, len(records))
	for _, record := range records {
		recordID := ""
		if id, ok := record[idField]; ok {
			recordID = fmt.Sprintf("%v", id)
		}
		if recordID == "" {
			continue
		}
		items = append(items, PushRecordItem{
			RecordID: recordID,
			Data:     record,
			Version:  0, // 不检查版本冲突
			Deleted:  false,
		})
	}

	if len(items) == 0 {
		return nil, nil
	}

	return d.PushRecordBatch(tableName, items)
}

// SyncTableFromServer 从服务器同步表数据到本地
// 返回需要更新/插入的记录和需要删除的记录ID
func (d *DataSyncClient) SyncTableFromServer(tableName string, since int64) (updates []SyncRecord, deletes []string, serverTime int64, err error) {
	records, serverTime, err := d.PullTable(tableName, since)
	if err != nil {
		return nil, nil, 0, err
	}

	updates = make([]SyncRecord, 0)
	deletes = make([]string, 0)

	for _, r := range records {
		if r.IsDeleted {
			deletes = append(deletes, r.ID)
		} else {
			updates = append(updates, r)
		}
	}

	return updates, deletes, serverTime, nil
}

// ==================== SQLite 辅助方法 ====================

// SQLiteRecord 从 SQLite 查询结果转换的记录
type SQLiteRecord struct {
	ID   interface{}            `json:"id"`
	Data map[string]interface{} `json:"data"`
}

// ConvertSQLiteRows 将 SQLite 查询结果转换为同步记录格式
// 需要传入列名列表和行数据
func ConvertSQLiteRows(columns []string, rows [][]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		record := make(map[string]interface{})
		for i, col := range columns {
			if i < len(row) {
				record[col] = row[i]
			}
		}
		result = append(result, record)
	}
	return result
}

// ApplySyncRecordToMap 将同步记录转换为 map（用于插入/更新本地数据库）
func ApplySyncRecordToMap(record SyncRecord) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range record.Data {
		result[k] = v
	}
	return result
}

// ==================== 自动同步管理器 ====================

// AutoSyncManager 自动同步管理器
type AutoSyncManager struct {
	syncClient   *DataSyncClient
	tables       []string
	interval     time.Duration
	stopChan     chan struct{}
	mu           sync.Mutex
	running      bool
	onPull       func(tableName string, records []SyncRecord, deletes []string) error
	onConflict   func(tableName string, result SyncResult) error
	lastSyncTime map[string]int64
}

// NewAutoSyncManager 创建自动同步管理器
func (d *DataSyncClient) NewAutoSyncManager(tables []string, interval time.Duration) *AutoSyncManager {
	return &AutoSyncManager{
		syncClient:   d,
		tables:       tables,
		interval:     interval,
		stopChan:     make(chan struct{}),
		lastSyncTime: make(map[string]int64),
	}
}

// OnPull 设置拉取数据回调
func (m *AutoSyncManager) OnPull(fn func(tableName string, records []SyncRecord, deletes []string) error) {
	m.onPull = fn
}

// OnConflict 设置冲突处理回调
func (m *AutoSyncManager) OnConflict(fn func(tableName string, result SyncResult) error) {
	m.onConflict = fn
}

// Start 启动自动同步
func (m *AutoSyncManager) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	m.stopChan = stopCh
	m.running = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			if m.stopChan == stopCh {
				m.running = false
			}
			m.mu.Unlock()
		}()

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		// 立即执行一次同步
		m.syncAll()

		for {
			select {
			case <-ticker.C:
				m.syncAll()
			case <-stopCh:
				return
			}
		}
	}()
}

// Stop 停止自动同步
func (m *AutoSyncManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	close(m.stopChan)
	m.running = false
}

// syncAll 同步所有表
func (m *AutoSyncManager) syncAll() {
	for _, tableName := range m.tables {
		since := m.lastSyncTime[tableName]
		updates, deletes, serverTime, err := m.syncClient.SyncTableFromServer(tableName, since)
		if err != nil {
			continue
		}

		if m.onPull != nil && (len(updates) > 0 || len(deletes) > 0) {
			if err := m.onPull(tableName, updates, deletes); err != nil {
				continue
			}
		}

		m.lastSyncTime[tableName] = serverTime
	}
}

// SyncNow 立即同步
func (m *AutoSyncManager) SyncNow() {
	m.syncAll()
}

// ==================== 高级数据同步功能 ====================

// SyncChange 同步变更记录
type SyncChange struct {
	ID           string                 `json:"id"`
	DataType     string                 `json:"data_type,omitempty"`
	DataKey      string                 `json:"data_key,omitempty"`
	Action       string                 `json:"action,omitempty"` // create, update, delete
	Table        string                 `json:"table,omitempty"`  // 兼容旧字段，等同于 data_type
	RecordID     string                 `json:"record_id,omitempty"`
	Operation    string                 `json:"operation,omitempty"` // 兼容旧字段: insert, update, delete
	Data         map[string]interface{} `json:"data"`
	Version      int64                  `json:"version"`
	LocalVersion int64                  `json:"local_version,omitempty"`
	UpdatedAt    int64                  `json:"updated_at,omitempty"`
	ChangeTime   int64                  `json:"change_time"`
}

// SyncStatus 同步状态
type SyncStatus struct {
	LastSyncTime   int64            `json:"last_sync_time"`
	PendingChanges int              `json:"pending_changes"`
	TableStatus    map[string]int64 `json:"table_status"` // 每个表的最后同步时间
	ServerTime     int64            `json:"server_time"`
}

// ConflictInfo 冲突信息
type ConflictInfo struct {
	Table         string                 `json:"table"`
	RecordID      string                 `json:"record_id"`
	LocalData     map[string]interface{} `json:"local_data"`
	ServerData    map[string]interface{} `json:"server_data"`
	LocalVersion  int64                  `json:"local_version"`
	ServerVersion int64                  `json:"server_version"`
	ConflictTime  int64                  `json:"conflict_time"`
}

// ConflictResolution 冲突解决策略
type ConflictResolution string

const (
	// UseLocal 使用本地数据
	UseLocal ConflictResolution = "use_local"
	// UseServer 使用服务器数据
	UseServer ConflictResolution = "use_server"
	// Merge 合并数据
	Merge ConflictResolution = "merge"
)

type pushChangeItem struct {
	DataType     string                 `json:"data_type"`
	DataKey      string                 `json:"data_key"`
	Action       string                 `json:"action"`
	Data         map[string]interface{} `json:"data"`
	LocalVersion int64                  `json:"local_version"`
}

func (c SyncChange) toPushItem() pushChangeItem {
	dataType := c.DataType
	if dataType == "" {
		dataType = c.Table
	}

	dataKey := c.DataKey
	if dataKey == "" {
		dataKey = c.RecordID
	}
	if dataKey == "" {
		dataKey = c.ID
	}

	action := c.Action
	if action == "" {
		switch c.Operation {
		case "insert", "create":
			action = "create"
		case "delete":
			action = "delete"
		default:
			action = "update"
		}
	}

	localVersion := c.LocalVersion
	if localVersion == 0 {
		localVersion = c.Version
	}

	return pushChangeItem{
		DataType:     dataType,
		DataKey:      dataKey,
		Action:       action,
		Data:         c.Data,
		LocalVersion: localVersion,
	}
}

func normalizeSyncResults(results []SyncResult) []SyncResult {
	for i := range results {
		if results[i].RecordID == "" {
			results[i].RecordID = results[i].DataKey
		}
		if results[i].DataKey == "" {
			results[i].DataKey = results[i].RecordID
		}
		if results[i].Version == 0 {
			results[i].Version = results[i].ServerVersion
		}
	}
	return results
}

// PushChanges 推送客户端变更到服务端（Push）
// changes: 变更列表
func (d *DataSyncClient) PushChanges(changes []SyncChange) ([]SyncResult, error) {
	items := make([]pushChangeItem, 0, len(changes))
	for _, change := range changes {
		items = append(items, change.toPushItem())
	}

	reqBody := map[string]interface{}{
		"items": items,
	}

	data, err := d.postSyncAPIData("/api/client/sync/push", reqBody)
	if err != nil {
		return nil, err
	}
	var result struct {
		Results    []SyncResult `json:"results"`
		ServerTime int64        `json:"server_time"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return normalizeSyncResults(result.Results), nil
}

// GetChanges 获取服务端变更（Pull）
// since: 从指定时间戳开始获取变更
// dataTypes: 指定要获取的数据类型（为空则获取所有支持的数据类型）
func (d *DataSyncClient) GetChanges(since int64, dataTypes []string) ([]SyncChange, int64, error) {
	if len(dataTypes) > 1 {
		allChanges := make([]SyncChange, 0)
		var latestServerTime int64
		for _, dataType := range dataTypes {
			page, err := d.getChangesByType(since, dataType, 0, 0)
			if err != nil {
				return nil, 0, err
			}
			allChanges = append(allChanges, page.Changes...)
			if page.ServerTime > latestServerTime {
				latestServerTime = page.ServerTime
			}
		}
		return allChanges, latestServerTime, nil
	}

	dataType := ""
	if len(dataTypes) == 1 {
		dataType = dataTypes[0]
	}
	page, err := d.getChangesByType(since, dataType, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	return page.Changes, page.ServerTime, nil
}

type SyncChangesPage struct {
	Changes    []SyncChange `json:"changes"`
	ServerTime int64        `json:"server_time"`
	HasMore    bool         `json:"has_more"`
	NextOffset int          `json:"next_offset"`
	Limit      int          `json:"limit"`
	Offset     int          `json:"offset"`
}

func (d *DataSyncClient) GetChangesPage(since int64, dataType string, limit, offset int) (*SyncChangesPage, error) {
	return d.getChangesPageByType(since, dataType, limit, offset)
}

func (d *DataSyncClient) getChangesByType(since int64, dataType string, limit, offset int) (*SyncChangesPage, error) {
	allChanges := make([]SyncChange, 0)
	var serverTime int64
	currentOffset := offset
	for {
		page, err := d.getChangesPageByType(since, dataType, limit, currentOffset)
		if err != nil {
			return nil, err
		}
		allChanges = append(allChanges, page.Changes...)
		if page.ServerTime > serverTime {
			serverTime = page.ServerTime
		}
		if limit > 0 || !page.HasMore {
			page.Changes = allChanges
			page.ServerTime = serverTime
			return page, nil
		}
		if page.NextOffset <= currentOffset {
			return nil, fmt.Errorf("服务端返回了无效的 next_offset: %d", page.NextOffset)
		}
		currentOffset = page.NextOffset
	}
}

func (d *DataSyncClient) getChangesPageByType(since int64, dataType string, limit, offset int) (*SyncChangesPage, error) {
	params := url.Values{}
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}
	if dataType != "" {
		params.Set("data_type", dataType)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}

	data, err := d.getSyncAPIData("/api/client/sync/changes", params)
	if err != nil {
		return nil, err
	}
	var result struct {
		Changes []struct {
			DataType  string          `json:"data_type"`
			DataKey   string          `json:"data_key"`
			Action    string          `json:"action"`
			Data      json.RawMessage `json:"data"`
			Version   int64           `json:"version"`
			UpdatedAt int64           `json:"updated_at"`
		} `json:"changes"`
		ServerTime int64 `json:"server_time"`
		HasMore    bool  `json:"has_more"`
		NextOffset int   `json:"next_offset"`
		Limit      int   `json:"limit"`
		Offset     int   `json:"offset"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	changes := make([]SyncChange, 0, len(result.Changes))
	for _, item := range result.Changes {
		data := make(map[string]interface{})
		if len(item.Data) > 0 {
			if err := json.Unmarshal(item.Data, &data); err != nil {
				var value interface{}
				if err := json.Unmarshal(item.Data, &value); err == nil {
					data["value"] = value
				} else {
					data["value"] = string(item.Data)
				}
			}
		}
		changes = append(changes, SyncChange{
			ID:         item.DataKey,
			DataType:   item.DataType,
			DataKey:    item.DataKey,
			Action:     item.Action,
			Table:      item.DataType,
			RecordID:   item.DataKey,
			Operation:  item.Action,
			Data:       data,
			Version:    item.Version,
			UpdatedAt:  item.UpdatedAt,
			ChangeTime: item.UpdatedAt,
		})
	}

	return &SyncChangesPage{
		Changes:    changes,
		ServerTime: result.ServerTime,
		HasMore:    result.HasMore,
		NextOffset: result.NextOffset,
		Limit:      result.Limit,
		Offset:     result.Offset,
	}, nil
}

// GetSyncStatus 获取同步状态
func (d *DataSyncClient) GetSyncStatus() (*SyncStatus, error) {
	data, err := d.getSyncAPIData("/api/client/sync/status", nil)
	if err != nil {
		return nil, err
	}
	var result SyncStatus
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ResolveConflict 解决数据冲突
// conflictID: PushChanges 返回的 conflict_id
// resolution: 解决策略 (use_local, use_server, merge)
// mergedData: 当策略为 merge 时，提供合并后的数据
func (d *DataSyncClient) ResolveConflict(conflictID string, resolution ConflictResolution, mergedData map[string]interface{}) (*SyncResult, error) {
	reqBody := map[string]interface{}{
		"conflict_id": conflictID,
		"resolution":  string(resolution),
	}
	if mergedData != nil {
		reqBody["merged_data"] = mergedData
	}

	_, err := d.postSyncAPIData("/api/client/sync/conflict/resolve", reqBody)
	if err != nil {
		return nil, err
	}

	return &SyncResult{
		ConflictID: conflictID,
		Status:     "resolved",
	}, nil
}

// ==================== 分类数据同步功能 ====================

// ConfigData 配置数据
type ConfigData struct {
	Key       string      `json:"key"`
	Value     interface{} `json:"value"`
	Version   int64       `json:"version,omitempty"`
	UpdatedAt int64       `json:"updated_at"`
}

// WorkflowData 工作流数据
type WorkflowData struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Config    map[string]interface{} `json:"config"`
	Enabled   bool                   `json:"enabled"`
	Version   int64                  `json:"version,omitempty"`
	UpdatedAt int64                  `json:"updated_at"`
}

// MaterialData 素材数据
type MaterialData struct {
	ID         string `json:"id"`
	MaterialID int64  `json:"material_id,omitempty"`
	Type       string `json:"type"`
	Content    string `json:"content"`
	Tags       string `json:"tags"`
	Version    int64  `json:"version,omitempty"`
	UpdatedAt  int64  `json:"updated_at"`
}

// PostData 帖子数据
type PostData struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name,omitempty"`
	PostType  string `json:"post_type,omitempty"`
	PostLink  string `json:"post_link,omitempty"`
	Version   int64  `json:"version,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

// CommentScriptData 评论话术数据
type CommentScriptData struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	Version   int64  `json:"version,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

// VoiceConfigData TTS声音配置数据
type VoiceConfigData struct {
	ID           int64   `json:"id"`
	Role         string  `json:"role"`           // 角色标识
	Name         string  `json:"name"`           // 配置名称
	GPTPath      string  `json:"gpt_path"`       // GPT模型路径
	SoVITSPath   string  `json:"sovits_path"`    // SoVITS模型路径
	RefAudioPath string  `json:"ref_audio_path"` // 参考音频路径
	RefText      string  `json:"ref_text"`       // 参考文本
	Language     string  `json:"language"`       // 语言
	SpeedFactor  float64 `json:"speed_factor"`   // 语速因子
	TTSVersion   int     `json:"tts_version"`    // TTS版本: 1=v1, 2=v2, 3=v3, 4=v4, 5=v2Pro, 6=v2ProPlus
	Enabled      bool    `json:"enabled"`        // 是否启用
	Version      int64   `json:"version,omitempty"`
	UpdatedAt    int64   `json:"updated_at"`
}

// GetConfigs 获取配置数据
func (d *DataSyncClient) GetConfigs(since int64) ([]ConfigData, int64, error) {
	params := url.Values{}
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}

	data, err := d.getSyncAPIData("/api/client/sync/configs", params)
	if err != nil {
		return nil, 0, err
	}

	var listResponse struct {
		Configs    []ConfigData `json:"configs"`
		ServerTime int64        `json:"server_time"`
	}
	if err := json.Unmarshal(data, &listResponse); err == nil && listResponse.Configs != nil {
		return listResponse.Configs, listResponse.ServerTime, nil
	}

	var configMap map[string]struct {
		Value     interface{} `json:"value"`
		Version   int64       `json:"version"`
		UpdatedAt int64       `json:"updated_at"`
	}
	if err := json.Unmarshal(data, &configMap); err != nil {
		return nil, 0, err
	}

	configs := make([]ConfigData, 0, len(configMap))
	var serverTime int64
	for key, cfg := range configMap {
		configs = append(configs, ConfigData{
			Key:       key,
			Value:     cfg.Value,
			Version:   cfg.Version,
			UpdatedAt: cfg.UpdatedAt,
		})
		if cfg.UpdatedAt > serverTime {
			serverTime = cfg.UpdatedAt
		}
	}

	return configs, serverTime, nil
}

// SaveConfigs 保存配置数据
func (d *DataSyncClient) SaveConfigs(configs []ConfigData) error {
	for _, cfg := range configs {
		reqBody := map[string]interface{}{
			"config_key": cfg.Key,
			"value":      cfg.Value,
			"version":    cfg.Version,
		}

		data, err := d.postSyncAPIData("/api/client/sync/configs", reqBody)
		if err != nil {
			return err
		}

		var result struct {
			Status     string `json:"status"`
			ConflictID string `json:"conflict_id,omitempty"`
			Error      string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}
		if result.Status != "" && result.Status != "success" {
			if result.Error != "" {
				return fmt.Errorf("保存配置 %s 失败: %s", cfg.Key, result.Error)
			}
			if result.ConflictID != "" {
				return fmt.Errorf("保存配置 %s 冲突: %s", cfg.Key, result.ConflictID)
			}
			return fmt.Errorf("保存配置 %s 失败: %s", cfg.Key, result.Status)
		}
	}

	return nil
}

type workflowServerData struct {
	ID           string    `json:"id"`
	WorkflowID   string    `json:"WorkflowID"`
	WorkflowName string    `json:"WorkflowName"`
	Steps        string    `json:"Steps"`
	Status       string    `json:"Status"`
	Version      int64     `json:"Version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type materialServerData struct {
	ID         string    `json:"id"`
	MaterialID int64     `json:"MaterialID"`
	FileName   string    `json:"FileName"`
	FileType   string    `json:"FileType"`
	Caption    string    `json:"Caption"`
	GroupName  string    `json:"GroupName"`
	Status     string    `json:"Status"`
	Version    int64     `json:"Version"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type postServerData struct {
	ID        string    `json:"id"`
	PostType  string    `json:"PostType"`
	GroupName string    `json:"GroupName"`
	PostLink  string    `json:"PostLink"`
	Caption   string    `json:"Caption"`
	Status    string    `json:"Status"`
	Version   int64     `json:"Version"`
	UpdatedAt time.Time `json:"updated_at"`
}

type commentScriptServerData struct {
	ID        string    `json:"id"`
	GroupName string    `json:"GroupName"`
	Content   string    `json:"Content"`
	Status    string    `json:"Status"`
	Version   int64     `json:"Version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func decodeAPIData(body []byte) (json.RawMessage, error) {
	var result struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("API error: %s", result.Message)
	}
	return result.Data, nil
}

func (d *DataSyncClient) requestSyncAPIData(method, path string, params url.Values, reqBody interface{}) (json.RawMessage, error) {
	doRequest := func() (json.RawMessage, error) {
		accessToken, _, _, _, _ := d.client.getSessionSnapshot()
		if accessToken == "" {
			return nil, fmt.Errorf("请先激活或登录")
		}

		fullURL := d.client.serverURL + path
		if params != nil && len(params) > 0 {
			fullURL += "?" + params.Encode()
		}

		var body io.Reader
		if reqBody != nil {
			jsonBody, err := json.Marshal(reqBody)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(jsonBody)
		}

		req, err := http.NewRequest(method, fullURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := d.client.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("API error: 认证失败")
		}
		data, err := decodeAPIData(respBody)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("API error: HTTP %d", resp.StatusCode)
		}
		return data, nil
	}

	data, err := doRequest()
	if err != nil && shouldRetryWithRefresh(err) && d.client.refreshClientSession() {
		return doRequest()
	}
	return data, err
}

func (d *DataSyncClient) syncParams() url.Values {
	return url.Values{}
}

func (d *DataSyncClient) getSyncAPIData(path string, params url.Values) (json.RawMessage, error) {
	if params == nil {
		params = d.syncParams()
	}
	return d.requestSyncAPIData(http.MethodGet, path, params, nil)
}

func (d *DataSyncClient) postSyncAPIData(path string, reqBody map[string]interface{}) (json.RawMessage, error) {
	return d.requestSyncAPIData(http.MethodPost, path, nil, reqBody)
}

func (d *DataSyncClient) putSyncAPIData(path string, reqBody map[string]interface{}) (json.RawMessage, error) {
	return d.requestSyncAPIData(http.MethodPut, path, nil, reqBody)
}

func (d *DataSyncClient) deleteSyncAPIData(path string, params url.Values) (json.RawMessage, error) {
	if params == nil {
		params = d.syncParams()
	}
	return d.requestSyncAPIData(http.MethodDelete, path, params, nil)
}

func decodeSyncResults(data json.RawMessage) ([]SyncResult, error) {
	var one SyncResult
	if err := json.Unmarshal(data, &one); err == nil && (one.Status != "" || one.DataKey != "" || one.RecordID != "") {
		return normalizeSyncResults([]SyncResult{one}), nil
	}

	var batch struct {
		Results []SyncResult `json:"results"`
	}
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, err
	}
	return normalizeSyncResults(batch.Results), nil
}

func checkSyncResults(action string, results []SyncResult) error {
	if len(results) == 0 {
		return fmt.Errorf("%s失败: 服务端没有返回同步结果", action)
	}
	for _, result := range results {
		if result.Status == "" || result.Status == "success" {
			continue
		}
		if result.Status == "conflict" {
			if result.ConflictID != "" {
				return fmt.Errorf("%s冲突: %s", action, result.ConflictID)
			}
			return fmt.Errorf("%s冲突", action)
		}
		if result.Error != "" {
			return fmt.Errorf("%s失败: %s", action, result.Error)
		}
		return fmt.Errorf("%s失败: %s", action, result.Status)
	}
	return nil
}

func syncRFC3339(ts int64) string {
	if ts > 0 {
		return time.Unix(ts, 0).UTC().Format(time.RFC3339Nano)
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func unixTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func maxInt64(current, value int64) int64 {
	if value > current {
		return value
	}
	return current
}

func parseWorkflowConfig(steps string) map[string]interface{} {
	if steps == "" {
		return map[string]interface{}{}
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(steps), &config); err == nil && config != nil {
		return config
	}
	var value interface{}
	if err := json.Unmarshal([]byte(steps), &value); err == nil {
		return map[string]interface{}{"steps": value}
	}
	return map[string]interface{}{"steps": steps}
}

func workflowToServerData(workflow WorkflowData) map[string]interface{} {
	status := "active"
	if !workflow.Enabled {
		status = "disabled"
	}
	steps, _ := json.Marshal(workflow.Config)
	if len(steps) == 0 || string(steps) == "null" {
		steps = []byte("{}")
	}
	return map[string]interface{}{
		"WorkflowID":   workflow.ID,
		"WorkflowName": workflow.Name,
		"Steps":        string(steps),
		"Status":       status,
		"Version":      workflow.Version,
		"create_time":  syncRFC3339(workflow.UpdatedAt),
	}
}

func workflowFromServerData(workflow workflowServerData) WorkflowData {
	return WorkflowData{
		ID:        workflow.WorkflowID,
		Name:      workflow.WorkflowName,
		Config:    parseWorkflowConfig(workflow.Steps),
		Enabled:   workflow.Status != "disabled",
		Version:   workflow.Version,
		UpdatedAt: unixTime(workflow.UpdatedAt),
	}
}

func stableInt64ID(value string) int64 {
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
		return parsed
	}
	if value == "" {
		value = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

func materialToServerData(material MaterialData) map[string]interface{} {
	materialID := material.MaterialID
	if materialID == 0 {
		materialID = stableInt64ID(material.ID)
	}
	status := "未使用"
	return map[string]interface{}{
		"MaterialID": materialID,
		"FileName":   material.Content,
		"FileType":   material.Type,
		"Caption":    material.Content,
		"GroupName":  material.Tags,
		"Status":     status,
		"Version":    material.Version,
	}
}

func materialFromServerData(material materialServerData) MaterialData {
	content := material.Caption
	if content == "" {
		content = material.FileName
	}
	id := strconv.FormatInt(material.MaterialID, 10)
	if material.MaterialID == 0 {
		id = material.ID
	}
	return MaterialData{
		ID:         id,
		MaterialID: material.MaterialID,
		Type:       material.FileType,
		Content:    content,
		Tags:       material.GroupName,
		Version:    material.Version,
		UpdatedAt:  unixTime(material.UpdatedAt),
	}
}

func postToServerData(post PostData) map[string]interface{} {
	groupName := post.GroupName
	if groupName == "" {
		groupName = post.GroupID
	}
	if groupName == "" {
		groupName = "default"
	}
	postType := post.PostType
	if postType == "" {
		postType = "user_posts"
	}
	postLink := post.PostLink
	if postLink == "" {
		postLink = post.ID
	}
	if postLink == "" {
		postLink = "local://" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	status := post.Status
	if status == "" {
		status = "unused"
	}
	return map[string]interface{}{
		"id":           post.ID,
		"PostType":     postType,
		"GroupName":    groupName,
		"PostLink":     postLink,
		"Caption":      post.Content,
		"Status":       status,
		"Version":      post.Version,
		"collected_at": syncRFC3339(post.UpdatedAt),
	}
}

func postFromServerData(post postServerData) PostData {
	return PostData{
		ID:        post.ID,
		Content:   post.Caption,
		Status:    post.Status,
		GroupID:   post.GroupName,
		GroupName: post.GroupName,
		PostType:  post.PostType,
		PostLink:  post.PostLink,
		Version:   post.Version,
		UpdatedAt: unixTime(post.UpdatedAt),
	}
}

func commentScriptToServerData(script CommentScriptData) map[string]interface{} {
	status := "active"
	return map[string]interface{}{
		"id":        script.ID,
		"GroupName": script.Category,
		"Content":   script.Content,
		"Status":    status,
		"Version":   script.Version,
	}
}

func commentScriptFromServerData(script commentScriptServerData) CommentScriptData {
	return CommentScriptData{
		ID:        script.ID,
		Content:   script.Content,
		Category:  script.GroupName,
		Version:   script.Version,
		UpdatedAt: unixTime(script.UpdatedAt),
	}
}

// GetWorkflows 获取工作流数据
func (d *DataSyncClient) GetWorkflows(since int64) ([]WorkflowData, int64, error) {
	params := d.syncParams()
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}

	data, err := d.getSyncAPIData("/api/client/sync/workflows", params)
	if err != nil {
		return nil, 0, err
	}

	var serverItems []workflowServerData
	var serverTime int64
	if err := json.Unmarshal(data, &serverItems); err != nil {
		var wrapped struct {
			Workflows  []workflowServerData `json:"workflows"`
			ServerTime int64                `json:"server_time"`
		}
		if wrapErr := json.Unmarshal(data, &wrapped); wrapErr != nil {
			return nil, 0, err
		}
		serverItems = wrapped.Workflows
		serverTime = wrapped.ServerTime
	}

	workflows := make([]WorkflowData, 0, len(serverItems))
	for _, item := range serverItems {
		workflows = append(workflows, workflowFromServerData(item))
		serverTime = maxInt64(serverTime, unixTime(item.UpdatedAt))
	}
	return workflows, serverTime, nil
}

// SaveWorkflows 保存工作流数据
func (d *DataSyncClient) SaveWorkflows(workflows []WorkflowData) error {
	for _, workflow := range workflows {
		data, err := d.postSyncAPIData("/api/client/sync/workflows", map[string]interface{}{
			"workflow": workflowToServerData(workflow),
			"version":  workflow.Version,
		})
		if err != nil {
			return err
		}
		results, err := decodeSyncResults(data)
		if err != nil {
			return err
		}
		if err := checkSyncResults("保存工作流 "+workflow.ID, results); err != nil {
			return err
		}
	}
	return nil
}

// DeleteWorkflow 删除工作流
func (d *DataSyncClient) DeleteWorkflow(workflowID string) error {
	data, err := d.deleteSyncAPIData("/api/client/sync/workflows/"+workflowID, nil)
	if err != nil {
		return err
	}
	results, err := decodeSyncResults(data)
	if err != nil {
		return err
	}
	return checkSyncResults("删除工作流 "+workflowID, results)
}

// GetMaterials 获取素材数据
func (d *DataSyncClient) GetMaterials(since int64) ([]MaterialData, int64, error) {
	params := d.syncParams()
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}

	data, err := d.getSyncAPIData("/api/client/sync/materials", params)
	if err != nil {
		return nil, 0, err
	}

	var serverItems []materialServerData
	var serverTime int64
	if err := json.Unmarshal(data, &serverItems); err != nil {
		var wrapped struct {
			Materials  []materialServerData `json:"materials"`
			ServerTime int64                `json:"server_time"`
		}
		if wrapErr := json.Unmarshal(data, &wrapped); wrapErr != nil {
			return nil, 0, err
		}
		serverItems = wrapped.Materials
		serverTime = wrapped.ServerTime
	}

	materials := make([]MaterialData, 0, len(serverItems))
	for _, item := range serverItems {
		materials = append(materials, materialFromServerData(item))
		serverTime = maxInt64(serverTime, unixTime(item.UpdatedAt))
	}
	return materials, serverTime, nil
}

// SaveMaterials 保存素材数据
func (d *DataSyncClient) SaveMaterials(materials []MaterialData) error {
	if len(materials) == 0 {
		return nil
	}
	serverItems := make([]map[string]interface{}, 0, len(materials))
	for _, material := range materials {
		serverItems = append(serverItems, materialToServerData(material))
	}
	data, err := d.postSyncAPIData("/api/client/sync/materials/batch", map[string]interface{}{
		"materials": serverItems,
	})
	if err != nil {
		return err
	}
	results, err := decodeSyncResults(data)
	if err != nil {
		return err
	}
	return checkSyncResults("保存素材", results)
}

// GetPosts 获取帖子数据
func (d *DataSyncClient) GetPosts(since int64, groupID string) ([]PostData, int64, error) {
	params := d.syncParams()
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}
	if groupID != "" {
		params.Set("group", groupID)
	}

	data, err := d.getSyncAPIData("/api/client/sync/posts", params)
	if err != nil {
		return nil, 0, err
	}

	var serverItems []postServerData
	var serverTime int64
	if err := json.Unmarshal(data, &serverItems); err != nil {
		var wrapped struct {
			List       []postServerData `json:"list"`
			Posts      []postServerData `json:"posts"`
			ServerTime int64            `json:"server_time"`
		}
		if wrapErr := json.Unmarshal(data, &wrapped); wrapErr != nil {
			return nil, 0, err
		}
		serverItems = wrapped.List
		if serverItems == nil {
			serverItems = wrapped.Posts
		}
		serverTime = wrapped.ServerTime
	}

	posts := make([]PostData, 0, len(serverItems))
	for _, item := range serverItems {
		posts = append(posts, postFromServerData(item))
		serverTime = maxInt64(serverTime, unixTime(item.UpdatedAt))
	}
	return posts, serverTime, nil
}

// SavePosts 批量保存帖子数据
func (d *DataSyncClient) SavePosts(posts []PostData) error {
	if len(posts) == 0 {
		return nil
	}
	serverItems := make([]map[string]interface{}, 0, len(posts))
	for _, post := range posts {
		serverItems = append(serverItems, postToServerData(post))
	}
	data, err := d.postSyncAPIData("/api/client/sync/posts/batch", map[string]interface{}{
		"posts": serverItems,
	})
	if err != nil {
		return err
	}
	results, err := decodeSyncResults(data)
	if err != nil {
		return err
	}
	return checkSyncResults("保存帖子", results)
}

// UpdatePostStatus 更新帖子状态
func (d *DataSyncClient) UpdatePostStatus(postID, status string) error {
	_, err := d.putSyncAPIData("/api/client/sync/posts/"+postID+"/status", map[string]interface{}{
		"status": status,
	})
	return err
}

// PostGroup 帖子分组
type PostGroup struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// GetPostGroups 获取帖子分组
func (d *DataSyncClient) GetPostGroups() ([]PostGroup, error) {
	data, err := d.getSyncAPIData("/api/client/sync/posts/groups", d.syncParams())
	if err != nil {
		return nil, err
	}

	var serverGroups []struct {
		GroupName  string `json:"group_name"`
		PostType   string `json:"post_type"`
		TotalCount int    `json:"total_count"`
	}
	if err := json.Unmarshal(data, &serverGroups); err != nil {
		var wrapped struct {
			Groups []PostGroup `json:"groups"`
		}
		if wrapErr := json.Unmarshal(data, &wrapped); wrapErr != nil {
			return nil, err
		}
		return wrapped.Groups, nil
	}

	groups := make([]PostGroup, 0, len(serverGroups))
	for _, group := range serverGroups {
		groups = append(groups, PostGroup{
			ID:    group.GroupName,
			Name:  group.GroupName,
			Count: group.TotalCount,
		})
	}
	return groups, nil
}

// GetCommentScripts 获取评论话术
func (d *DataSyncClient) GetCommentScripts(since int64, category string) ([]CommentScriptData, int64, error) {
	params := d.syncParams()
	if since > 0 {
		params.Set("since", strconv.FormatInt(since, 10))
	}
	if category != "" {
		params.Set("group", category)
	}

	data, err := d.getSyncAPIData("/api/client/sync/comment-scripts", params)
	if err != nil {
		return nil, 0, err
	}

	var serverItems []commentScriptServerData
	var serverTime int64
	if err := json.Unmarshal(data, &serverItems); err != nil {
		var wrapped struct {
			Scripts    []commentScriptServerData `json:"scripts"`
			ServerTime int64                     `json:"server_time"`
		}
		if wrapErr := json.Unmarshal(data, &wrapped); wrapErr != nil {
			return nil, 0, err
		}
		serverItems = wrapped.Scripts
		serverTime = wrapped.ServerTime
	}

	scripts := make([]CommentScriptData, 0, len(serverItems))
	for _, item := range serverItems {
		scripts = append(scripts, commentScriptFromServerData(item))
		serverTime = maxInt64(serverTime, unixTime(item.UpdatedAt))
	}
	return scripts, serverTime, nil
}

// SaveCommentScripts 批量保存评论话术
func (d *DataSyncClient) SaveCommentScripts(scripts []CommentScriptData) error {
	if len(scripts) == 0 {
		return nil
	}
	serverItems := make([]map[string]interface{}, 0, len(scripts))
	for _, script := range scripts {
		serverItems = append(serverItems, commentScriptToServerData(script))
	}
	data, err := d.postSyncAPIData("/api/client/sync/comment-scripts/batch", map[string]interface{}{
		"scripts": serverItems,
	})
	if err != nil {
		return err
	}
	results, err := decodeSyncResults(data)
	if err != nil {
		return err
	}
	return checkSyncResults("保存评论话术", results)
}

// ==================== TTS声音配置同步 ====================

func voiceConfigToServerData(config VoiceConfigData) map[string]interface{} {
	return map[string]interface{}{
		"VoiceID":      config.ID,
		"Role":         config.Role,
		"Name":         config.Name,
		"GPTPath":      config.GPTPath,
		"SoVITSPath":   config.SoVITSPath,
		"RefAudioPath": config.RefAudioPath,
		"RefText":      config.RefText,
		"Language":     config.Language,
		"SpeedFactor":  config.SpeedFactor,
		"TTSVersion":   config.TTSVersion,
		"Enabled":      config.Enabled,
		"Version":      config.Version,
	}
}

func stringFromMap(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
}

func int64FromMap(data map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			switch typed := value.(type) {
			case float64:
				return int64(typed)
			case int64:
				return typed
			case int:
				return int64(typed)
			case string:
				parsed, _ := strconv.ParseInt(typed, 10, 64)
				return parsed
			}
		}
	}
	return 0
}

func float64FromMap(data map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			switch typed := value.(type) {
			case float64:
				return typed
			case int:
				return float64(typed)
			case int64:
				return float64(typed)
			case string:
				parsed, _ := strconv.ParseFloat(typed, 64)
				return parsed
			}
		}
	}
	return 0
}

func boolFromMap(data map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			if typed, ok := value.(bool); ok {
				return typed
			}
		}
	}
	return false
}

func voiceConfigFromSyncData(data map[string]interface{}, updatedAt int64) VoiceConfigData {
	return VoiceConfigData{
		ID:           int64FromMap(data, "VoiceID", "voice_id", "id"),
		Role:         stringFromMap(data, "Role", "role"),
		Name:         stringFromMap(data, "Name", "name"),
		GPTPath:      stringFromMap(data, "GPTPath", "gpt_path"),
		SoVITSPath:   stringFromMap(data, "SoVITSPath", "sovits_path"),
		RefAudioPath: stringFromMap(data, "RefAudioPath", "ref_audio_path"),
		RefText:      stringFromMap(data, "RefText", "ref_text"),
		Language:     stringFromMap(data, "Language", "language"),
		SpeedFactor:  float64FromMap(data, "SpeedFactor", "speed_factor"),
		TTSVersion:   int(int64FromMap(data, "TTSVersion", "tts_version")),
		Enabled:      boolFromMap(data, "Enabled", "enabled"),
		Version:      int64FromMap(data, "Version", "version"),
		UpdatedAt:    updatedAt,
	}
}

// GetVoiceConfigs 获取TTS声音配置
func (d *DataSyncClient) GetVoiceConfigs(since int64) ([]VoiceConfigData, int64, error) {
	changes, serverTime, err := d.GetChanges(since, []string{DataTypeVoiceConfig})
	if err != nil {
		return nil, 0, err
	}
	configs := make([]VoiceConfigData, 0, len(changes))
	for _, change := range changes {
		if change.Action == "delete" {
			continue
		}
		configs = append(configs, voiceConfigFromSyncData(change.Data, change.UpdatedAt))
	}
	return configs, serverTime, nil
}

// SaveVoiceConfigs 批量保存TTS声音配置
func (d *DataSyncClient) SaveVoiceConfigs(configs []VoiceConfigData) error {
	if len(configs) == 0 {
		return nil
	}
	changes := make([]SyncChange, 0, len(configs))
	for _, config := range configs {
		if config.ID == 0 {
			return fmt.Errorf("声音配置ID不能为空")
		}
		changes = append(changes, SyncChange{
			DataType:     DataTypeVoiceConfig,
			DataKey:      strconv.FormatInt(config.ID, 10),
			Action:       "update",
			Data:         voiceConfigToServerData(config),
			LocalVersion: config.Version,
		})
	}
	results, err := d.PushChanges(changes)
	if err != nil {
		return err
	}
	return checkSyncResults("保存声音配置", results)
}

// SaveVoiceConfig 保存单个TTS声音配置
func (d *DataSyncClient) SaveVoiceConfig(config VoiceConfigData) error {
	return d.SaveVoiceConfigs([]VoiceConfigData{config})
}

// DeleteVoiceConfig 删除TTS声音配置
func (d *DataSyncClient) DeleteVoiceConfig(voiceConfigID int64) error {
	results, err := d.PushChanges([]SyncChange{
		{
			DataType: DataTypeVoiceConfig,
			DataKey:  strconv.FormatInt(voiceConfigID, 10),
			Action:   "delete",
			Data:     nil,
		},
	})
	if err != nil {
		return err
	}
	return checkSyncResults("删除声音配置", results)
}

// ==================== 数据备份和同步功能 ====================

// PushBackup 推送备份数据到服务器
// dataType: 数据类型（scripts/danmaku_groups/ai_config/random_word_ai_config）
// dataJSON: JSON格式的数据
// deviceName: 设备名称（可选）
// itemCount: 条目数量（可选）
func (d *DataSyncClient) PushBackup(dataType, dataJSON, deviceName string, itemCount int) error {
	reqBody := map[string]interface{}{
		"data_type":   dataType,
		"data_json":   dataJSON,
		"device_name": deviceName,
		"item_count":  itemCount,
	}

	_, err := d.postSyncAPIData("/api/client/backup/push", reqBody)
	return err
}

// PullBackup 从服务器拉取指定类型的备份数据
// dataType: 数据类型（scripts/danmaku_groups/ai_config/random_word_ai_config）
// 返回备份数据列表（按版本降序排列，第一个为当前版本）
func (d *DataSyncClient) PullBackup(dataType string) ([]BackupData, error) {
	params := url.Values{}
	params.Set("data_type", dataType)

	data, err := d.getSyncAPIData("/api/client/backup/pull", params)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	return parseBackupData(data)
}

// PullAllBackups 从服务器拉取所有类型的备份数据
// 返回按数据类型分组的备份数据映射
func (d *DataSyncClient) PullAllBackups() (map[string][]BackupData, error) {
	data, err := d.getSyncAPIData("/api/client/backup/pull", nil)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	backups, err := parseBackupData(data)
	if err != nil {
		return nil, err
	}

	// 按数据类型分组
	backupMap := make(map[string][]BackupData)
	for _, backup := range backups {
		backupMap[backup.DataType] = append(backupMap[backup.DataType], backup)
	}

	return backupMap, nil
}

func parseBackupData(raw json.RawMessage) ([]BackupData, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var direct []BackupData
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}

	var wrapped struct {
		Data []BackupData `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("解析备份数据失败: %w", err)
	}

	return wrapped.Data, nil
}
