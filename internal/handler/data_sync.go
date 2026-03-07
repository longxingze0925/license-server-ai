package handler

import (
	"encoding/json"
	"errors"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"license-server/internal/service"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DataSyncHandler struct {
	service *service.DataSyncService
}

func NewDataSyncHandler() *DataSyncHandler {
	return &DataSyncHandler{
		service: service.NewDataSyncService(),
	}
}

// ==================== 同步 API ====================

// GetChanges 获取服务端变更 (Pull)
func (h *DataSyncHandler) GetChanges(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	dataType := c.Query("data_type")
	sinceStr := c.Query("since")
	limitStr := c.DefaultQuery("limit", "100")

	app, device, err := h.validateAppAndDevice(c, appKey, machineID)
	if err != nil {
		return
	}

	// 获取用户ID (通过授权或订阅)
	userID := h.getUserID(device)
	if userID == "" {
		response.Error(c, 401, "无法确定用户")
		return
	}

	// 解析时间
	var since time.Time
	if sinceStr != "" {
		sinceUnix, err := strconv.ParseInt(sinceStr, 10, 64)
		if err == nil {
			since = time.Unix(sinceUnix, 0)
		}
	}

	limit, _ := strconv.Atoi(limitStr)

	// 记录开始时间
	startTime := time.Now()

	// 获取变更
	items, err := h.service.GetChanges(userID, app.ID, dataType, since, limit)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}

	// 记录同步日志
	duration := time.Since(startTime).Milliseconds()
	h.service.LogSync(userID, device.ID, app.ID, model.SyncActionPull, dataType, "", len(items), "success", "", duration)

	response.Success(c, gin.H{
		"changes":     items,
		"count":       len(items),
		"has_more":    len(items) >= limit,
		"server_time": time.Now().Unix(),
	})
}

// PushChanges 推送客户端变更 (Push)
func (h *DataSyncHandler) PushChanges(c *gin.Context) {
	var req struct {
		AppKey    string             `json:"app_key" binding:"required"`
		MachineID string             `json:"machine_id" binding:"required"`
		Items     []service.PushItem `json:"items" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	app, device, err := h.validateAppAndDevice(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	userID := h.getUserID(device)
	if userID == "" {
		response.Error(c, 401, "无法确定用户")
		return
	}

	// 记录开始时间
	startTime := time.Now()

	// 推送变更
	results, err := h.service.PushChanges(userID, app.ID, device.ID, req.Items)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}

	// 统计结果
	successCount := 0
	conflictCount := 0
	errorCount := 0
	for _, r := range results {
		switch r.Status {
		case "success":
			successCount++
		case "conflict":
			conflictCount++
		default:
			errorCount++
		}
	}

	// 记录同步日志
	duration := time.Since(startTime).Milliseconds()
	status := "success"
	if conflictCount > 0 || errorCount > 0 {
		status = "partial"
	}
	h.service.LogSync(userID, device.ID, app.ID, model.SyncActionPush, "", "", len(req.Items), status, "", duration)

	response.Success(c, gin.H{
		"results":        results,
		"success_count":  successCount,
		"conflict_count": conflictCount,
		"error_count":    errorCount,
		"server_time":    time.Now().Unix(),
	})
}

// ResolveConflict 解决冲突
func (h *DataSyncHandler) ResolveConflict(c *gin.Context) {
	var req struct {
		AppKey     string          `json:"app_key" binding:"required"`
		MachineID  string          `json:"machine_id" binding:"required"`
		ConflictID string          `json:"conflict_id" binding:"required"`
		Resolution string          `json:"resolution" binding:"required"` // use_local/use_server/merge
		MergedData json.RawMessage `json:"merged_data"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	if req.Resolution != model.ConflictResolutionUseLocal &&
		req.Resolution != model.ConflictResolutionUseServer &&
		req.Resolution != model.ConflictResolutionMerge {
		response.BadRequest(c, "无效的解决方式")
		return
	}

	if req.Resolution == model.ConflictResolutionMerge && len(req.MergedData) == 0 {
		response.BadRequest(c, "合并方式需要提供 merged_data")
		return
	}

	if err := h.service.ResolveConflict(req.ConflictID, req.Resolution, req.MergedData); err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	response.SuccessWithMessage(c, "冲突已解决", nil)
}

// GetSyncStatus 获取同步状态
func (h *DataSyncHandler) GetSyncStatus(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")

	app, device, err := h.validateAppAndDevice(c, appKey, machineID)
	if err != nil {
		return
	}

	userID := h.getUserID(device)
	if userID == "" {
		response.Error(c, 401, "无法确定用户")
		return
	}

	stats, err := h.service.GetSyncStats(userID, app.ID)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}

	// 获取各类型的最后同步时间
	var checkpoints []model.SyncCheckpoint
	model.DB.Where("user_id = ? AND device_id = ? AND app_id = ?", userID, device.ID, app.ID).Find(&checkpoints)

	lastSyncMap := make(map[string]int64)
	for _, cp := range checkpoints {
		lastSyncMap[cp.DataType] = cp.LastSyncAt.Unix()
	}

	response.Success(c, gin.H{
		"stats":       stats,
		"last_sync":   lastSyncMap,
		"server_time": time.Now().Unix(),
	})
}

// ==================== 分类数据 API ====================

// GetConfigs 获取配置列表
func (h *DataSyncHandler) GetConfigs(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	var configs []model.UserConfig
	model.DB.Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).Find(&configs)

	result := make(map[string]interface{})
	for _, cfg := range configs {
		var value interface{}
		json.Unmarshal([]byte(cfg.ConfigValue), &value)
		if value == nil {
			value = cfg.ConfigValue
		}
		result[cfg.ConfigKey] = gin.H{
			"value":      value,
			"version":    cfg.Version,
			"updated_at": cfg.UpdatedAt.Unix(),
		}
	}

	response.Success(c, result)
}

// SaveConfig 保存配置
func (h *DataSyncHandler) SaveConfig(c *gin.Context) {
	var req struct {
		AppKey    string      `json:"app_key" binding:"required"`
		MachineID string      `json:"machine_id" binding:"required"`
		ConfigKey string      `json:"config_key" binding:"required"`
		Value     interface{} `json:"value" binding:"required"`
		Version   int64       `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	valueJSON, _ := json.Marshal(req.Value)
	item := service.PushItem{
		DataType:     model.DataTypeConfig,
		DataKey:      req.ConfigKey,
		Action:       "update",
		Data:         valueJSON,
		LocalVersion: req.Version,
	}

	results, _ := h.service.PushChanges(userID, appID, "", []service.PushItem{item})
	if len(results) > 0 {
		response.Success(c, results[0])
	} else {
		response.ServerError(c, "保存失败")
	}
}

// GetWorkflows 获取工作流列表
func (h *DataSyncHandler) GetWorkflows(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	var workflows []model.UserWorkflow
	model.DB.Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).
		Order("create_time DESC").Find(&workflows)

	response.Success(c, workflows)
}

// SaveWorkflow 保存工作流
func (h *DataSyncHandler) SaveWorkflow(c *gin.Context) {
	var req struct {
		AppKey    string             `json:"app_key" binding:"required"`
		MachineID string             `json:"machine_id" binding:"required"`
		Workflow  model.UserWorkflow `json:"workflow" binding:"required"`
		Version   int64              `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	workflowJSON, _ := json.Marshal(req.Workflow)
	item := service.PushItem{
		DataType:     model.DataTypeWorkflow,
		DataKey:      req.Workflow.WorkflowID,
		Action:       "update",
		Data:         workflowJSON,
		LocalVersion: req.Version,
	}

	results, _ := h.service.PushChanges(userID, appID, "", []service.PushItem{item})
	if len(results) > 0 {
		response.Success(c, results[0])
	} else {
		response.ServerError(c, "保存失败")
	}
}

// DeleteWorkflow 删除工作流
func (h *DataSyncHandler) DeleteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	item := service.PushItem{
		DataType:     model.DataTypeWorkflow,
		DataKey:      workflowID,
		Action:       "delete",
		Data:         nil,
		LocalVersion: 0,
	}

	h.service.PushChanges(userID, appID, "", []service.PushItem{item})
	response.SuccessWithMessage(c, "删除成功", nil)
}

// GetMaterials 获取素材列表
func (h *DataSyncHandler) GetMaterials(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	groupName := c.Query("group")
	status := c.Query("status")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	query := model.DB.Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false)
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var materials []model.UserMaterial
	query.Order("created_at DESC").Find(&materials)

	response.Success(c, materials)
}

// SaveMaterial 保存素材
func (h *DataSyncHandler) SaveMaterial(c *gin.Context) {
	var req struct {
		AppKey    string             `json:"app_key" binding:"required"`
		MachineID string             `json:"machine_id" binding:"required"`
		Material  model.UserMaterial `json:"material" binding:"required"`
		Version   int64              `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	materialJSON, _ := json.Marshal(req.Material)
	item := service.PushItem{
		DataType:     model.DataTypeMaterial,
		DataKey:      strconv.FormatInt(req.Material.MaterialID, 10),
		Action:       "update",
		Data:         materialJSON,
		LocalVersion: req.Version,
	}

	results, _ := h.service.PushChanges(userID, appID, "", []service.PushItem{item})
	if len(results) > 0 {
		response.Success(c, results[0])
	} else {
		response.ServerError(c, "保存失败")
	}
}

// SaveMaterialsBatch 批量保存素材
func (h *DataSyncHandler) SaveMaterialsBatch(c *gin.Context) {
	var req struct {
		AppKey    string               `json:"app_key" binding:"required"`
		MachineID string               `json:"machine_id" binding:"required"`
		Materials []model.UserMaterial `json:"materials" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	items := make([]service.PushItem, 0, len(req.Materials))
	for _, m := range req.Materials {
		materialJSON, _ := json.Marshal(m)
		items = append(items, service.PushItem{
			DataType:     model.DataTypeMaterial,
			DataKey:      strconv.FormatInt(m.MaterialID, 10),
			Action:       "update",
			Data:         materialJSON,
			LocalVersion: 0,
		})
	}

	results, _ := h.service.PushChanges(userID, appID, "", items)
	response.Success(c, gin.H{
		"results": results,
		"count":   len(results),
	})
}

// GetPosts 获取帖子列表
func (h *DataSyncHandler) GetPosts(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	postType := c.Query("type")
	groupName := c.Query("group")
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	query := model.DB.Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false)
	if postType != "" {
		query = query.Where("post_type = ?", postType)
	}
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Model(&model.UserPost{}).Count(&total)

	var posts []model.UserPost
	offset := (page - 1) * pageSize
	query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&posts)

	response.Success(c, gin.H{
		"list":      posts,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// SavePostsBatch 批量保存帖子
func (h *DataSyncHandler) SavePostsBatch(c *gin.Context) {
	var req struct {
		AppKey    string           `json:"app_key" binding:"required"`
		MachineID string           `json:"machine_id" binding:"required"`
		Posts     []model.UserPost `json:"posts" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	items := make([]service.PushItem, 0, len(req.Posts))
	for _, p := range req.Posts {
		postJSON, _ := json.Marshal(p)
		items = append(items, service.PushItem{
			DataType:     model.DataTypePost,
			DataKey:      p.ID,
			Action:       "update",
			Data:         postJSON,
			LocalVersion: 0,
		})
	}

	results, _ := h.service.PushChanges(userID, appID, "", items)
	response.Success(c, gin.H{
		"results": results,
		"count":   len(results),
	})
}

// UpdatePostStatus 更新帖子状态
func (h *DataSyncHandler) UpdatePostStatus(c *gin.Context) {
	postID := c.Param("id")

	var req struct {
		AppKey    string `json:"app_key" binding:"required"`
		MachineID string `json:"machine_id" binding:"required"`
		Status    string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	var post model.UserPost
	if err := model.DB.Where("id = ? AND user_id = ? AND app_id = ?", postID, userID, appID).First(&post).Error; err != nil {
		response.NotFound(c, "帖子不存在")
		return
	}

	post.Status = req.Status
	if req.Status == "used" {
		now := time.Now()
		post.UsedAt = &now
	}
	post.Version++
	model.DB.Save(&post)

	response.Success(c, gin.H{
		"version": post.Version,
	})
}

// GetCommentScripts 获取评论话术列表
func (h *DataSyncHandler) GetCommentScripts(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	groupName := c.Query("group")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	query := model.DB.Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false)
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}

	var scripts []model.UserCommentScript
	query.Order("created_at DESC").Find(&scripts)

	response.Success(c, scripts)
}

// SaveCommentScriptsBatch 批量保存评论话术
func (h *DataSyncHandler) SaveCommentScriptsBatch(c *gin.Context) {
	var req struct {
		AppKey    string                    `json:"app_key" binding:"required"`
		MachineID string                    `json:"machine_id" binding:"required"`
		Scripts   []model.UserCommentScript `json:"scripts" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	items := make([]service.PushItem, 0, len(req.Scripts))
	for _, s := range req.Scripts {
		scriptJSON, _ := json.Marshal(s)
		items = append(items, service.PushItem{
			DataType:     model.DataTypeCommentScript,
			DataKey:      s.ID,
			Action:       "update",
			Data:         scriptJSON,
			LocalVersion: 0,
		})
	}

	results, _ := h.service.PushChanges(userID, appID, "", items)
	response.Success(c, gin.H{
		"results": results,
		"count":   len(results),
	})
}

// GetPostGroups 获取帖子分组列表
func (h *DataSyncHandler) GetPostGroups(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	postType := c.Query("type")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	type GroupInfo struct {
		GroupName   string `json:"group_name"`
		PostType    string `json:"post_type"`
		TotalCount  int64  `json:"total_count"`
		UnusedCount int64  `json:"unused_count"`
		UsedCount   int64  `json:"used_count"`
	}

	query := model.DB.Model(&model.UserPost{}).
		Select("group_name, post_type, COUNT(*) as total_count, "+
			"SUM(CASE WHEN status = 'unused' THEN 1 ELSE 0 END) as unused_count, "+
			"SUM(CASE WHEN status = 'used' THEN 1 ELSE 0 END) as used_count").
		Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false)

	if postType != "" {
		query = query.Where("post_type = ?", postType)
	}

	var groups []GroupInfo
	query.Group("group_name, post_type").Find(&groups)

	response.Success(c, groups)
}

// ==================== 通用表数据 API ====================

// GetTableData 获取指定表的数据
func (h *DataSyncHandler) GetTableData(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	tableName := c.Query("table")
	sinceStr := c.Query("since") // 增量同步时间戳

	if tableName == "" {
		response.BadRequest(c, "缺少 table 参数")
		return
	}

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	query := model.DB.Where("user_id = ? AND app_id = ? AND table_name = ?", userID, appID, tableName)

	// 增量同步
	if sinceStr != "" {
		sinceUnix, err := strconv.ParseInt(sinceStr, 10, 64)
		if err == nil {
			since := time.Unix(sinceUnix, 0)
			query = query.Where("updated_at > ?", since)
		}
	}

	var records []model.UserTableData
	query.Order("updated_at ASC").Find(&records)

	// 转换为更友好的格式
	result := make([]map[string]interface{}, 0, len(records))
	for _, r := range records {
		var data map[string]interface{}
		json.Unmarshal([]byte(r.Data), &data)
		result = append(result, map[string]interface{}{
			"id":         r.RecordID,
			"data":       data,
			"version":    r.Version,
			"is_deleted": r.IsDeleted,
			"updated_at": r.UpdatedAt.Unix(),
		})
	}

	response.Success(c, gin.H{
		"table":       tableName,
		"records":     result,
		"count":       len(result),
		"server_time": time.Now().Unix(),
	})
}

// SaveTableData 保存表数据（单条）
func (h *DataSyncHandler) SaveTableData(c *gin.Context) {
	var req struct {
		AppKey    string                 `json:"app_key" binding:"required"`
		MachineID string                 `json:"machine_id" binding:"required"`
		Table     string                 `json:"table" binding:"required"`
		RecordID  string                 `json:"record_id" binding:"required"`
		Data      map[string]interface{} `json:"data" binding:"required"`
		Version   int64                  `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	dataJSON, _ := json.Marshal(req.Data)

	// 查找现有记录
	var existing model.UserTableData
	result := model.DB.Where("user_id = ? AND app_id = ? AND table_name = ? AND record_id = ?",
		userID, appID, req.Table, req.RecordID).First(&existing)

	if result.Error == nil {
		// 更新现有记录
		if req.Version > 0 && existing.Version > req.Version {
			// 版本冲突
			response.Success(c, gin.H{
				"status":         "conflict",
				"server_version": existing.Version,
				"server_data":    existing.Data,
			})
			return
		}
		existing.Data = string(dataJSON)
		existing.Version++
		existing.IsDeleted = false
		model.DB.Save(&existing)
		response.Success(c, gin.H{
			"status":  "success",
			"version": existing.Version,
		})
	} else {
		// 创建新记录
		newRecord := model.UserTableData{
			UserID:      userID,
			AppID:       appID,
			SourceTable: req.Table,
			RecordID:    req.RecordID,
			Data:        string(dataJSON),
			Version:     1,
			IsDeleted:   false,
		}
		model.DB.Create(&newRecord)
		response.Success(c, gin.H{
			"status":  "success",
			"version": newRecord.Version,
		})
	}
}

// SaveTableDataBatch 批量保存表数据
func (h *DataSyncHandler) SaveTableDataBatch(c *gin.Context) {
	var req struct {
		AppKey    string `json:"app_key" binding:"required"`
		MachineID string `json:"machine_id" binding:"required"`
		Table     string `json:"table" binding:"required"`
		Records   []struct {
			RecordID string                 `json:"record_id"`
			Data     map[string]interface{} `json:"data"`
			Version  int64                  `json:"version"`
			Deleted  bool                   `json:"deleted"`
		} `json:"records" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	results := make([]map[string]interface{}, 0, len(req.Records))

	for _, record := range req.Records {
		dataJSON, _ := json.Marshal(record.Data)

		var existing model.UserTableData
		result := model.DB.Where("user_id = ? AND app_id = ? AND table_name = ? AND record_id = ?",
			userID, appID, req.Table, record.RecordID).First(&existing)

		if result.Error == nil {
			// 更新
			if record.Version > 0 && existing.Version > record.Version {
				results = append(results, map[string]interface{}{
					"record_id":      record.RecordID,
					"status":         "conflict",
					"server_version": existing.Version,
				})
				continue
			}
			existing.Data = string(dataJSON)
			existing.Version++
			existing.IsDeleted = record.Deleted
			model.DB.Save(&existing)
			results = append(results, map[string]interface{}{
				"record_id": record.RecordID,
				"status":    "success",
				"version":   existing.Version,
			})
		} else {
			// 创建
			newRecord := model.UserTableData{
				UserID:      userID,
				AppID:       appID,
				SourceTable: req.Table,
				RecordID:    record.RecordID,
				Data:        string(dataJSON),
				Version:     1,
				IsDeleted:   record.Deleted,
			}
			model.DB.Create(&newRecord)
			results = append(results, map[string]interface{}{
				"record_id": record.RecordID,
				"status":    "success",
				"version":   newRecord.Version,
			})
		}
	}

	response.Success(c, gin.H{
		"table":       req.Table,
		"results":     results,
		"count":       len(results),
		"server_time": time.Now().Unix(),
	})
}

// DeleteTableData 删除表数据
func (h *DataSyncHandler) DeleteTableData(c *gin.Context) {
	var req struct {
		AppKey    string `json:"app_key" binding:"required"`
		MachineID string `json:"machine_id" binding:"required"`
		Table     string `json:"table" binding:"required"`
		RecordID  string `json:"record_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return
	}

	// 软删除
	model.DB.Model(&model.UserTableData{}).
		Where("user_id = ? AND app_id = ? AND table_name = ? AND record_id = ?",
			userID, appID, req.Table, req.RecordID).
		Updates(map[string]interface{}{
			"is_deleted": true,
			"version":    gorm.Expr("version + 1"),
		})

	response.SuccessWithMessage(c, "删除成功", nil)
}

// GetTableList 获取用户所有表名列表
func (h *DataSyncHandler) GetTableList(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	type TableInfo struct {
		TableName   string `json:"table_name"`
		RecordCount int64  `json:"record_count"`
		LastUpdated string `json:"last_updated"`
	}

	var tables []TableInfo
	model.DB.Model(&model.UserTableData{}).
		Select("table_name, COUNT(*) as record_count, MAX(updated_at) as last_updated").
		Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).
		Group("table_name").
		Find(&tables)

	response.Success(c, tables)
}

// SyncAllTables 全量同步所有表数据
func (h *DataSyncHandler) SyncAllTables(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	sinceStr := c.Query("since")

	userID, appID, err := h.validateAndGetUser(c, appKey, machineID)
	if err != nil {
		return
	}

	query := model.DB.Where("user_id = ? AND app_id = ?", userID, appID)

	if sinceStr != "" {
		sinceUnix, err := strconv.ParseInt(sinceStr, 10, 64)
		if err == nil {
			since := time.Unix(sinceUnix, 0)
			query = query.Where("updated_at > ?", since)
		}
	}

	var records []model.UserTableData
	query.Order("table_name, updated_at ASC").Find(&records)

	// 按表名分组
	tableData := make(map[string][]map[string]interface{})
	for _, r := range records {
		var data map[string]interface{}
		json.Unmarshal([]byte(r.Data), &data)
		item := map[string]interface{}{
			"id":         r.RecordID,
			"data":       data,
			"version":    r.Version,
			"is_deleted": r.IsDeleted,
			"updated_at": r.UpdatedAt.Unix(),
		}
		tableData[r.SourceTable] = append(tableData[r.SourceTable], item)
	}

	response.Success(c, gin.H{
		"tables":      tableData,
		"server_time": time.Now().Unix(),
	})
}

// ==================== 辅助方法 ====================

func (h *DataSyncHandler) getUserID(device *model.Device) string {
	// 优先从订阅获取客户ID
	if device.SubscriptionID != nil && *device.SubscriptionID != "" {
		var sub model.Subscription
		if err := model.DB.First(&sub, "id = ?", *device.SubscriptionID).Error; err == nil {
			return sub.CustomerID
		}
	}

	// 从授权获取客户ID
	if device.LicenseID != nil && *device.LicenseID != "" {
		var license model.License
		if err := model.DB.First(&license, "id = ?", *device.LicenseID).Error; err == nil {
			if license.CustomerID != nil {
				return *license.CustomerID
			}
		}
	}

	// 直接使用设备关联的客户
	if device.CustomerID != "" {
		return device.CustomerID
	}

	return ""
}

func (h *DataSyncHandler) validateAndGetUser(c *gin.Context, appKey, machineID string) (userID, appID string, err error) {
	app, device, err := h.validateAppAndDevice(c, appKey, machineID)
	if err != nil {
		return "", "", err
	}

	userID = h.getUserID(device)
	if userID == "" {
		response.Error(c, 401, "无法确定用户")
		return "", "", errors.New("user not found")
	}

	return userID, app.ID, nil
}

func (h *DataSyncHandler) validateAppAndDevice(c *gin.Context, appKey, machineID string) (*model.Application, *model.Device, error) {
	if appKey == "" || machineID == "" {
		response.BadRequest(c, "缺少 app_key 或 machine_id")
		return nil, nil, errors.New("missing params")
	}

	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", appKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return nil, nil, err
	}

	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ?", machineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 401, "设备未授权")
		return nil, nil, err
	}

	if (device.License == nil || device.License.AppID != app.ID) &&
		(device.Subscription == nil || device.Subscription.AppID != app.ID) {
		response.Error(c, 401, "设备未绑定当前应用")
		return nil, nil, errors.New("device app mismatch")
	}

	return &app, &device, nil
}
