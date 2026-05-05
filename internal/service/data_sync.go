package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"license-server/internal/model"
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// DataSyncService 数据同步服务
type DataSyncService struct {
	db *gorm.DB
}

func NewDataSyncService() *DataSyncService {
	return &DataSyncService{db: model.DB}
}

func (s *DataSyncService) database() *gorm.DB {
	if s != nil && s.db != nil {
		return s.db
	}
	return model.DB
}

// SyncItem 同步项
type SyncItem struct {
	DataType  string      `json:"data_type"`
	DataKey   string      `json:"data_key"`
	Action    string      `json:"action"` // create/update/delete
	Data      interface{} `json:"data"`
	Version   int64       `json:"version"`
	UpdatedAt int64       `json:"updated_at"`
}

// SyncResult 同步结果
type SyncResult struct {
	DataKey       string      `json:"data_key"`
	Status        string      `json:"status"` // success/conflict/error
	ServerVersion int64       `json:"server_version"`
	ConflictID    string      `json:"conflict_id,omitempty"`
	ConflictData  interface{} `json:"conflict_data,omitempty"`
	Error         string      `json:"error,omitempty"`
}

// ==================== 获取变更 (Pull) ====================

// GetChanges 获取服务端变更
func (s *DataSyncService) GetChanges(userID, appID, dataType string, since time.Time, limit int) ([]SyncItem, error) {
	page, err := s.GetChangesPage(userID, appID, dataType, since, limit, 0)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

type SyncChangesPage struct {
	Items      []SyncItem
	HasMore    bool
	NextOffset int
	Limit      int
	Offset     int
}

func normalizeSyncLimitOffset(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// GetChangesPage 获取一页服务端变更。dataType 为空时会合并所有支持的类型后统一分页。
func (s *DataSyncService) GetChangesPage(userID, appID, dataType string, since time.Time, limit, offset int) (*SyncChangesPage, error) {
	limit, offset = normalizeSyncLimitOffset(limit, offset)
	allLimit := limit + offset + 1
	var items []SyncItem

	switch dataType {
	case model.DataTypeConfig:
		items = s.getConfigChanges(userID, appID, since, allLimit)
	case model.DataTypeWorkflow:
		items = s.getWorkflowChanges(userID, appID, since, allLimit)
	case model.DataTypeBatchTask:
		items = s.getBatchTaskChanges(userID, appID, since, allLimit)
	case model.DataTypeMaterial:
		items = s.getMaterialChanges(userID, appID, since, allLimit)
	case model.DataTypePost:
		items = s.getPostChanges(userID, appID, since, allLimit)
	case model.DataTypeComment:
		items = s.getCommentChanges(userID, appID, since, allLimit)
	case model.DataTypeCommentScript:
		items = s.getCommentScriptChanges(userID, appID, since, allLimit)
	case model.DataTypeVoiceConfig:
		items = s.getVoiceConfigChanges(userID, appID, since, allLimit)
	case "":
		// 获取所有类型
		items = append(items, s.getConfigChanges(userID, appID, since, allLimit)...)
		items = append(items, s.getWorkflowChanges(userID, appID, since, allLimit)...)
		items = append(items, s.getBatchTaskChanges(userID, appID, since, allLimit)...)
		items = append(items, s.getMaterialChanges(userID, appID, since, allLimit)...)
		items = append(items, s.getPostChanges(userID, appID, since, allLimit)...)
		items = append(items, s.getCommentChanges(userID, appID, since, allLimit)...)
		items = append(items, s.getCommentScriptChanges(userID, appID, since, allLimit)...)
		items = append(items, s.getVoiceConfigChanges(userID, appID, since, allLimit)...)
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].UpdatedAt == items[j].UpdatedAt {
				if items[i].DataType == items[j].DataType {
					return items[i].DataKey < items[j].DataKey
				}
				return items[i].DataType < items[j].DataType
			}
			return items[i].UpdatedAt < items[j].UpdatedAt
		})
	default:
		return nil, errors.New("未知的数据类型")
	}

	page := &SyncChangesPage{
		Limit:  limit,
		Offset: offset,
	}
	if offset < len(items) {
		end := offset + limit
		if end > len(items) {
			end = len(items)
		}
		page.Items = items[offset:end]
	}
	page.HasMore = len(items) > offset+limit
	if page.HasMore {
		page.NextOffset = offset + len(page.Items)
	}
	return page, nil
}

func (s *DataSyncService) getConfigChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var configs []model.UserConfig
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, config_key ASC").Limit(limit).Find(&configs)

	items := make([]SyncItem, 0, len(configs))
	for _, c := range configs {
		action := "update"
		if c.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypeConfig,
			DataKey:   c.ConfigKey,
			Action:    action,
			Data:      c.ConfigValue,
			Version:   c.Version,
			UpdatedAt: c.UpdatedAt.Unix(),
		})
	}
	return items
}

func (s *DataSyncService) getWorkflowChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var workflows []model.UserWorkflow
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, workflow_id ASC").Limit(limit).Find(&workflows)

	items := make([]SyncItem, 0, len(workflows))
	for _, w := range workflows {
		action := "update"
		if w.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypeWorkflow,
			DataKey:   w.WorkflowID,
			Action:    action,
			Data:      w,
			Version:   w.Version,
			UpdatedAt: w.UpdatedAt.Unix(),
		})
	}
	return items
}

func (s *DataSyncService) getBatchTaskChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var tasks []model.UserBatchTask
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, task_id ASC").Limit(limit).Find(&tasks)

	items := make([]SyncItem, 0, len(tasks))
	for _, t := range tasks {
		action := "update"
		if t.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypeBatchTask,
			DataKey:   t.TaskID,
			Action:    action,
			Data:      t,
			Version:   t.Version,
			UpdatedAt: t.UpdatedAt.Unix(),
		})
	}
	return items
}

func (s *DataSyncService) getMaterialChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var materials []model.UserMaterial
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, material_id ASC").Limit(limit).Find(&materials)

	items := make([]SyncItem, 0, len(materials))
	for _, m := range materials {
		action := "update"
		if m.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypeMaterial,
			DataKey:   strconv.FormatInt(m.MaterialID, 10),
			Action:    action,
			Data:      m,
			Version:   m.Version,
			UpdatedAt: m.UpdatedAt.Unix(),
		})
	}
	return items
}

func (s *DataSyncService) getPostChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var posts []model.UserPost
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, id ASC").Limit(limit).Find(&posts)

	items := make([]SyncItem, 0, len(posts))
	for _, p := range posts {
		action := "update"
		if p.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypePost,
			DataKey:   p.ID,
			Action:    action,
			Data:      p,
			Version:   p.Version,
			UpdatedAt: p.UpdatedAt.Unix(),
		})
	}
	return items
}

func (s *DataSyncService) getCommentChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var comments []model.UserComment
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, id ASC").Limit(limit).Find(&comments)

	items := make([]SyncItem, 0, len(comments))
	for _, c := range comments {
		action := "update"
		if c.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypeComment,
			DataKey:   c.ID,
			Action:    action,
			Data:      c,
			Version:   c.Version,
			UpdatedAt: c.UpdatedAt.Unix(),
		})
	}
	return items
}

func (s *DataSyncService) getCommentScriptChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var scripts []model.UserCommentScript
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, id ASC").Limit(limit).Find(&scripts)

	items := make([]SyncItem, 0, len(scripts))
	for _, cs := range scripts {
		action := "update"
		if cs.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypeCommentScript,
			DataKey:   cs.ID,
			Action:    action,
			Data:      cs,
			Version:   cs.Version,
			UpdatedAt: cs.UpdatedAt.Unix(),
		})
	}
	return items
}

func (s *DataSyncService) getVoiceConfigChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var configs []model.UserVoiceConfig
	s.database().Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC, voice_id ASC").Limit(limit).Find(&configs)

	items := make([]SyncItem, 0, len(configs))
	for _, vc := range configs {
		action := "update"
		if vc.IsDeleted {
			action = "delete"
		}
		items = append(items, SyncItem{
			DataType:  model.DataTypeVoiceConfig,
			DataKey:   strconv.FormatInt(vc.VoiceID, 10),
			Action:    action,
			Data:      vc,
			Version:   vc.Version,
			UpdatedAt: vc.UpdatedAt.Unix(),
		})
	}
	return items
}

// ==================== 推送变更 (Push) ====================

// PushItem 推送项
type PushItem struct {
	DataType     string          `json:"data_type"`
	DataKey      string          `json:"data_key"`
	Action       string          `json:"action"` // create/update/delete
	Data         json.RawMessage `json:"data"`
	LocalVersion int64           `json:"local_version"`
}

// PushChanges 推送客户端变更
func (s *DataSyncService) PushChanges(userID, appID, deviceID string, items []PushItem) ([]SyncResult, error) {
	results := make([]SyncResult, 0, len(items))

	for _, item := range items {
		result := s.pushSingleItem(userID, appID, deviceID, item)
		results = append(results, result)
	}

	return results, nil
}

func (s *DataSyncService) pushSingleItem(userID, appID, deviceID string, item PushItem) SyncResult {
	switch item.DataType {
	case model.DataTypeConfig:
		return s.pushConfig(userID, appID, deviceID, item)
	case model.DataTypeWorkflow:
		return s.pushWorkflow(userID, appID, deviceID, item)
	case model.DataTypeBatchTask:
		return s.pushBatchTask(userID, appID, deviceID, item)
	case model.DataTypeMaterial:
		return s.pushMaterial(userID, appID, deviceID, item)
	case model.DataTypePost:
		return s.pushPost(userID, appID, deviceID, item)
	case model.DataTypeComment:
		return s.pushComment(userID, appID, deviceID, item)
	case model.DataTypeCommentScript:
		return s.pushCommentScript(userID, appID, deviceID, item)
	case model.DataTypeVoiceConfig:
		return s.pushVoiceConfig(userID, appID, deviceID, item)
	default:
		return SyncResult{
			DataKey: item.DataKey,
			Status:  "error",
			Error:   "未知的数据类型",
		}
	}
}

func errorResult(item PushItem, format string, args ...interface{}) SyncResult {
	return SyncResult{
		DataKey: item.DataKey,
		Status:  "error",
		Error:   fmt.Sprintf(format, args...),
	}
}

func decodePushData(item PushItem, target interface{}) error {
	if item.Action == "delete" && len(item.Data) == 0 {
		return nil
	}
	if len(item.Data) == 0 {
		return errors.New("缺少 data")
	}
	return json.Unmarshal(item.Data, target)
}

func parseInt64DataKey(item PushItem) (int64, error) {
	value, err := strconv.ParseInt(item.DataKey, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("无效的数据键 %q: %w", item.DataKey, err)
	}
	return value, nil
}

func (s *DataSyncService) pushConfig(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var existing model.UserConfig
	err := db.Where("user_id = ? AND app_id = ? AND config_key = ?", userID, appID, item.DataKey).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 新建
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		config := model.UserConfig{
			UserID:      userID,
			AppID:       appID,
			ConfigKey:   item.DataKey,
			ConfigValue: string(item.Data),
			Version:     1,
		}
		if err := db.Create(&config).Error; err != nil {
			return errorResult(item, "创建配置失败: %v", err)
		}
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询配置失败: %v", err)
	}

	// 检查版本冲突
	if existing.Version != item.LocalVersion {
		return s.conflictResult(userID, appID, deviceID, item, existing.Version, existing.ConfigValue)
	}

	// 更新
	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.ConfigValue = string(item.Data)
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存配置失败: %v", err)
	}

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushWorkflow(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var workflow model.UserWorkflow
	if err := decodePushData(item, &workflow); err != nil {
		return errorResult(item, "解析工作流数据失败: %v", err)
	}
	if workflow.WorkflowID == "" {
		workflow.WorkflowID = item.DataKey
	}

	var existing model.UserWorkflow
	err := db.Where("workflow_id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 新建
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		workflow.UserID = userID
		workflow.AppID = appID
		workflow.Version = 1
		if err := db.Create(&workflow).Error; err != nil {
			return errorResult(item, "创建工作流失败: %v", err)
		}
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询工作流失败: %v", err)
	}

	// 检查版本冲突
	if item.Action != "delete" && existing.Version != item.LocalVersion {
		return s.conflictResult(userID, appID, deviceID, item, existing.Version, existing)
	}

	// 更新
	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.WorkflowName = workflow.WorkflowName
		existing.Description = workflow.Description
		existing.Steps = workflow.Steps
		existing.Status = workflow.Status
		existing.CurrentStep = workflow.CurrentStep
		existing.StartTime = workflow.StartTime
		existing.EndTime = workflow.EndTime
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存工作流失败: %v", err)
	}

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushBatchTask(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var task model.UserBatchTask
	if err := decodePushData(item, &task); err != nil {
		return errorResult(item, "解析批量任务数据失败: %v", err)
	}
	if task.TaskID == "" {
		task.TaskID = item.DataKey
	}

	var existing model.UserBatchTask
	err := db.Where("task_id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		task.UserID = userID
		task.AppID = appID
		task.Version = 1
		if err := db.Create(&task).Error; err != nil {
			return errorResult(item, "创建批量任务失败: %v", err)
		}
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询批量任务失败: %v", err)
	}

	if existing.Version != item.LocalVersion {
		return s.conflictResult(userID, appID, deviceID, item, existing.Version, existing)
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.TaskName = task.TaskName
		existing.Description = task.Description
		existing.ScriptPath = task.ScriptPath
		existing.ScriptType = task.ScriptType
		existing.Params = task.Params
		existing.Environments = task.Environments
		existing.EnvConfig = task.EnvConfig
		existing.Status = task.Status
		existing.Concurrency = task.Concurrency
		existing.TotalCount = task.TotalCount
		existing.CompletedCount = task.CompletedCount
		existing.FailedCount = task.FailedCount
		existing.CurrentIndex = task.CurrentIndex
		existing.CloseOnComplete = task.CloseOnComplete
		existing.StartTime = task.StartTime
		existing.EndTime = task.EndTime
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存批量任务失败: %v", err)
	}

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushMaterial(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var material model.UserMaterial
	if err := decodePushData(item, &material); err != nil {
		return errorResult(item, "解析素材数据失败: %v", err)
	}
	if material.MaterialID == 0 {
		materialID, err := parseInt64DataKey(item)
		if err != nil {
			return errorResult(item, "%v", err)
		}
		material.MaterialID = materialID
	}

	var existing model.UserMaterial
	err := db.Where("material_id = ? AND user_id = ? AND app_id = ?", material.MaterialID, userID, appID).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		material.UserID = userID
		material.AppID = appID
		material.Version = 1
		if err := db.Create(&material).Error; err != nil {
			return errorResult(item, "创建素材失败: %v", err)
		}
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询素材失败: %v", err)
	}

	if existing.Version != item.LocalVersion {
		return s.conflictResult(userID, appID, deviceID, item, existing.Version, existing)
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.FileName = material.FileName
		existing.FileType = material.FileType
		existing.Caption = material.Caption
		existing.GroupName = material.GroupName
		existing.Status = material.Status
		existing.LocalPath = material.LocalPath
		existing.UsedAt = material.UsedAt
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存素材失败: %v", err)
	}

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushPost(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var post model.UserPost
	if err := decodePushData(item, &post); err != nil {
		return errorResult(item, "解析帖子数据失败: %v", err)
	}
	if post.ID == "" {
		post.ID = item.DataKey
	}

	var existing model.UserPost
	query := db.Where("user_id = ? AND app_id = ? AND id = ?", userID, appID, item.DataKey)
	if post.PostLink != "" && post.GroupName != "" {
		query = db.Where("user_id = ? AND app_id = ? AND (id = ? OR (post_link = ? AND group_name = ?))",
			userID, appID, item.DataKey, post.PostLink, post.GroupName)
	}
	err := query.First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		post.UserID = userID
		post.AppID = appID
		post.Version = 1
		if err := db.Create(&post).Error; err != nil {
			return errorResult(item, "创建帖子失败: %v", err)
		}
		return SyncResult{DataKey: post.ID, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询帖子失败: %v", err)
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		result := s.conflictResult(userID, appID, deviceID, item, existing.Version, existing)
		result.DataKey = existing.ID
		return result
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		if post.PostType != "" {
			existing.PostType = post.PostType
		}
		if post.GroupName != "" {
			existing.GroupName = post.GroupName
		}
		if post.PostLink != "" {
			existing.PostLink = post.PostLink
		}
		if post.PostID != "" {
			existing.PostID = post.PostID
		}
		if post.Shortcode != "" {
			existing.Shortcode = post.Shortcode
		}
		if post.Username != "" {
			existing.Username = post.Username
		}
		if post.FullName != "" {
			existing.FullName = post.FullName
		}
		if post.Caption != "" {
			existing.Caption = post.Caption
		}
		if post.MediaType != "" {
			existing.MediaType = post.MediaType
		}
		if post.LikeCount != 0 {
			existing.LikeCount = post.LikeCount
		}
		if post.CommentCount != 0 {
			existing.CommentCount = post.CommentCount
		}
		if post.Timestamp != nil {
			existing.Timestamp = post.Timestamp
		}
		if post.Status != "" {
			existing.Status = post.Status
		}
		if !post.CollectedAt.IsZero() {
			existing.CollectedAt = post.CollectedAt
		}
		existing.UsedAt = post.UsedAt
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存帖子失败: %v", err)
	}

	return SyncResult{DataKey: existing.ID, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushComment(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var comment model.UserComment
	if err := decodePushData(item, &comment); err != nil {
		return errorResult(item, "解析评论数据失败: %v", err)
	}
	if comment.ID == "" {
		comment.ID = item.DataKey
	}

	var existing model.UserComment
	err := db.Where("id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		comment.UserID = userID
		comment.AppID = appID
		comment.Version = 1
		if err := db.Create(&comment).Error; err != nil {
			return errorResult(item, "创建评论失败: %v", err)
		}
		return SyncResult{DataKey: comment.ID, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询评论失败: %v", err)
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		result := s.conflictResult(userID, appID, deviceID, item, existing.Version, existing)
		result.DataKey = existing.ID
		return result
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.GroupName = comment.GroupName
		existing.PostLink = comment.PostLink
		existing.PostID = comment.PostID
		existing.CommentID = comment.CommentID
		existing.Username = comment.Username
		existing.FullName = comment.FullName
		existing.Content = comment.Content
		existing.LikeCount = comment.LikeCount
		existing.Timestamp = comment.Timestamp
		existing.CollectedAt = comment.CollectedAt
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存评论失败: %v", err)
	}

	return SyncResult{DataKey: existing.ID, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushCommentScript(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var script model.UserCommentScript
	if err := decodePushData(item, &script); err != nil {
		return errorResult(item, "解析评论话术数据失败: %v", err)
	}
	if script.ID == "" {
		script.ID = item.DataKey
	}

	var existing model.UserCommentScript
	err := db.Where("id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		script.UserID = userID
		script.AppID = appID
		script.Version = 1
		if err := db.Create(&script).Error; err != nil {
			return errorResult(item, "创建评论话术失败: %v", err)
		}
		return SyncResult{DataKey: script.ID, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询评论话术失败: %v", err)
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		result := s.conflictResult(userID, appID, deviceID, item, existing.Version, existing)
		result.DataKey = existing.ID
		return result
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.GroupName = script.GroupName
		existing.Content = script.Content
		existing.UseCount = script.UseCount
		existing.Status = script.Status
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存评论话术失败: %v", err)
	}

	return SyncResult{DataKey: existing.ID, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushVoiceConfig(userID, appID, deviceID string, item PushItem) SyncResult {
	db := s.database()
	var voiceConfig model.UserVoiceConfig
	if err := decodePushData(item, &voiceConfig); err != nil {
		return errorResult(item, "解析声音配置数据失败: %v", err)
	}
	if voiceConfig.VoiceID == 0 {
		voiceID, err := parseInt64DataKey(item)
		if err != nil {
			return errorResult(item, "%v", err)
		}
		voiceConfig.VoiceID = voiceID
	}

	var existing model.UserVoiceConfig
	err := db.Where("voice_id = ? AND user_id = ? AND app_id = ?", voiceConfig.VoiceID, userID, appID).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		voiceConfig.UserID = userID
		voiceConfig.AppID = appID
		voiceConfig.Version = 1
		if err := db.Create(&voiceConfig).Error; err != nil {
			return errorResult(item, "创建声音配置失败: %v", err)
		}
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}
	if err != nil {
		return errorResult(item, "查询声音配置失败: %v", err)
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		return s.conflictResult(userID, appID, deviceID, item, existing.Version, existing)
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.Role = voiceConfig.Role
		existing.Name = voiceConfig.Name
		existing.GPTPath = voiceConfig.GPTPath
		existing.SoVITSPath = voiceConfig.SoVITSPath
		existing.RefAudioPath = voiceConfig.RefAudioPath
		existing.RefText = voiceConfig.RefText
		existing.Language = voiceConfig.Language
		existing.SpeedFactor = voiceConfig.SpeedFactor
		existing.TTSVersion = voiceConfig.TTSVersion
		existing.Enabled = voiceConfig.Enabled
		existing.IsDeleted = false
	}
	existing.Version++
	if err := db.Save(&existing).Error; err != nil {
		return errorResult(item, "保存声音配置失败: %v", err)
	}

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) conflictResult(userID, appID, deviceID string, item PushItem, serverVersion int64, serverData interface{}) SyncResult {
	conflictID, err := s.recordConflict(userID, appID, deviceID, item, serverVersion, serverData)
	if err != nil {
		return errorResult(item, "记录冲突失败: %v", err)
	}
	return SyncResult{
		DataKey:       item.DataKey,
		Status:        "conflict",
		ServerVersion: serverVersion,
		ConflictID:    conflictID,
		ConflictData:  serverData,
	}
}

func (s *DataSyncService) recordConflict(userID, appID, deviceID string, item PushItem, serverVersion int64, serverData interface{}) (string, error) {
	db := s.database()
	localData := string(item.Data)
	serverDataText := conflictDataString(serverData)

	var conflict model.SyncConflict
	err := db.Where(
		"user_id = ? AND app_id = ? AND device_id = ? AND data_type = ? AND data_key = ? AND status = ?",
		userID,
		appID,
		deviceID,
		item.DataType,
		item.DataKey,
		"pending",
	).First(&conflict).Error
	if err == nil {
		conflict.LocalVersion = item.LocalVersion
		conflict.ServerVersion = serverVersion
		conflict.LocalData = localData
		conflict.ServerData = serverDataText
		if saveErr := db.Save(&conflict).Error; saveErr != nil {
			return "", saveErr
		}
		return conflict.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	conflict = model.SyncConflict{
		UserID:        userID,
		DeviceID:      deviceID,
		AppID:         appID,
		DataType:      item.DataType,
		DataKey:       item.DataKey,
		LocalVersion:  item.LocalVersion,
		ServerVersion: serverVersion,
		LocalData:     localData,
		ServerData:    serverDataText,
		Status:        "pending",
	}
	if createErr := db.Create(&conflict).Error; createErr != nil {
		return "", createErr
	}
	return conflict.ID, nil
}

func conflictDataString(data interface{}) string {
	switch v := data.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case json.RawMessage:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// ==================== 冲突处理 ====================

// ResolveConflict 解决冲突
func (s *DataSyncService) ResolveConflict(conflictID, userID, appID, deviceID, resolution string, mergedData json.RawMessage) error {
	return s.database().Transaction(func(tx *gorm.DB) error {
		var conflict model.SyncConflict
		if err := tx.First(&conflict, "id = ? AND user_id = ? AND app_id = ? AND device_id = ?",
			conflictID, userID, appID, deviceID).Error; err != nil {
			return errors.New("冲突记录不存在")
		}

		if conflict.Status != "pending" {
			return errors.New("冲突已解决")
		}

		now := time.Now()
		conflict.Resolution = resolution
		conflict.ResolvedAt = &now
		conflict.Status = "resolved"

		var dataToUse json.RawMessage
		switch resolution {
		case model.ConflictResolutionUseLocal:
			dataToUse = json.RawMessage(conflict.LocalData)
		case model.ConflictResolutionUseServer:
			return tx.Save(&conflict).Error
		case model.ConflictResolutionMerge:
			if len(mergedData) == 0 {
				return errors.New("合并方式需要提供 merged_data")
			}
			dataToUse = mergedData
			conflict.ResolvedData = string(mergedData)
		default:
			return errors.New("无效的解决方式")
		}

		// 应用解决方案
		item := PushItem{
			DataType:     conflict.DataType,
			DataKey:      conflict.DataKey,
			Action:       "update",
			Data:         dataToUse,
			LocalVersion: conflict.ServerVersion, // 使用服务端版本以避免再次冲突
		}
		result := (&DataSyncService{db: tx}).pushSingleItem(conflict.UserID, conflict.AppID, conflict.DeviceID, item)
		if result.Status != "success" {
			if result.Error != "" {
				return errors.New(result.Error)
			}
			if result.ConflictID != "" {
				return fmt.Errorf("解决冲突时再次发生冲突: %s", result.ConflictID)
			}
			return errors.New("应用冲突解决结果失败")
		}

		return tx.Save(&conflict).Error
	})
}

// ==================== 同步检查点 ====================

// UpdateCheckpoint 更新同步检查点
func (s *DataSyncService) UpdateCheckpoint(userID, deviceID, appID, dataType string, syncTime time.Time, version int64) error {
	db := s.database()
	var checkpoint model.SyncCheckpoint
	err := db.Where("user_id = ? AND device_id = ? AND app_id = ? AND data_type = ?",
		userID, deviceID, appID, dataType).First(&checkpoint).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		checkpoint = model.SyncCheckpoint{
			UserID:      userID,
			DeviceID:    deviceID,
			AppID:       appID,
			DataType:    dataType,
			LastSyncAt:  syncTime,
			LastVersion: version,
		}
		return db.Create(&checkpoint).Error
	}
	if err != nil {
		return err
	}

	checkpoint.LastSyncAt = syncTime
	checkpoint.LastVersion = version
	return db.Save(&checkpoint).Error
}

// GetCheckpoint 获取同步检查点
func (s *DataSyncService) GetCheckpoint(userID, deviceID, appID, dataType string) (*model.SyncCheckpoint, error) {
	var checkpoint model.SyncCheckpoint
	err := s.database().Where("user_id = ? AND device_id = ? AND app_id = ? AND data_type = ?",
		userID, deviceID, appID, dataType).First(&checkpoint).Error
	if err != nil {
		return nil, err
	}
	return &checkpoint, nil
}

// ==================== 同步日志 ====================

// LogSync 记录同步日志
func (s *DataSyncService) LogSync(userID, deviceID, appID, action, dataType, dataKey string, itemCount int, status, errorMsg string, duration int64) {
	log := model.SyncLog{
		UserID:    userID,
		DeviceID:  deviceID,
		AppID:     appID,
		Action:    action,
		DataType:  dataType,
		DataKey:   dataKey,
		ItemCount: itemCount,
		Status:    status,
		ErrorMsg:  errorMsg,
		Duration:  duration,
	}
	_ = s.database().Create(&log).Error
}

// ==================== 统计信息 ====================

// SyncStats 同步统计
type SyncStats struct {
	ConfigCount        int64 `json:"config_count"`
	WorkflowCount      int64 `json:"workflow_count"`
	BatchTaskCount     int64 `json:"batch_task_count"`
	MaterialCount      int64 `json:"material_count"`
	PostCount          int64 `json:"post_count"`
	CommentCount       int64 `json:"comment_count"`
	CommentScriptCount int64 `json:"comment_script_count"`
	VoiceConfigCount   int64 `json:"voice_config_count"`
	PendingConflicts   int64 `json:"pending_conflicts"`
	StorageUsed        int64 `json:"storage_used"`
}

// GetSyncStats 获取同步统计
func (s *DataSyncService) GetSyncStats(userID, appID string) (*SyncStats, error) {
	db := s.database()
	stats := &SyncStats{}

	if err := db.Model(&model.UserConfig{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.ConfigCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.UserWorkflow{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.WorkflowCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.UserBatchTask{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.BatchTaskCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.UserMaterial{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.MaterialCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.UserPost{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.PostCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.UserComment{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.CommentCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.UserCommentScript{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.CommentScriptCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.UserVoiceConfig{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.VoiceConfigCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.SyncConflict{}).Where("user_id = ? AND app_id = ? AND status = ?", userID, appID, "pending").Count(&stats.PendingConflicts).Error; err != nil {
		return nil, err
	}

	// 计算存储使用量 (文件)
	var totalSize int64
	if err := db.Model(&model.UserFile{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).
		Select("COALESCE(SUM(file_size), 0)").Scan(&totalSize).Error; err != nil {
		return nil, err
	}
	stats.StorageUsed = totalSize

	return stats, nil
}
