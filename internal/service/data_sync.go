package service

import (
	"encoding/json"
	"errors"
	"license-server/internal/model"
	"strconv"
	"time"
)

// DataSyncService 数据同步服务
type DataSyncService struct{}

func NewDataSyncService() *DataSyncService {
	return &DataSyncService{}
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
	ConflictData  interface{} `json:"conflict_data,omitempty"`
	Error         string      `json:"error,omitempty"`
}

// ==================== 获取变更 (Pull) ====================

// GetChanges 获取服务端变更
func (s *DataSyncService) GetChanges(userID, appID, dataType string, since time.Time, limit int) ([]SyncItem, error) {
	var items []SyncItem

	if limit <= 0 {
		limit = 100
	}

	switch dataType {
	case model.DataTypeConfig:
		items = s.getConfigChanges(userID, appID, since, limit)
	case model.DataTypeWorkflow:
		items = s.getWorkflowChanges(userID, appID, since, limit)
	case model.DataTypeBatchTask:
		items = s.getBatchTaskChanges(userID, appID, since, limit)
	case model.DataTypeMaterial:
		items = s.getMaterialChanges(userID, appID, since, limit)
	case model.DataTypePost:
		items = s.getPostChanges(userID, appID, since, limit)
	case model.DataTypeComment:
		items = s.getCommentChanges(userID, appID, since, limit)
	case model.DataTypeCommentScript:
		items = s.getCommentScriptChanges(userID, appID, since, limit)
	case model.DataTypeVoiceConfig:
		items = s.getVoiceConfigChanges(userID, appID, since, limit)
	case "":
		// 获取所有类型
		items = append(items, s.getConfigChanges(userID, appID, since, limit)...)
		items = append(items, s.getWorkflowChanges(userID, appID, since, limit)...)
		items = append(items, s.getMaterialChanges(userID, appID, since, limit)...)
		items = append(items, s.getCommentScriptChanges(userID, appID, since, limit)...)
		items = append(items, s.getVoiceConfigChanges(userID, appID, since, limit)...)
	default:
		return nil, errors.New("未知的数据类型")
	}

	return items, nil
}

func (s *DataSyncService) getConfigChanges(userID, appID string, since time.Time, limit int) []SyncItem {
	var configs []model.UserConfig
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&configs)

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
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&workflows)

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
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&tasks)

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
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&materials)

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
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&posts)

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
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&comments)

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
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&scripts)

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
	model.DB.Where("user_id = ? AND app_id = ? AND updated_at > ?", userID, appID, since).
		Order("updated_at ASC").Limit(limit).Find(&configs)

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
		return s.pushConfig(userID, appID, item)
	case model.DataTypeWorkflow:
		return s.pushWorkflow(userID, appID, item)
	case model.DataTypeBatchTask:
		return s.pushBatchTask(userID, appID, item)
	case model.DataTypeMaterial:
		return s.pushMaterial(userID, appID, item)
	case model.DataTypePost:
		return s.pushPost(userID, appID, item)
	case model.DataTypeComment:
		return s.pushComment(userID, appID, item)
	case model.DataTypeCommentScript:
		return s.pushCommentScript(userID, appID, item)
	case model.DataTypeVoiceConfig:
		return s.pushVoiceConfig(userID, appID, item)
	default:
		return SyncResult{
			DataKey: item.DataKey,
			Status:  "error",
			Error:   "未知的数据类型",
		}
	}
}

func (s *DataSyncService) pushConfig(userID, appID string, item PushItem) SyncResult {
	var existing model.UserConfig
	err := model.DB.Where("user_id = ? AND app_id = ? AND config_key = ?", userID, appID, item.DataKey).First(&existing).Error

	if err != nil {
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
		model.DB.Create(&config)
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}

	// 检查版本冲突
	if existing.Version != item.LocalVersion {
		return SyncResult{
			DataKey:       item.DataKey,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing.ConfigValue,
		}
	}

	// 更新
	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.ConfigValue = string(item.Data)
		existing.IsDeleted = false
	}
	existing.Version++
	model.DB.Save(&existing)

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushWorkflow(userID, appID string, item PushItem) SyncResult {
	var workflow model.UserWorkflow
	json.Unmarshal(item.Data, &workflow)

	var existing model.UserWorkflow
	err := model.DB.Where("workflow_id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if err != nil {
		// 新建
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		workflow.UserID = userID
		workflow.AppID = appID
		workflow.Version = 1
		model.DB.Create(&workflow)
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}

	// 检查版本冲突
	if existing.Version != item.LocalVersion {
		return SyncResult{
			DataKey:       item.DataKey,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing,
		}
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
	model.DB.Save(&existing)

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushBatchTask(userID, appID string, item PushItem) SyncResult {
	var task model.UserBatchTask
	json.Unmarshal(item.Data, &task)

	var existing model.UserBatchTask
	err := model.DB.Where("task_id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if err != nil {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		task.UserID = userID
		task.AppID = appID
		task.Version = 1
		model.DB.Create(&task)
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}

	if existing.Version != item.LocalVersion {
		return SyncResult{
			DataKey:       item.DataKey,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing,
		}
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
	model.DB.Save(&existing)

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushMaterial(userID, appID string, item PushItem) SyncResult {
	var material model.UserMaterial
	json.Unmarshal(item.Data, &material)

	var existing model.UserMaterial
	err := model.DB.Where("material_id = ? AND user_id = ? AND app_id = ?", material.MaterialID, userID, appID).First(&existing).Error

	if err != nil {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		material.UserID = userID
		material.AppID = appID
		material.Version = 1
		model.DB.Create(&material)
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}

	if existing.Version != item.LocalVersion {
		return SyncResult{
			DataKey:       item.DataKey,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing,
		}
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
	model.DB.Save(&existing)

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushPost(userID, appID string, item PushItem) SyncResult {
	var post model.UserPost
	json.Unmarshal(item.Data, &post)

	var existing model.UserPost
	err := model.DB.Where("user_id = ? AND app_id = ? AND (id = ? OR (post_link = ? AND group_name = ?))",
		userID, appID, item.DataKey, post.PostLink, post.GroupName).First(&existing).Error

	if err != nil {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		post.UserID = userID
		post.AppID = appID
		post.Version = 1
		model.DB.Create(&post)
		return SyncResult{DataKey: post.ID, Status: "success", ServerVersion: 1}
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		return SyncResult{
			DataKey:       existing.ID,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing,
		}
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	} else {
		existing.Status = post.Status
		existing.UsedAt = post.UsedAt
		existing.IsDeleted = false
	}
	existing.Version++
	model.DB.Save(&existing)

	return SyncResult{DataKey: existing.ID, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushComment(userID, appID string, item PushItem) SyncResult {
	var comment model.UserComment
	json.Unmarshal(item.Data, &comment)

	var existing model.UserComment
	err := model.DB.Where("id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if err != nil {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		comment.UserID = userID
		comment.AppID = appID
		comment.Version = 1
		model.DB.Create(&comment)
		return SyncResult{DataKey: comment.ID, Status: "success", ServerVersion: 1}
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		return SyncResult{
			DataKey:       existing.ID,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing,
		}
	}

	if item.Action == "delete" {
		existing.IsDeleted = true
	}
	existing.Version++
	model.DB.Save(&existing)

	return SyncResult{DataKey: existing.ID, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushCommentScript(userID, appID string, item PushItem) SyncResult {
	var script model.UserCommentScript
	json.Unmarshal(item.Data, &script)

	var existing model.UserCommentScript
	err := model.DB.Where("id = ? AND user_id = ? AND app_id = ?", item.DataKey, userID, appID).First(&existing).Error

	if err != nil {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		script.UserID = userID
		script.AppID = appID
		script.Version = 1
		model.DB.Create(&script)
		return SyncResult{DataKey: script.ID, Status: "success", ServerVersion: 1}
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		return SyncResult{
			DataKey:       existing.ID,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing,
		}
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
	model.DB.Save(&existing)

	return SyncResult{DataKey: existing.ID, Status: "success", ServerVersion: existing.Version}
}

func (s *DataSyncService) pushVoiceConfig(userID, appID string, item PushItem) SyncResult {
	var voiceConfig model.UserVoiceConfig
	json.Unmarshal(item.Data, &voiceConfig)

	var existing model.UserVoiceConfig
	err := model.DB.Where("voice_id = ? AND user_id = ? AND app_id = ?", voiceConfig.VoiceID, userID, appID).First(&existing).Error

	if err != nil {
		if item.Action == "delete" {
			return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 0}
		}
		voiceConfig.UserID = userID
		voiceConfig.AppID = appID
		voiceConfig.Version = 1
		model.DB.Create(&voiceConfig)
		return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: 1}
	}

	if existing.Version != item.LocalVersion && item.LocalVersion != 0 {
		return SyncResult{
			DataKey:       item.DataKey,
			Status:        "conflict",
			ServerVersion: existing.Version,
			ConflictData:  existing,
		}
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
	model.DB.Save(&existing)

	return SyncResult{DataKey: item.DataKey, Status: "success", ServerVersion: existing.Version}
}

// ==================== 冲突处理 ====================

// ResolveConflict 解决冲突
func (s *DataSyncService) ResolveConflict(conflictID, resolution string, mergedData json.RawMessage) error {
	var conflict model.SyncConflict
	if err := model.DB.First(&conflict, "id = ?", conflictID).Error; err != nil {
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
		dataToUse = json.RawMessage(conflict.ServerData)
	case model.ConflictResolutionMerge:
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
	s.pushSingleItem(conflict.UserID, conflict.AppID, conflict.DeviceID, item)

	model.DB.Save(&conflict)
	return nil
}

// ==================== 同步检查点 ====================

// UpdateCheckpoint 更新同步检查点
func (s *DataSyncService) UpdateCheckpoint(userID, deviceID, appID, dataType string, syncTime time.Time, version int64) error {
	var checkpoint model.SyncCheckpoint
	err := model.DB.Where("user_id = ? AND device_id = ? AND app_id = ? AND data_type = ?",
		userID, deviceID, appID, dataType).First(&checkpoint).Error

	if err != nil {
		checkpoint = model.SyncCheckpoint{
			UserID:      userID,
			DeviceID:    deviceID,
			AppID:       appID,
			DataType:    dataType,
			LastSyncAt:  syncTime,
			LastVersion: version,
		}
		return model.DB.Create(&checkpoint).Error
	}

	checkpoint.LastSyncAt = syncTime
	checkpoint.LastVersion = version
	return model.DB.Save(&checkpoint).Error
}

// GetCheckpoint 获取同步检查点
func (s *DataSyncService) GetCheckpoint(userID, deviceID, appID, dataType string) (*model.SyncCheckpoint, error) {
	var checkpoint model.SyncCheckpoint
	err := model.DB.Where("user_id = ? AND device_id = ? AND app_id = ? AND data_type = ?",
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
	model.DB.Create(&log)
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
	stats := &SyncStats{}

	model.DB.Model(&model.UserConfig{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.ConfigCount)
	model.DB.Model(&model.UserWorkflow{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.WorkflowCount)
	model.DB.Model(&model.UserBatchTask{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.BatchTaskCount)
	model.DB.Model(&model.UserMaterial{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.MaterialCount)
	model.DB.Model(&model.UserPost{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.PostCount)
	model.DB.Model(&model.UserComment{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.CommentCount)
	model.DB.Model(&model.UserCommentScript{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.CommentScriptCount)
	model.DB.Model(&model.UserVoiceConfig{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Count(&stats.VoiceConfigCount)
	model.DB.Model(&model.SyncConflict{}).Where("user_id = ? AND app_id = ? AND status = ?", userID, appID, "pending").Count(&stats.PendingConflicts)

	// 计算存储使用量 (文件)
	var totalSize int64
	model.DB.Model(&model.UserFile{}).Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).
		Select("COALESCE(SUM(file_size), 0)").Scan(&totalSize)
	stats.StorageUsed = totalSize

	return stats, nil
}
