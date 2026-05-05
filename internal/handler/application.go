package handler

import (
	"encoding/json"
	"license-server/internal/config"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"license-server/internal/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ApplicationHandler struct{}

func NewApplicationHandler() *ApplicationHandler {
	return &ApplicationHandler{}
}

// CreateAppRequest 创建应用请求
type CreateAppRequest struct {
	Name              string   `json:"name" binding:"required"`
	Description       string   `json:"description"`
	AuthMode          string   `json:"auth_mode"`
	HeartbeatInterval int      `json:"heartbeat_interval"`
	OfflineTolerance  int      `json:"offline_tolerance"`
	MaxDevicesDefault int      `json:"max_devices_default"`
	GracePeriodDays   int      `json:"grace_period_days"`
	Features          []string `json:"features"` // 功能列表
}

// Create 创建应用
func (h *ApplicationHandler) Create(c *gin.Context) {
	var req CreateAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if req.HeartbeatInterval < 0 {
		response.BadRequest(c, "心跳间隔不能小于 0")
		return
	}
	if req.OfflineTolerance < 0 {
		response.BadRequest(c, "离线容忍时间不能小于 0")
		return
	}
	if req.MaxDevicesDefault < 0 {
		response.BadRequest(c, "默认最大设备数不能小于 0")
		return
	}
	if req.GracePeriodDays < 0 {
		response.BadRequest(c, "宽限天数不能小于 0")
		return
	}

	// 生成 RSA 密钥对
	publicKey, privateKey, err := crypto.GenerateRSAKeyPair(config.Get().RSA.KeySize)
	if err != nil {
		response.ServerError(c, "生成密钥对失败")
		return
	}

	// 获取租户ID
	tenantID := middleware.GetTenantID(c)

	app := model.Application{
		TenantID:          tenantID,
		Name:              req.Name,
		AppKey:            utils.GenerateAppKey(),
		AppSecret:         utils.GenerateAppSecret(),
		PublicKey:         publicKey,
		PrivateKey:        privateKey,
		Description:       req.Description,
		AuthMode:          model.AuthModeBoth,
		HeartbeatInterval: req.HeartbeatInterval,
		OfflineTolerance:  req.OfflineTolerance,
		MaxDevicesDefault: req.MaxDevicesDefault,
		GracePeriodDays:   req.GracePeriodDays,
		Status:            model.AppStatusActive,
	}
	if req.AuthMode != "" {
		if !isValidAppAuthMode(req.AuthMode) {
			response.BadRequest(c, "授权模式不支持")
			return
		}
		app.AuthMode = model.AuthMode(req.AuthMode)
	}

	// 处理功能列表
	if len(req.Features) > 0 {
		featuresJSON, _ := json.Marshal(req.Features)
		app.Features = string(featuresJSON)
	} else {
		app.Features = "[]" // 空数组作为默认值
	}

	// 设置默认值
	if app.HeartbeatInterval == 0 {
		app.HeartbeatInterval = 3600
	}
	if app.OfflineTolerance == 0 {
		app.OfflineTolerance = 86400
	}
	if app.MaxDevicesDefault == 0 {
		app.MaxDevicesDefault = 1
	}
	if app.GracePeriodDays == 0 {
		app.GracePeriodDays = 3
	}

	if err := model.DB.Create(&app).Error; err != nil {
		response.ServerError(c, "创建应用失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"id":         app.ID,
		"name":       app.Name,
		"app_key":    app.AppKey,
		"auth_mode":  app.AuthMode,
		"app_secret": app.AppSecret,
		"public_key": app.PublicKey,
		"created_at": app.CreatedAt,
	})
}

// List 获取应用列表
func (h *ApplicationHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var apps []model.Application
	if err := model.DB.Where("tenant_id = ?", tenantID).Find(&apps).Error; err != nil {
		response.ServerError(c, "获取应用列表失败")
		return
	}

	var result []gin.H
	for _, app := range apps {
		// 解析功能列表
		var features []string
		if app.Features != "" {
			json.Unmarshal([]byte(app.Features), &features)
		}
		result = append(result, gin.H{
			"id":                  app.ID,
			"name":                app.Name,
			"app_key":             app.AppKey,
			"auth_mode":           app.AuthMode,
			"description":         app.Description,
			"heartbeat_interval":  app.HeartbeatInterval,
			"offline_tolerance":   app.OfflineTolerance,
			"max_devices_default": app.MaxDevicesDefault,
			"grace_period_days":   app.GracePeriodDays,
			"features":            features,
			"status":              app.Status,
			"created_at":          app.CreatedAt,
		})
	}

	response.Success(c, result)
}

// Get 获取应用详情
func (h *ApplicationHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 解析功能列表
	var features []string
	if app.Features != "" {
		json.Unmarshal([]byte(app.Features), &features)
	}

	result := gin.H{
		"id":                  app.ID,
		"name":                app.Name,
		"app_key":             app.AppKey,
		"auth_mode":           app.AuthMode,
		"public_key":          app.PublicKey,
		"description":         app.Description,
		"heartbeat_interval":  app.HeartbeatInterval,
		"offline_tolerance":   app.OfflineTolerance,
		"max_devices_default": app.MaxDevicesDefault,
		"grace_period_days":   app.GracePeriodDays,
		"features":            features,
		"status":              app.Status,
		"created_at":          app.CreatedAt,
	}
	if current, ok := c.Get("team_member"); ok {
		if member, ok := current.(model.TeamMember); ok && member.HasPermission("app:update") {
			result["app_secret"] = app.AppSecret
		}
	}

	response.Success(c, result)
}

// UpdateAppRequest 更新应用请求
type UpdateAppRequest struct {
	Name              *string  `json:"name"`
	Description       *string  `json:"description"`
	AuthMode          *string  `json:"auth_mode"`
	HeartbeatInterval *int     `json:"heartbeat_interval"`
	OfflineTolerance  *int     `json:"offline_tolerance"`
	MaxDevicesDefault *int     `json:"max_devices_default"`
	GracePeriodDays   *int     `json:"grace_period_days"`
	Features          []string `json:"features"` // 功能列表
	Status            *string  `json:"status"`
}

// Update 更新应用
func (h *ApplicationHandler) Update(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var req UpdateAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 更新字段
	updates := map[string]interface{}{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.AuthMode != nil {
		if *req.AuthMode == "" {
			response.BadRequest(c, "授权模式不能为空")
			return
		}
		if !isValidAppAuthMode(*req.AuthMode) {
			response.BadRequest(c, "授权模式不支持")
			return
		}
		updates["auth_mode"] = *req.AuthMode
	}
	if req.HeartbeatInterval != nil {
		if *req.HeartbeatInterval <= 0 {
			response.BadRequest(c, "心跳间隔必须大于 0")
			return
		}
		updates["heartbeat_interval"] = *req.HeartbeatInterval
	}
	if req.OfflineTolerance != nil {
		if *req.OfflineTolerance <= 0 {
			response.BadRequest(c, "离线容忍时间必须大于 0")
			return
		}
		updates["offline_tolerance"] = *req.OfflineTolerance
	}
	if req.MaxDevicesDefault != nil {
		if *req.MaxDevicesDefault <= 0 {
			response.BadRequest(c, "默认最大设备数必须大于 0")
			return
		}
		updates["max_devices_default"] = *req.MaxDevicesDefault
	}
	if req.GracePeriodDays != nil {
		if *req.GracePeriodDays < 0 {
			response.BadRequest(c, "宽限天数不能小于 0")
			return
		}
		updates["grace_period_days"] = *req.GracePeriodDays
	}
	if req.Features != nil {
		featuresJSON, _ := json.Marshal(req.Features)
		updates["features"] = string(featuresJSON)
	}
	if req.Status != nil && *req.Status != "" {
		if !isValidAppStatus(*req.Status) {
			response.BadRequest(c, "应用状态不支持")
			return
		}
		updates["status"] = *req.Status
	}

	if err := model.DB.Model(&app).Updates(updates).Error; err != nil {
		response.ServerError(c, "更新应用失败")
		return
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

func isValidAppStatus(status string) bool {
	switch model.AppStatus(status) {
	case model.AppStatusActive, model.AppStatusDisabled:
		return true
	default:
		return false
	}
}

func isValidAppAuthMode(authMode string) bool {
	switch model.AuthMode(authMode) {
	case model.AuthModeLicense, model.AuthModeSubscription, model.AuthModeBoth:
		return true
	default:
		return false
	}
}

// Delete 删除应用
func (h *ApplicationHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	blocker, err := firstApplicationDeleteBlocker(tenantID, id)
	if err != nil {
		response.ServerError(c, "检查应用关联数据失败: "+err.Error())
		return
	}
	if blocker != "" {
		response.Error(c, 400, "该应用下存在"+blocker+"，无法删除")
		return
	}

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := cleanupApplicationRuntimeData(tx, tenantID, id); err != nil {
			return err
		}
		return tx.Delete(&app).Error
	}); err != nil {
		response.ServerError(c, "删除应用失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
}

func firstApplicationDeleteBlocker(tenantID, appID string) (string, error) {
	checks := []struct {
		name  string
		model interface{}
		query string
		args  []interface{}
	}{
		{"授权记录", &model.License{}, "app_id = ? AND tenant_id = ?", []interface{}{appID, tenantID}},
		{"订阅记录", &model.Subscription{}, "app_id = ? AND tenant_id = ?", []interface{}{appID, tenantID}},
		{"设备记录", &model.Device{}, "tenant_id = ? AND (license_id IN (SELECT id FROM licenses WHERE app_id = ? AND tenant_id = ?) OR subscription_id IN (SELECT id FROM subscriptions WHERE app_id = ? AND tenant_id = ?))", []interface{}{tenantID, appID, tenantID, appID, tenantID}},
		{"设备黑名单", &model.DeviceBlacklist{}, "tenant_id = ? AND app_id = ?", []interface{}{tenantID, appID}},
		{"脚本记录", &model.Script{}, "app_id = ?", []interface{}{appID}},
		{"版本记录", &model.AppRelease{}, "app_id = ?", []interface{}{appID}},
		{"热更新记录", &model.HotUpdate{}, "app_id = ?", []interface{}{appID}},
		{"热更新日志", &model.HotUpdateLog{}, "hot_update_id IN (SELECT id FROM hot_updates WHERE app_id = ?)", []interface{}{appID}},
		{"安全脚本", &model.SecureScript{}, "app_id = ?", []interface{}{appID}},
		{"脚本下发记录", &model.ScriptDelivery{}, "script_id IN (SELECT id FROM secure_scripts WHERE app_id = ?)", []interface{}{appID}},
		{"客户端同步数据", &model.ClientSyncData{}, "tenant_id = ? AND app_id = ?", []interface{}{tenantID, appID}},
		{"发布任务", &model.PublishTask{}, "tenant_id = ? AND app_id = ?", []interface{}{tenantID, appID}},
		{"生成任务", &model.GenerationTask{}, "tenant_id = ? AND app_id = ?", []interface{}{tenantID, appID}},
		{"用户配置", &model.UserConfig{}, "app_id = ?", []interface{}{appID}},
		{"用户工作流", &model.UserWorkflow{}, "app_id = ?", []interface{}{appID}},
		{"用户批量任务", &model.UserBatchTask{}, "app_id = ?", []interface{}{appID}},
		{"用户素材", &model.UserMaterial{}, "app_id = ?", []interface{}{appID}},
		{"用户帖子", &model.UserPost{}, "app_id = ?", []interface{}{appID}},
		{"用户评论", &model.UserComment{}, "app_id = ?", []interface{}{appID}},
		{"用户评论话术", &model.UserCommentScript{}, "app_id = ?", []interface{}{appID}},
		{"用户文件", &model.UserFile{}, "app_id = ?", []interface{}{appID}},
		{"同步检查点", &model.SyncCheckpoint{}, "app_id = ?", []interface{}{appID}},
		{"同步冲突", &model.SyncConflict{}, "app_id = ?", []interface{}{appID}},
		{"同步日志", &model.SyncLog{}, "app_id = ?", []interface{}{appID}},
		{"用户声音配置", &model.UserVoiceConfig{}, "app_id = ?", []interface{}{appID}},
		{"通用表数据", &model.UserTableData{}, "app_id = ?", []interface{}{appID}},
	}

	for _, check := range checks {
		var count int64
		if err := model.DB.Model(check.model).Where(check.query, check.args...).Count(&count).Error; err != nil {
			return "", err
		}
		if count > 0 {
			return check.name, nil
		}
	}
	return "", nil
}

func cleanupApplicationRuntimeData(tx *gorm.DB, tenantID, appID string) error {
	cleanups := []struct {
		model interface{}
		query string
		args  []interface{}
	}{
		{&model.RealtimeInstructionResult{}, "app_id = ?", []interface{}{appID}},
		{&model.RealtimeInstruction{}, "app_id = ?", []interface{}{appID}},
		{&model.DeviceConnection{}, "app_id = ?", []interface{}{appID}},
		{&model.ClientSession{}, "tenant_id = ? AND app_id = ?", []interface{}{tenantID, appID}},
		{
			&model.Heartbeat{},
			"tenant_id = ? AND (license_id IN (SELECT id FROM licenses WHERE app_id = ? AND tenant_id = ?) OR subscription_id IN (SELECT id FROM subscriptions WHERE app_id = ? AND tenant_id = ?))",
			[]interface{}{tenantID, appID, tenantID, appID, tenantID},
		},
	}

	for _, cleanup := range cleanups {
		if err := tx.Where(cleanup.query, cleanup.args...).Delete(cleanup.model).Error; err != nil {
			return err
		}
	}
	return nil
}

// RegenerateKeys 重新生成密钥对
func (h *ApplicationHandler) RegenerateKeys(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 生成新的 RSA 密钥对
	publicKey, privateKey, err := crypto.GenerateRSAKeyPair(config.Get().RSA.KeySize)
	if err != nil {
		response.ServerError(c, "生成密钥对失败")
		return
	}

	app.PublicKey = publicKey
	app.PrivateKey = privateKey
	app.AppSecret = utils.GenerateAppSecret()

	if err := model.DB.Save(&app).Error; err != nil {
		response.ServerError(c, "更新密钥失败")
		return
	}

	response.Success(c, gin.H{
		"app_key":    app.AppKey,
		"app_secret": app.AppSecret,
		"public_key": app.PublicKey,
	})
}
