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
)

type ApplicationHandler struct{}

func NewApplicationHandler() *ApplicationHandler {
	return &ApplicationHandler{}
}

// CreateAppRequest 创建应用请求
type CreateAppRequest struct {
	Name              string   `json:"name" binding:"required"`
	Description       string   `json:"description"`
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
		HeartbeatInterval: req.HeartbeatInterval,
		OfflineTolerance:  req.OfflineTolerance,
		MaxDevicesDefault: req.MaxDevicesDefault,
		GracePeriodDays:   req.GracePeriodDays,
		Status:            model.AppStatusActive,
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

	response.Success(c, gin.H{
		"id":                  app.ID,
		"name":                app.Name,
		"app_key":             app.AppKey,
		"app_secret":          app.AppSecret,
		"public_key":          app.PublicKey,
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

// UpdateAppRequest 更新应用请求
type UpdateAppRequest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	HeartbeatInterval int      `json:"heartbeat_interval"`
	OfflineTolerance  int      `json:"offline_tolerance"`
	MaxDevicesDefault int      `json:"max_devices_default"`
	GracePeriodDays   int      `json:"grace_period_days"`
	Features          []string `json:"features"` // 功能列表
	Status            string   `json:"status"`
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
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.HeartbeatInterval > 0 {
		updates["heartbeat_interval"] = req.HeartbeatInterval
	}
	if req.OfflineTolerance > 0 {
		updates["offline_tolerance"] = req.OfflineTolerance
	}
	if req.MaxDevicesDefault > 0 {
		updates["max_devices_default"] = req.MaxDevicesDefault
	}
	if req.GracePeriodDays > 0 {
		updates["grace_period_days"] = req.GracePeriodDays
	}
	if req.Features != nil {
		featuresJSON, _ := json.Marshal(req.Features)
		updates["features"] = string(featuresJSON)
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}

	if err := model.DB.Model(&app).Updates(updates).Error; err != nil {
		response.ServerError(c, "更新应用失败")
		return
	}

	response.SuccessWithMessage(c, "更新成功", nil)
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

	// 检查是否有关联的授权
	var licenseCount int64
	model.DB.Model(&model.License{}).Where("app_id = ? AND tenant_id = ?", id, tenantID).Count(&licenseCount)
	if licenseCount > 0 {
		response.Error(c, 400, "该应用下存在授权记录，无法删除")
		return
	}

	if err := model.DB.Delete(&app).Error; err != nil {
		response.ServerError(c, "删除应用失败")
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
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
