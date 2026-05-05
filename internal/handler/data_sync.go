package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"license-server/internal/service"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DataSyncHandler struct {
	service *service.DataSyncService
}

const (
	maxSyncIdentifierLength = 100
	defaultSyncPageSize     = 100
	maxSyncPageSize         = 500
)

var syncIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func NewDataSyncHandler() *DataSyncHandler {
	return &DataSyncHandler{
		service: service.NewDataSyncService(),
	}
}

func parseBoundedInt(value string, fallback, min, max int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		parsed = fallback
	}
	if parsed < min {
		parsed = fallback
	}
	if parsed > max {
		parsed = max
	}
	return parsed
}

func parseSyncPage(c *gin.Context, defaultSize, maxSize int) (int, int) {
	page := parseBoundedInt(c.DefaultQuery("page", "1"), 1, 1, 1_000_000)
	pageSize := parseBoundedInt(c.DefaultQuery("page_size", strconv.Itoa(defaultSize)), defaultSize, 1, maxSize)
	return page, pageSize
}

func validateNonEmptySyncKey(field, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

// ==================== 同步 API ====================

// GetChanges 获取服务端变更 (Pull)
func (h *DataSyncHandler) GetChanges(c *gin.Context) {
	dataType := c.Query("data_type")
	sinceStr := c.Query("since")
	limit := parseBoundedInt(c.DefaultQuery("limit", strconv.Itoa(defaultSyncPageSize)), defaultSyncPageSize, 1, maxSyncPageSize)
	offset := parseBoundedInt(c.DefaultQuery("offset", "0"), 0, 0, 10_000_000)

	app, device, err := h.validateAppAndDevice(c)
	if err != nil {
		return
	}

	// 获取用户ID (通过授权或订阅)
	userID := h.resolveUserID(c, device, app)
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

	// 记录开始时间
	startTime := time.Now()

	// 获取变更
	page, err := h.service.GetChangesPage(userID, app.ID, dataType, since, limit, offset)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}

	// 记录同步日志
	duration := time.Since(startTime).Milliseconds()
	h.service.LogSync(userID, device.ID, app.ID, model.SyncActionPull, dataType, "", len(page.Items), "success", "", duration)
	if !page.HasMore {
		_ = h.service.UpdateCheckpoint(userID, device.ID, app.ID, dataType, time.Now(), 0)
	}

	response.Success(c, gin.H{
		"changes":     page.Items,
		"count":       len(page.Items),
		"has_more":    page.HasMore,
		"next_offset": page.NextOffset,
		"limit":       page.Limit,
		"offset":      page.Offset,
		"server_time": time.Now().Unix(),
	})
}

// PushChanges 推送客户端变更 (Push)
func (h *DataSyncHandler) PushChanges(c *gin.Context) {
	var req struct {
		Items []service.PushItem `json:"items" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	app, device, err := h.validateAppAndDevice(c)
	if err != nil {
		return
	}

	userID := h.resolveUserID(c, device, app)
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
	if successCount > 0 {
		now := time.Now()
		_ = h.service.UpdateCheckpoint(userID, device.ID, app.ID, "", now, 0)
		updatedTypes := map[string]bool{}
		for _, item := range req.Items {
			if item.DataType != "" && !updatedTypes[item.DataType] {
				_ = h.service.UpdateCheckpoint(userID, device.ID, app.ID, item.DataType, now, 0)
				updatedTypes[item.DataType] = true
			}
		}
	}

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

	userID, appID, deviceID, err := h.validateAndGetUserWithDevice(c)
	if err != nil {
		return
	}

	if err := h.service.ResolveConflict(req.ConflictID, userID, appID, deviceID, req.Resolution, req.MergedData); err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	response.SuccessWithMessage(c, "冲突已解决", nil)
}

// GetSyncStatus 获取同步状态
func (h *DataSyncHandler) GetSyncStatus(c *gin.Context) {

	app, device, err := h.validateAppAndDevice(c)
	if err != nil {
		return
	}

	userID := h.resolveUserID(c, device, app)
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
	var lastSyncTime int64
	for _, cp := range checkpoints {
		lastSyncMap[cp.DataType] = cp.LastSyncAt.Unix()
		if cp.LastSyncAt.Unix() > lastSyncTime {
			lastSyncTime = cp.LastSyncAt.Unix()
		}
	}

	response.Success(c, gin.H{
		"stats":           stats,
		"last_sync":       lastSyncMap,
		"last_sync_time":  lastSyncTime,
		"table_status":    lastSyncMap,
		"pending_changes": stats.PendingConflicts,
		"server_time":     time.Now().Unix(),
	})
}

// ==================== 分类数据 API ====================

// GetConfigs 获取配置列表
func (h *DataSyncHandler) GetConfigs(c *gin.Context) {

	userID, appID, err := h.validateAndGetUser(c)
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
		ConfigKey string      `json:"config_key" binding:"required"`
		Value     interface{} `json:"value" binding:"required"`
		Version   int64       `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
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

	h.respondSinglePushResult(c, userID, appID, item, "保存配置失败")
}

// GetWorkflows 获取工作流列表
func (h *DataSyncHandler) GetWorkflows(c *gin.Context) {

	userID, appID, err := h.validateAndGetUser(c)
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
		Workflow model.UserWorkflow `json:"workflow" binding:"required"`
		Version  int64              `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}
	workflowID, ok := validateNonEmptySyncKey("workflow_id", req.Workflow.WorkflowID)
	if !ok {
		response.BadRequest(c, "工作流ID不能为空")
		return
	}
	req.Workflow.WorkflowID = workflowID

	workflowJSON, _ := json.Marshal(req.Workflow)
	item := service.PushItem{
		DataType:     model.DataTypeWorkflow,
		DataKey:      workflowID,
		Action:       "update",
		Data:         workflowJSON,
		LocalVersion: req.Version,
	}

	h.respondSinglePushResult(c, userID, appID, item, "保存工作流失败")
}

// DeleteWorkflow 删除工作流
func (h *DataSyncHandler) DeleteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	userID, appID, err := h.validateAndGetUser(c)
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

	h.respondSinglePushResult(c, userID, appID, item, "删除工作流失败")
}

// GetMaterials 获取素材列表
func (h *DataSyncHandler) GetMaterials(c *gin.Context) {
	groupName := c.Query("group")
	status := c.Query("status")

	userID, appID, err := h.validateAndGetUser(c)
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
		Material model.UserMaterial `json:"material" binding:"required"`
		Version  int64              `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}
	if req.Material.MaterialID <= 0 {
		response.BadRequest(c, "素材ID不能为空")
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

	h.respondSinglePushResult(c, userID, appID, item, "保存素材失败")
}

// SaveMaterialsBatch 批量保存素材
func (h *DataSyncHandler) SaveMaterialsBatch(c *gin.Context) {
	var req struct {
		Materials []model.UserMaterial `json:"materials" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	items := make([]service.PushItem, 0, len(req.Materials))
	for _, m := range req.Materials {
		if m.MaterialID <= 0 {
			response.BadRequest(c, "素材ID不能为空")
			return
		}
		materialJSON, _ := json.Marshal(m)
		items = append(items, service.PushItem{
			DataType:     model.DataTypeMaterial,
			DataKey:      strconv.FormatInt(m.MaterialID, 10),
			Action:       "update",
			Data:         materialJSON,
			LocalVersion: m.Version,
		})
	}

	h.respondBatchPushResults(c, userID, appID, items, "批量保存素材失败")
}

// GetPosts 获取帖子列表
func (h *DataSyncHandler) GetPosts(c *gin.Context) {
	postType := c.Query("type")
	groupName := c.Query("group")
	status := c.Query("status")
	page, pageSize := parseSyncPage(c, 100, maxSyncPageSize)

	userID, appID, err := h.validateAndGetUser(c)
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
		Posts []model.UserPost `json:"posts" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	items := make([]service.PushItem, 0, len(req.Posts))
	for _, p := range req.Posts {
		postID, ok := validateNonEmptySyncKey("post_id", p.ID)
		if !ok {
			response.BadRequest(c, "帖子ID不能为空")
			return
		}
		p.ID = postID
		postJSON, _ := json.Marshal(p)
		items = append(items, service.PushItem{
			DataType:     model.DataTypePost,
			DataKey:      postID,
			Action:       "update",
			Data:         postJSON,
			LocalVersion: p.Version,
		})
	}

	h.respondBatchPushResults(c, userID, appID, items, "批量保存帖子失败")
}

// UpdatePostStatus 更新帖子状态
func (h *DataSyncHandler) UpdatePostStatus(c *gin.Context) {
	postID := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
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
	if err := model.DB.Save(&post).Error; err != nil {
		response.ServerError(c, "更新帖子状态失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"version": post.Version,
	})
}

// GetCommentScripts 获取评论话术列表
func (h *DataSyncHandler) GetCommentScripts(c *gin.Context) {
	groupName := c.Query("group")

	userID, appID, err := h.validateAndGetUser(c)
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
		Scripts []model.UserCommentScript `json:"scripts" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	items := make([]service.PushItem, 0, len(req.Scripts))
	for _, s := range req.Scripts {
		scriptID, ok := validateNonEmptySyncKey("script_id", s.ID)
		if !ok {
			response.BadRequest(c, "评论话术ID不能为空")
			return
		}
		s.ID = scriptID
		scriptJSON, _ := json.Marshal(s)
		items = append(items, service.PushItem{
			DataType:     model.DataTypeCommentScript,
			DataKey:      scriptID,
			Action:       "update",
			Data:         scriptJSON,
			LocalVersion: s.Version,
		})
	}

	h.respondBatchPushResults(c, userID, appID, items, "批量保存评论话术失败")
}

// GetPostGroups 获取帖子分组列表
func (h *DataSyncHandler) GetPostGroups(c *gin.Context) {
	postType := c.Query("type")

	userID, appID, err := h.validateAndGetUser(c)
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

func (h *DataSyncHandler) respondSinglePushResult(c *gin.Context, userID, appID string, item service.PushItem, fallback string) {
	results, ok := h.pushAndCheckResults(c, userID, appID, []service.PushItem{item}, fallback)
	if !ok {
		return
	}
	response.Success(c, results[0])
}

func (h *DataSyncHandler) respondBatchPushResults(c *gin.Context, userID, appID string, items []service.PushItem, fallback string) {
	results, ok := h.pushAndCheckResults(c, userID, appID, items, fallback)
	if !ok {
		return
	}
	response.Success(c, gin.H{
		"results": results,
		"count":   len(results),
	})
}

func (h *DataSyncHandler) pushAndCheckResults(c *gin.Context, userID, appID string, items []service.PushItem, fallback string) ([]service.SyncResult, bool) {
	results, err := h.service.PushChanges(userID, appID, "", items)
	if err != nil {
		response.ServerError(c, err.Error())
		return nil, false
	}
	if len(results) == 0 && len(items) > 0 {
		response.ServerError(c, fallback)
		return nil, false
	}
	for _, result := range results {
		if result.Status == "error" {
			message := result.Error
			if message == "" {
				message = fallback
			}
			response.ServerError(c, message)
			return nil, false
		}
	}
	deviceID := middleware.GetClientDeviceID(c)
	if deviceID != "" {
		now := time.Now()
		updatedTypes := map[string]bool{}
		for _, item := range items {
			if item.DataType != "" && !updatedTypes[item.DataType] {
				_ = h.service.UpdateCheckpoint(userID, deviceID, appID, item.DataType, now, 0)
				updatedTypes[item.DataType] = true
			}
		}
	}
	return results, true
}

// ==================== 通用表数据 API ====================

func normalizeSyncIdentifier(value string) string {
	return strings.TrimSpace(value)
}

func validateSyncIdentifier(field, value string) error {
	if value == "" {
		return fmt.Errorf("缺少 %s 参数", field)
	}
	if len(value) > maxSyncIdentifierLength {
		return fmt.Errorf("%s 长度不能超过 %d 字符", field, maxSyncIdentifierLength)
	}
	if !syncIdentifierPattern.MatchString(value) {
		return fmt.Errorf("%s 只能包含字母、数字、下划线、点和横线", field)
	}
	return nil
}

// GetTableData 获取指定表的数据
func (h *DataSyncHandler) GetTableData(c *gin.Context) {
	tableName := normalizeSyncIdentifier(c.Query("table"))
	sinceStr := c.Query("since") // 增量同步时间戳

	if err := validateSyncIdentifier("table", tableName); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
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
	if err := query.Order("updated_at ASC").Find(&records).Error; err != nil {
		response.ServerError(c, "获取表数据失败: "+err.Error())
		return
	}

	// 转换为更友好的格式
	result := make([]map[string]interface{}, 0, len(records))
	for _, r := range records {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(r.Data), &data); err != nil {
			data = map[string]interface{}{}
		}
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
		Table    string                 `json:"table" binding:"required"`
		RecordID string                 `json:"record_id" binding:"required"`
		Data     map[string]interface{} `json:"data" binding:"required"`
		Version  int64                  `json:"version"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	req.Table = normalizeSyncIdentifier(req.Table)
	req.RecordID = normalizeSyncIdentifier(req.RecordID)
	if err := validateSyncIdentifier("table", req.Table); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := validateSyncIdentifier("record_id", req.RecordID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	dataJSON, err := json.Marshal(req.Data)
	if err != nil {
		response.BadRequest(c, "数据序列化失败: "+err.Error())
		return
	}

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
		if err := model.DB.Save(&existing).Error; err != nil {
			response.ServerError(c, "保存表数据失败: "+err.Error())
			return
		}
		response.Success(c, gin.H{
			"status":  "success",
			"version": existing.Version,
		})
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		response.ServerError(c, "查询表数据失败: "+result.Error.Error())
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
		if err := model.DB.Create(&newRecord).Error; err != nil {
			response.ServerError(c, "创建表数据失败: "+err.Error())
			return
		}
		response.Success(c, gin.H{
			"status":  "success",
			"version": newRecord.Version,
		})
	}
}

// SaveTableDataBatch 批量保存表数据
func (h *DataSyncHandler) SaveTableDataBatch(c *gin.Context) {
	var req struct {
		Table   string `json:"table" binding:"required"`
		Records []struct {
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
	req.Table = normalizeSyncIdentifier(req.Table)
	if err := validateSyncIdentifier("table", req.Table); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	results := make([]map[string]interface{}, 0, len(req.Records))

	for _, record := range req.Records {
		record.RecordID = normalizeSyncIdentifier(record.RecordID)
		if err := validateSyncIdentifier("record_id", record.RecordID); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		dataJSON, err := json.Marshal(record.Data)
		if err != nil {
			response.BadRequest(c, "数据序列化失败: "+err.Error())
			return
		}

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
			if err := model.DB.Save(&existing).Error; err != nil {
				response.ServerError(c, "保存表数据失败: "+err.Error())
				return
			}
			results = append(results, map[string]interface{}{
				"record_id": record.RecordID,
				"status":    "success",
				"version":   existing.Version,
			})
		} else if errors.Is(result.Error, gorm.ErrRecordNotFound) {
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
			if err := model.DB.Create(&newRecord).Error; err != nil {
				response.ServerError(c, "创建表数据失败: "+err.Error())
				return
			}
			results = append(results, map[string]interface{}{
				"record_id": record.RecordID,
				"status":    "success",
				"version":   newRecord.Version,
			})
		} else {
			response.ServerError(c, "查询表数据失败: "+result.Error.Error())
			return
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
		Table    string `json:"table" binding:"required"`
		RecordID string `json:"record_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	req.Table = normalizeSyncIdentifier(req.Table)
	req.RecordID = normalizeSyncIdentifier(req.RecordID)
	if err := validateSyncIdentifier("table", req.Table); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := validateSyncIdentifier("record_id", req.RecordID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	// 软删除
	result := model.DB.Model(&model.UserTableData{}).
		Where("user_id = ? AND app_id = ? AND table_name = ? AND record_id = ?",
			userID, appID, req.Table, req.RecordID).
		Updates(map[string]interface{}{
			"is_deleted": true,
			"version":    gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		response.ServerError(c, "删除表数据失败: "+result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		response.NotFound(c, "记录不存在")
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
}

// GetTableList 获取用户所有表名列表
func (h *DataSyncHandler) GetTableList(c *gin.Context) {

	userID, appID, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	type TableInfo struct {
		TableName   string `json:"table_name"`
		RecordCount int64  `json:"record_count"`
		LastUpdated string `json:"last_updated"`
	}

	var tables []TableInfo
	if err := model.DB.Model(&model.UserTableData{}).
		Select("table_name, COUNT(*) as record_count, MAX(updated_at) as last_updated").
		Where("user_id = ? AND app_id = ? AND is_deleted = ?", userID, appID, false).
		Group("table_name").
		Find(&tables).Error; err != nil {
		response.ServerError(c, "获取表列表失败: "+err.Error())
		return
	}

	response.Success(c, tables)
}

// SyncAllTables 全量同步所有表数据
func (h *DataSyncHandler) SyncAllTables(c *gin.Context) {
	sinceStr := c.Query("since")

	userID, appID, err := h.validateAndGetUser(c)
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

func (h *DataSyncHandler) resolveUserID(c *gin.Context, device *model.Device, app *model.Application) string {
	if customerID := middleware.GetClientCustomerID(c); customerID != "" {
		if h.isActiveCustomer(customerID, app.TenantID) {
			return customerID
		}
		return ""
	}
	customerID := h.getUserID(device, app)
	if customerID != "" && h.isActiveCustomer(customerID, app.TenantID) {
		return customerID
	}
	return ""
}

func (h *DataSyncHandler) isActiveCustomer(customerID, tenantID string) bool {
	var customer model.Customer
	return model.DB.First(&customer, "id = ? AND tenant_id = ? AND status = ?", customerID, tenantID, model.CustomerStatusActive).Error == nil
}

func (h *DataSyncHandler) getUserID(device *model.Device, app *model.Application) string {
	// 优先从订阅获取客户ID
	if device.SubscriptionID != nil && *device.SubscriptionID != "" {
		var sub model.Subscription
		if err := model.DB.First(&sub, "id = ? AND tenant_id = ? AND app_id = ?",
			*device.SubscriptionID, app.TenantID, app.ID).Error; err == nil {
			return sub.CustomerID
		}
	}

	// 从授权获取客户ID
	if device.LicenseID != nil && *device.LicenseID != "" {
		var license model.License
		if err := model.DB.First(&license, "id = ? AND tenant_id = ? AND app_id = ?",
			*device.LicenseID, app.TenantID, app.ID).Error; err == nil {
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

func (h *DataSyncHandler) validateAndGetUser(c *gin.Context) (userID, appID string, err error) {
	userID, appID, _, err = h.validateAndGetUserWithDevice(c)
	return userID, appID, err
}

func (h *DataSyncHandler) validateAndGetUserWithDevice(c *gin.Context) (userID, appID, deviceID string, err error) {
	app, device, err := h.validateAppAndDevice(c)
	if err != nil {
		return "", "", "", err
	}

	userID = h.resolveUserID(c, device, app)
	if userID == "" {
		response.Error(c, 401, "无法确定用户")
		return "", "", "", errors.New("user not found")
	}

	return userID, app.ID, device.ID, nil
}

func (h *DataSyncHandler) validateAppAndDevice(c *gin.Context) (*model.Application, *model.Device, error) {
	tenantID := middleware.GetClientTenantID(c)
	appID := middleware.GetClientAppID(c)
	deviceID := middleware.GetClientDeviceID(c)
	sessionMachineID := middleware.GetClientMachineID(c)
	authMode := middleware.GetClientAuthMode(c)
	if tenantID == "" || appID == "" || deviceID == "" || sessionMachineID == "" {
		response.Unauthorized(c, "客户端会话无效")
		return nil, nil, errors.New("invalid client session")
	}

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ? AND status = ?", appID, tenantID, model.AppStatusActive).Error; err != nil {
		response.Error(c, 401, "应用已失效，请重新登录")
		return nil, nil, err
	}

	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		First(&device, "id = ? AND tenant_id = ? AND machine_id = ?", deviceID, tenantID, sessionMachineID).Error; err != nil {
		response.Error(c, 401, "设备已解绑，请重新登录")
		return nil, nil, err
	}
	if device.Status == model.DeviceStatusBlacklisted {
		response.Error(c, 401, "设备已被禁止使用")
		return nil, nil, errors.New("device blacklisted")
	}

	switch authMode {
	case "license":
		if device.LicenseID == nil || *device.LicenseID == "" {
			response.Error(c, 401, "授权已失效，请重新激活")
			return nil, nil, errors.New("missing license")
		}
		var license model.License
		if device.License != nil {
			license = *device.License
		} else if err := model.DB.First(&license, "id = ? AND tenant_id = ?", *device.LicenseID, tenantID).Error; err != nil {
			response.Error(c, 401, "授权已失效，请重新激活")
			return nil, nil, err
		}
		if license.AppID != app.ID || !license.IsValid() {
			response.Error(c, 401, "授权无效，请重新激活")
			return nil, nil, errors.New("invalid license")
		}
	case "subscription":
		if device.SubscriptionID == nil || *device.SubscriptionID == "" {
			response.Error(c, 401, "订阅已失效，请重新登录")
			return nil, nil, errors.New("missing subscription")
		}
		var subscription model.Subscription
		if device.Subscription != nil {
			subscription = *device.Subscription
		} else if err := model.DB.First(&subscription, "id = ? AND tenant_id = ?", *device.SubscriptionID, tenantID).Error; err != nil {
			response.Error(c, 401, "订阅已失效，请重新登录")
			return nil, nil, err
		}
		if subscription.AppID != app.ID || !subscription.IsValid() {
			response.Error(c, 401, "订阅无效，请重新登录")
			return nil, nil, errors.New("invalid subscription")
		}
		if customerID := middleware.GetClientCustomerID(c); customerID != "" && customerID != subscription.CustomerID {
			response.Error(c, 401, "订阅归属已变更，请重新登录")
			return nil, nil, errors.New("subscription owner mismatch")
		}
	default:
		response.Error(c, 401, "会话模式无效，请重新登录")
		return nil, nil, errors.New("invalid auth mode")
	}

	return &app, &device, nil
}
