package handler

import (
	"encoding/json"
	"fmt"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"license-server/internal/service"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type SecureScriptHandler struct {
	service *service.SecureScriptService
}

func NewSecureScriptHandler() *SecureScriptHandler {
	return &SecureScriptHandler{
		service: service.NewSecureScriptService(),
	}
}

// ==================== 管理端接口 ====================

// CreateSecureScriptRequest 创建安全脚本请求
type CreateSecureScriptRequest struct {
	Name             string   `json:"name" binding:"required"`
	Description      string   `json:"description"`
	Version          string   `json:"version" binding:"required"`
	ScriptType       string   `json:"script_type"` // python/lua/instruction
	EntryPoint       string   `json:"entry_point"`
	Timeout          int      `json:"timeout"`
	MemoryLimit      int      `json:"memory_limit"`
	Parameters       string   `json:"parameters"`
	RequiredFeatures []string `json:"required_features"`
	AllowedDevices   []string `json:"allowed_devices"`
}

// Create 创建安全脚本 (上传脚本内容)
func (h *SecureScriptHandler) Create(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传脚本文件")
		return
	}
	defer file.Close()

	maxSecureScriptUploadBytes := getMaxSecureScriptUploadSizeBytes()
	if header.Size > maxSecureScriptUploadBytes {
		response.BadRequest(c, fmt.Sprintf("脚本文件过大，最大支持 %dMB", maxSecureScriptUploadBytes>>20))
		return
	}

	// 读取文件内容（限制大小，防止内存占用过高）
	content, err := readUploadedContentWithLimit(file, maxSecureScriptUploadBytes)
	if err != nil {
		if err == errUploadFileTooLarge {
			response.BadRequest(c, fmt.Sprintf("脚本文件过大，最大支持 %dMB", maxSecureScriptUploadBytes>>20))
			return
		}
		response.ServerError(c, "读取文件失败")
		return
	}

	// 获取其他参数
	name := c.PostForm("name")
	if name == "" {
		response.BadRequest(c, "请提供脚本名称")
		return
	}

	version := c.PostForm("version")
	if version == "" {
		version = "1.0.0"
	}

	scriptType := c.PostForm("script_type")
	if scriptType == "" {
		scriptType = "python"
	}

	// 加密脚本内容
	encryptedContent, encryptedKey, contentHash, err := h.service.EncryptScriptForStorage(content, app.PublicKey)
	if err != nil {
		response.ServerError(c, "加密脚本失败: "+err.Error())
		return
	}

	// 解析可选参数
	timeout, _ := strconv.Atoi(c.DefaultPostForm("timeout", "300"))
	memoryLimit, _ := strconv.Atoi(c.DefaultPostForm("memory_limit", "512"))

	requiredFeatures := c.PostForm("required_features")
	if requiredFeatures == "" {
		requiredFeatures = "[]"
	}

	allowedDevices := c.PostForm("allowed_devices")
	if allowedDevices == "" {
		allowedDevices = "[]"
	}

	// 创建脚本记录
	script := model.SecureScript{
		AppID:            appID,
		Name:             name,
		Description:      c.PostForm("description"),
		Version:          version,
		ScriptType:       model.SecureScriptType(scriptType),
		EntryPoint:       c.PostForm("entry_point"),
		EncryptedContent: encryptedContent,
		ContentHash:      contentHash,
		StorageKey:       encryptedKey,
		OriginalSize:     int64(len(content)),
		EncryptedSize:    int64(len(encryptedContent)),
		Timeout:          timeout,
		MemoryLimit:      memoryLimit,
		Parameters:       c.PostForm("parameters"),
		RequiredFeatures: requiredFeatures,
		AllowedDevices:   allowedDevices,
		Status:           model.SecureScriptStatusDraft,
	}

	if err := model.DB.Create(&script).Error; err != nil {
		response.ServerError(c, "创建脚本失败")
		return
	}

	response.Success(c, gin.H{
		"id":             script.ID,
		"name":           script.Name,
		"version":        script.Version,
		"script_type":    script.ScriptType,
		"content_hash":   script.ContentHash,
		"original_size":  script.OriginalSize,
		"encrypted_size": script.EncryptedSize,
		"status":         script.Status,
		"created_at":     script.CreatedAt,
	})
}

// List 获取安全脚本列表
func (h *SecureScriptHandler) List(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var scripts []model.SecureScript
	query := model.DB.Where("app_id = ?", appID).Order("created_at DESC")

	// 状态筛选
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	// 分页
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	offset := (page - 1) * pageSize

	var total int64
	query.Model(&model.SecureScript{}).Count(&total)
	query.Offset(offset).Limit(pageSize).Find(&scripts)

	var result []gin.H
	for _, script := range scripts {
		result = append(result, gin.H{
			"id":                 script.ID,
			"name":               script.Name,
			"description":        script.Description,
			"version":            script.Version,
			"script_type":        script.ScriptType,
			"entry_point":        script.EntryPoint,
			"content_hash":       script.ContentHash,
			"original_size":      script.OriginalSize,
			"timeout":            script.Timeout,
			"memory_limit":       script.MemoryLimit,
			"required_features":  script.RequiredFeatures,
			"allowed_devices":    script.AllowedDevices,
			"rollout_percentage": script.RolloutPercentage,
			"status":             script.Status,
			"delivery_count":     script.DeliveryCount,
			"execute_count":      script.ExecuteCount,
			"success_count":      script.SuccessCount,
			"fail_count":         script.FailCount,
			"published_at":       script.PublishedAt,
			"expires_at":         script.ExpiresAt,
			"created_at":         script.CreatedAt,
			"updated_at":         script.UpdatedAt,
		})
	}

	response.Success(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Get 获取安全脚本详情
func (h *SecureScriptHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.SecureScript
	if err := model.DB.Joins("JOIN applications ON applications.id = secure_scripts.app_id").
		Where("secure_scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	response.Success(c, gin.H{
		"id":                 script.ID,
		"app_id":             script.AppID,
		"name":               script.Name,
		"description":        script.Description,
		"version":            script.Version,
		"script_type":        script.ScriptType,
		"entry_point":        script.EntryPoint,
		"content_hash":       script.ContentHash,
		"original_size":      script.OriginalSize,
		"encrypted_size":     script.EncryptedSize,
		"timeout":            script.Timeout,
		"memory_limit":       script.MemoryLimit,
		"parameters":         script.Parameters,
		"required_features":  script.RequiredFeatures,
		"allowed_devices":    script.AllowedDevices,
		"rollout_percentage": script.RolloutPercentage,
		"status":             script.Status,
		"delivery_count":     script.DeliveryCount,
		"execute_count":      script.ExecuteCount,
		"success_count":      script.SuccessCount,
		"fail_count":         script.FailCount,
		"published_at":       script.PublishedAt,
		"expires_at":         script.ExpiresAt,
		"created_at":         script.CreatedAt,
		"updated_at":         script.UpdatedAt,
	})
}

// Update 更新安全脚本配置
func (h *SecureScriptHandler) Update(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.SecureScript
	if err := model.DB.Joins("JOIN applications ON applications.id = secure_scripts.app_id").
		Where("secure_scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	var req struct {
		Name              string   `json:"name"`
		Description       string   `json:"description"`
		EntryPoint        string   `json:"entry_point"`
		Timeout           int      `json:"timeout"`
		MemoryLimit       int      `json:"memory_limit"`
		Parameters        string   `json:"parameters"`
		RequiredFeatures  []string `json:"required_features"`
		AllowedDevices    []string `json:"allowed_devices"`
		RolloutPercentage int      `json:"rollout_percentage"`
		ExpiresAt         *string  `json:"expires_at"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.EntryPoint != "" {
		updates["entry_point"] = req.EntryPoint
	}
	if req.Timeout > 0 {
		updates["timeout"] = req.Timeout
	}
	if req.MemoryLimit > 0 {
		updates["memory_limit"] = req.MemoryLimit
	}
	if req.Parameters != "" {
		updates["parameters"] = req.Parameters
	}
	if req.RequiredFeatures != nil {
		featuresJSON, _ := json.Marshal(req.RequiredFeatures)
		updates["required_features"] = string(featuresJSON)
	}
	if req.AllowedDevices != nil {
		devicesJSON, _ := json.Marshal(req.AllowedDevices)
		updates["allowed_devices"] = string(devicesJSON)
	}
	if req.RolloutPercentage > 0 && req.RolloutPercentage <= 100 {
		updates["rollout_percentage"] = req.RolloutPercentage
	}
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			updates["expires_at"] = nil
		} else {
			expiresAt, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err == nil {
				updates["expires_at"] = expiresAt
			}
		}
	}

	model.DB.Model(&script).Updates(updates)

	response.SuccessWithMessage(c, "更新成功", nil)
}

// UpdateContent 更新脚本内容 (新版本)
func (h *SecureScriptHandler) UpdateContent(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.SecureScript
	if err := model.DB.Joins("JOIN applications ON applications.id = secure_scripts.app_id").
		Where("secure_scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	// 获取应用
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", script.AppID, tenantID).Error; err != nil {
		response.ServerError(c, "应用不存在")
		return
	}

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传脚本文件")
		return
	}
	defer file.Close()

	maxSecureScriptUploadBytes := getMaxSecureScriptUploadSizeBytes()
	if header.Size > maxSecureScriptUploadBytes {
		response.BadRequest(c, fmt.Sprintf("脚本文件过大，最大支持 %dMB", maxSecureScriptUploadBytes>>20))
		return
	}

	// 读取文件内容（限制大小，防止内存占用过高）
	content, err := readUploadedContentWithLimit(file, maxSecureScriptUploadBytes)
	if err != nil {
		if err == errUploadFileTooLarge {
			response.BadRequest(c, fmt.Sprintf("脚本文件过大，最大支持 %dMB", maxSecureScriptUploadBytes>>20))
			return
		}
		response.ServerError(c, "读取文件失败")
		return
	}

	// 获取新版本号
	version := c.PostForm("version")
	if version == "" {
		response.BadRequest(c, "请提供新版本号")
		return
	}

	// 加密新内容
	encryptedContent, encryptedKey, contentHash, err := h.service.EncryptScriptForStorage(content, app.PublicKey)
	if err != nil {
		response.ServerError(c, "加密脚本失败: "+err.Error())
		return
	}

	// 更新脚本
	updates := map[string]interface{}{
		"version":           version,
		"encrypted_content": encryptedContent,
		"storage_key":       encryptedKey,
		"content_hash":      contentHash,
		"original_size":     int64(len(content)),
		"encrypted_size":    int64(len(encryptedContent)),
		"status":            model.SecureScriptStatusDraft, // 更新内容后重置为草稿
	}

	model.DB.Model(&script).Updates(updates)

	response.Success(c, gin.H{
		"id":             script.ID,
		"version":        version,
		"content_hash":   contentHash,
		"original_size":  int64(len(content)),
		"encrypted_size": int64(len(encryptedContent)),
	})
}

// Publish 发布安全脚本
func (h *SecureScriptHandler) Publish(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.SecureScript
	if err := model.DB.Joins("JOIN applications ON applications.id = secure_scripts.app_id").
		Where("secure_scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	if script.Status == model.SecureScriptStatusPublished {
		response.Error(c, 400, "脚本已发布")
		return
	}

	now := time.Now()
	script.Status = model.SecureScriptStatusPublished
	script.PublishedAt = &now
	model.DB.Save(&script)

	response.SuccessWithMessage(c, "发布成功", nil)
}

// Deprecate 废弃安全脚本
func (h *SecureScriptHandler) Deprecate(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.SecureScript
	if err := model.DB.Joins("JOIN applications ON applications.id = secure_scripts.app_id").
		Where("secure_scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	script.Status = model.SecureScriptStatusDeprecated
	model.DB.Save(&script)

	response.SuccessWithMessage(c, "已废弃", nil)
}

// Delete 删除安全脚本
func (h *SecureScriptHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.SecureScript
	if err := model.DB.Joins("JOIN applications ON applications.id = secure_scripts.app_id").
		Where("secure_scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	// 删除下发记录
	model.DB.Where("script_id = ?", id).Delete(&model.ScriptDelivery{})

	// 删除脚本
	model.DB.Delete(&script)

	response.SuccessWithMessage(c, "删除成功", nil)
}

// GetDeliveries 获取脚本下发记录
func (h *SecureScriptHandler) GetDeliveries(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.SecureScript
	if err := model.DB.Joins("JOIN applications ON applications.id = secure_scripts.app_id").
		Where("secure_scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	var deliveries []model.ScriptDelivery
	query := model.DB.Where("script_id = ?", id).Order("created_at DESC")

	// 状态筛选
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	// 分页
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	offset := (page - 1) * pageSize

	var total int64
	query.Model(&model.ScriptDelivery{}).Count(&total)
	query.Offset(offset).Limit(pageSize).Find(&deliveries)

	var result []gin.H
	for _, d := range deliveries {
		result = append(result, gin.H{
			"id":            d.ID,
			"device_id":     d.DeviceID,
			"machine_id":    d.MachineID,
			"license_id":    d.LicenseID,
			"expires_at":    d.ExpiresAt,
			"ip_address":    d.IPAddress,
			"status":        d.Status,
			"executed_at":   d.ExecutedAt,
			"completed_at":  d.CompletedAt,
			"duration":      d.Duration,
			"result":        d.Result,
			"error_message": d.ErrorMessage,
			"created_at":    d.CreatedAt,
		})
	}

	response.Success(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetStats 获取脚本统计
func (h *SecureScriptHandler) GetStats(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var stats struct {
		TotalScripts    int64   `json:"total_scripts"`
		PublishedCount  int64   `json:"published_count"`
		TotalDeliveries int64   `json:"total_deliveries"`
		TotalExecutions int64   `json:"total_executions"`
		TotalSuccess    int64   `json:"total_success"`
		TotalFail       int64   `json:"total_fail"`
		SuccessRate     float64 `json:"success_rate"`
	}

	model.DB.Model(&model.SecureScript{}).Where("app_id = ?", appID).Count(&stats.TotalScripts)
	model.DB.Model(&model.SecureScript{}).Where("app_id = ? AND status = ?", appID, model.SecureScriptStatusPublished).Count(&stats.PublishedCount)

	var sums struct {
		Deliveries int64
		Executions int64
		Success    int64
		Fail       int64
	}
	model.DB.Model(&model.SecureScript{}).Where("app_id = ?", appID).
		Select("COALESCE(SUM(delivery_count), 0) as deliveries, COALESCE(SUM(execute_count), 0) as executions, COALESCE(SUM(success_count), 0) as success, COALESCE(SUM(fail_count), 0) as fail").
		Scan(&sums)

	stats.TotalDeliveries = sums.Deliveries
	stats.TotalExecutions = sums.Executions
	stats.TotalSuccess = sums.Success
	stats.TotalFail = sums.Fail

	if stats.TotalSuccess+stats.TotalFail > 0 {
		stats.SuccessRate = float64(stats.TotalSuccess) / float64(stats.TotalSuccess+stats.TotalFail) * 100
	}

	response.Success(c, stats)
}

// ==================== 客户端接口 ====================

// ClientGetVersions 客户端获取脚本版本列表
func (h *SecureScriptHandler) ClientGetVersions(c *gin.Context) {
	appKey := c.Query("app_key")
	if appKey == "" {
		response.BadRequest(c, "缺少 app_key")
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", appKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	versions, err := h.service.GetScriptVersions(app.ID)
	if err != nil {
		response.ServerError(c, "获取版本失败")
		return
	}

	response.Success(c, versions)
}

// ClientFetchScript 客户端获取加密脚本
func (h *SecureScriptHandler) ClientFetchScript(c *gin.Context) {
	var req struct {
		AppKey    string `json:"app_key" binding:"required"`
		MachineID string `json:"machine_id" binding:"required"`
		ScriptID  string `json:"script_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", req.AppKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	// 验证设备授权
	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ?", req.MachineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 401, "设备未授权")
		return
	}
	if (device.License == nil || device.License.AppID != app.ID) &&
		(device.Subscription == nil || device.Subscription.AppID != app.ID) {
		response.Error(c, 401, "设备未绑定当前应用")
		return
	}

	licenseID := ""
	var features []string
	if device.License != nil {
		licenseID = device.License.ID
		if device.License.Features != "" {
			json.Unmarshal([]byte(device.License.Features), &features)
		}
	} else if device.Subscription != nil && device.Subscription.Features != "" {
		json.Unmarshal([]byte(device.Subscription.Features), &features)
	}

	// 获取脚本
	var script model.SecureScript
	if err := model.DB.First(&script, "id = ? AND app_id = ? AND status = ?",
		req.ScriptID, app.ID, model.SecureScriptStatusPublished).Error; err != nil {
		response.NotFound(c, "脚本不存在或未发布")
		return
	}

	// 检查设备权限
	if err := h.service.CheckDevicePermission(&script, req.MachineID, features); err != nil {
		response.Error(c, 403, err.Error())
		return
	}

	// 灰度检查
	if script.RolloutPercentage < 100 {
		hash := crypto.SHA256HashString(req.MachineID)
		hashInt := int(hash[0]) % 100
		if hashInt >= script.RolloutPercentage {
			response.Error(c, 403, "脚本暂未对该设备开放")
			return
		}
	}

	// 准备下发包 (有效期5分钟)
	pkg, keyHint, err := h.service.PrepareScriptForDelivery(&script, &app, req.MachineID, 5*time.Minute)
	if err != nil {
		response.ServerError(c, "准备脚本失败: "+err.Error())
		return
	}

	// 记录下发
	h.service.RecordDelivery(
		script.ID,
		device.ID,
		req.MachineID,
		licenseID,
		keyHint,
		time.Unix(pkg.ExpiresAt, 0),
		c.ClientIP(),
	)

	response.Success(c, pkg)
}

// ClientReportExecution 客户端上报执行结果
func (h *SecureScriptHandler) ClientReportExecution(c *gin.Context) {
	var req struct {
		AppKey       string `json:"app_key" binding:"required"`
		MachineID    string `json:"machine_id" binding:"required"`
		ScriptID     string `json:"script_id" binding:"required"`
		DeliveryID   string `json:"delivery_id"`
		Status       string `json:"status" binding:"required"` // executing/success/failed
		Result       string `json:"result"`
		ErrorMessage string `json:"error_message"`
		Duration     int    `json:"duration"` // 执行耗时(毫秒)
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", req.AppKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	// 验证脚本归属应用
	var script model.SecureScript
	if err := model.DB.First(&script, "id = ? AND app_id = ?", req.ScriptID, app.ID).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	// 验证设备归属应用
	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ?", req.MachineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 401, "设备未授权")
		return
	}
	if (device.License == nil || device.License.AppID != app.ID) &&
		(device.Subscription == nil || device.Subscription.AppID != app.ID) {
		response.Error(c, 401, "设备未绑定当前应用")
		return
	}

	// 查找下发记录
	var delivery model.ScriptDelivery
	query := model.DB.Where("script_id = ? AND machine_id = ?", script.ID, req.MachineID)
	if req.DeliveryID != "" {
		query = query.Where("id = ?", req.DeliveryID)
	}
	query.Order("created_at DESC").First(&delivery)

	if delivery.ID == "" {
		response.Error(c, 400, "未找到下发记录")
		return
	}

	// 更新状态
	status := model.ScriptDeliveryStatus(req.Status)
	if err := h.service.UpdateDeliveryStatus(delivery.ID, status, req.Result, req.ErrorMessage, req.Duration); err != nil {
		response.ServerError(c, "更新状态失败")
		return
	}

	response.SuccessWithMessage(c, "上报成功", nil)
}
