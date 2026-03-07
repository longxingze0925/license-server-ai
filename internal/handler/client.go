package handler

import (
	"encoding/json"
	"fmt"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"license-server/internal/service"
	"time"

	"github.com/gin-gonic/gin"
)

type ClientHandler struct{}

func NewClientHandler() *ClientHandler {
	return &ClientHandler{}
}

// ActivateRequest 激活请求
type ActivateRequest struct {
	AppKey     string     `json:"app_key" binding:"required"`
	LicenseKey string     `json:"license_key" binding:"required"`
	MachineID  string     `json:"machine_id" binding:"required"`
	DeviceInfo DeviceInfo `json:"device_info"`
}

type DeviceInfo struct {
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	OSVersion  string `json:"os_version"`
	AppVersion string `json:"app_version"`
}

// ActivateResponse 激活响应
type ActivateResponse struct {
	Valid         bool       `json:"valid"`
	LicenseID     string     `json:"license_id"`
	DeviceID      string     `json:"device_id"`
	Type          string     `json:"type"`
	ExpireAt      *time.Time `json:"expire_at"`
	RemainingDays int        `json:"remaining_days"`
	Features      []string   `json:"features"`
	Signature     string     `json:"signature"`
}

// Activate 激活授权码
func (h *ClientHandler) Activate(c *gin.Context) {
	var req ActivateRequest
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

	// 检查设备是否在黑名单
	var blacklist model.DeviceBlacklist
	if err := model.DB.Where("tenant_id = ? AND machine_id = ? AND (app_id = ? OR app_id IS NULL)", app.TenantID, req.MachineID, app.ID).First(&blacklist).Error; err == nil {
		response.Error(c, 403, "设备已被禁止使用")
		return
	}

	// 查找授权
	var license model.License
	if err := model.DB.First(&license, "license_key = ? AND app_id = ?", req.LicenseKey, app.ID).Error; err != nil {
		response.Error(c, 400, "无效的授权码")
		return
	}

	// 检查授权状态
	if license.Status == model.LicenseStatusRevoked {
		response.Error(c, 403, "授权已被吊销")
		return
	}

	if license.Status == model.LicenseStatusSuspended {
		response.Error(c, 403, "授权已被暂停")
		return
	}

	// 首次激活
	if license.Status == model.LicenseStatusPending {
		now := time.Now()
		license.ActivatedAt = &now

		// 计算到期时间
		if license.DurationDays == -1 {
			// 永久授权
			license.ExpireAt = nil
		} else {
			expireAt := now.AddDate(0, 0, license.DurationDays)
			license.ExpireAt = &expireAt
			// 设置宽限期
			graceExpireAt := expireAt.AddDate(0, 0, app.GracePeriodDays)
			license.GraceExpireAt = &graceExpireAt
		}

		license.Status = model.LicenseStatusActive

		// 记录激活事件
		event := model.LicenseEvent{
			LicenseID:    license.ID,
			EventType:    model.LicenseEventActivated,
			OperatorType: "user",
			IPAddress:    c.ClientIP(),
		}
		model.DB.Create(&event)
	}

	// 检查是否已过期
	if license.ExpireAt != nil && time.Now().After(*license.ExpireAt) {
		// 检查宽限期
		if license.GraceExpireAt == nil || time.Now().After(*license.GraceExpireAt) {
			license.Status = model.LicenseStatusExpired
			model.DB.Save(&license)
			response.Error(c, 403, "授权已过期")
			return
		}
	}

	// 检查设备数量
	var deviceCount int64
	model.DB.Model(&model.Device{}).Where("tenant_id = ? AND license_id = ?", license.TenantID, license.ID).Count(&deviceCount)

	// 检查该设备是否已绑定
	var existingDevice model.Device
	deviceExists := model.DB.Where("tenant_id = ? AND license_id = ? AND machine_id = ?", license.TenantID, license.ID, req.MachineID).First(&existingDevice).Error == nil

	if !deviceExists && int(deviceCount) >= license.MaxDevices {
		response.Error(c, 403, "设备数量已达上限")
		return
	}

	// 绑定或更新设备
	var device model.Device
	if deviceExists {
		device = existingDevice
		device.TenantID = license.TenantID
		if license.CustomerID != nil {
			device.CustomerID = *license.CustomerID
		}
		device.DeviceName = req.DeviceInfo.Name
		device.Hostname = req.DeviceInfo.Hostname
		device.OSType = req.DeviceInfo.OS
		device.OSVersion = req.DeviceInfo.OSVersion
		device.AppVersion = req.DeviceInfo.AppVersion
		device.IPAddress = c.ClientIP()
		now := time.Now()
		device.LastActiveAt = &now
		if err := model.DB.Save(&device).Error; err != nil {
			response.ServerError(c, "更新设备失败: "+err.Error())
			return
		}
	} else {
		now := time.Now()
		customerID := ""
		if license.CustomerID != nil {
			customerID = *license.CustomerID
		}
		device = model.Device{
			TenantID:     license.TenantID,
			CustomerID:   customerID,
			LicenseID:    &license.ID,
			MachineID:    req.MachineID,
			DeviceName:   req.DeviceInfo.Name,
			Hostname:     req.DeviceInfo.Hostname,
			OSType:       req.DeviceInfo.OS,
			OSVersion:    req.DeviceInfo.OSVersion,
			AppVersion:   req.DeviceInfo.AppVersion,
			IPAddress:    c.ClientIP(),
			Status:       model.DeviceStatusActive,
			LastActiveAt: &now,
		}
		if err := model.DB.Create(&device).Error; err != nil {
			response.ServerError(c, "创建设备失败: "+err.Error())
			return
		}
	}

	// 更新授权验证时间
	now := time.Now()
	license.LastValidatedAt = &now
	if err := model.DB.Save(&license).Error; err != nil {
		response.ServerError(c, "更新授权状态失败: "+err.Error())
		return
	}

	// 解析 features
	var features []string
	if license.Features != "" {
		json.Unmarshal([]byte(license.Features), &features)
	}

	// 构建响应数据
	respData := ActivateResponse{
		Valid:         true,
		LicenseID:     license.ID,
		DeviceID:      device.ID,
		Type:          string(license.Type),
		ExpireAt:      license.ExpireAt,
		RemainingDays: license.RemainingDays(),
		Features:      features,
	}

	// 签名响应
	dataBytes, _ := json.Marshal(respData)
	signature, err := crypto.Sign(app.PrivateKey, dataBytes)
	if err == nil {
		respData.Signature = signature
	}

	response.Success(c, respData)
}

// VerifyRequest 验证请求
type VerifyRequest struct {
	AppKey    string `json:"app_key" binding:"required"`
	MachineID string `json:"machine_id" binding:"required"`
}

// Verify 验证授权
func (h *ClientHandler) Verify(c *gin.Context) {
	var req VerifyRequest
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

	// 查找设备
	var device model.Device
	if err := model.DB.Preload("License").
		Where("machine_id = ? AND tenant_id = ? AND license_id IS NOT NULL", req.MachineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 404, "设备未绑定授权")
		return
	}

	// 检查授权是否属于该应用
	if device.License == nil || device.License.AppID != app.ID {
		response.Error(c, 404, "设备未绑定该应用的授权")
		return
	}

	license := device.License

	// 检查授权状态
	if license.Status == model.LicenseStatusRevoked {
		response.Error(c, 403, "授权已被吊销")
		return
	}

	if license.Status == model.LicenseStatusSuspended {
		response.Error(c, 403, "授权已被暂停")
		return
	}

	// 检查是否过期
	if license.ExpireAt != nil && time.Now().After(*license.ExpireAt) {
		if license.GraceExpireAt == nil || time.Now().After(*license.GraceExpireAt) {
			license.Status = model.LicenseStatusExpired
			model.DB.Save(license)
			response.Error(c, 403, "授权已过期")
			return
		}
	}

	// 更新验证时间
	now := time.Now()
	license.LastValidatedAt = &now
	device.LastActiveAt = &now
	model.DB.Save(license)
	model.DB.Save(&device)

	// 解析 features
	var features []string
	if license.Features != "" {
		json.Unmarshal([]byte(license.Features), &features)
	}

	// 构建响应
	respData := gin.H{
		"valid":          true,
		"license_id":     license.ID,
		"type":           license.Type,
		"expire_at":      license.ExpireAt,
		"remaining_days": license.RemainingDays(),
		"features":       features,
	}

	// 签名
	dataBytes, _ := json.Marshal(respData)
	signature, _ := crypto.Sign(app.PrivateKey, dataBytes)
	respData["signature"] = signature

	response.Success(c, respData)
}

// HeartbeatRequest 心跳请求
type HeartbeatRequest struct {
	AppKey     string `json:"app_key" binding:"required"`
	MachineID  string `json:"machine_id" binding:"required"`
	AppVersion string `json:"app_version"`
}

// Heartbeat 心跳
func (h *ClientHandler) Heartbeat(c *gin.Context) {
	var req HeartbeatRequest
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

	// 查找设备
	var device model.Device
	if err := model.DB.Preload("License").Where("machine_id = ? AND tenant_id = ?", req.MachineID, app.TenantID).Order("created_at DESC").First(&device).Error; err != nil {
		response.Error(c, 404, "设备未绑定")
		return
	}

	if device.License == nil || device.License.AppID != app.ID {
		response.Error(c, 404, "设备未绑定该应用")
		return
	}

	// 检查授权状态
	if !device.License.IsValid() {
		response.Error(c, 403, "授权无效")
		return
	}

	// 更新心跳时间
	now := time.Now()
	device.LastHeartbeatAt = &now
	device.LastActiveAt = &now
	device.IPAddress = c.ClientIP()
	if req.AppVersion != "" {
		device.AppVersion = req.AppVersion
	}
	if err := model.DB.Save(&device).Error; err != nil {
		response.ServerError(c, "更新设备状态失败: "+err.Error())
		return
	}

	// 记录心跳
	if device.LicenseID == nil || *device.LicenseID == "" {
		response.Error(c, 400, "设备授权信息异常")
		return
	}
	heartbeat := model.Heartbeat{
		TenantID:   device.TenantID,
		LicenseID:  *device.LicenseID,
		DeviceID:   device.ID,
		IPAddress:  c.ClientIP(),
		AppVersion: req.AppVersion,
	}
	if err := model.DB.Create(&heartbeat).Error; err != nil {
		response.ServerError(c, "记录心跳失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"valid":          true,
		"remaining_days": device.License.RemainingDays(),
	})
}

// Deactivate 解绑设备
func (h *ClientHandler) Deactivate(c *gin.Context) {
	var req struct {
		AppKey    string `json:"app_key" binding:"required"`
		MachineID string `json:"machine_id" binding:"required"`
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

	// 查找并删除设备
	var device model.Device
	if err := model.DB.Preload("License").
		Where("machine_id = ? AND tenant_id = ? AND license_id IS NOT NULL", req.MachineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 404, "设备未绑定")
		return
	}

	if device.License == nil || device.License.AppID != app.ID {
		response.Error(c, 404, "设备未绑定该应用")
		return
	}

	model.DB.Delete(&device)

	response.SuccessWithMessage(c, "解绑成功", nil)
}

// GetScriptVersion 获取脚本版本
func (h *ClientHandler) GetScriptVersion(c *gin.Context) {
	appKey := c.Query("app_key")
	if appKey == "" {
		response.BadRequest(c, "缺少 app_key")
		return
	}

	var app model.Application
	if err := model.DB.First(&app, "app_key = ?", appKey).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	var scripts []model.Script
	model.DB.Where("app_id = ? AND status = ?", app.ID, model.ScriptStatusActive).Find(&scripts)

	result := make(map[string]string)
	for _, script := range scripts {
		result[script.Filename] = script.Version
	}

	response.Success(c, result)
}

// DownloadScript 下载脚本
func (h *ClientHandler) DownloadScript(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	filename := c.Param("filename")

	if appKey == "" || machineID == "" {
		response.BadRequest(c, "缺少参数")
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ?", appKey).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	// 验证设备授权
	var device model.Device
	if err := model.DB.Preload("License").
		Where("machine_id = ? AND tenant_id = ? AND license_id IS NOT NULL", machineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 403, "设备未授权")
		return
	}

	if device.License == nil || device.License.AppID != app.ID || !device.License.IsValid() {
		response.Error(c, 403, "授权无效")
		return
	}

	// 获取脚本
	var script model.Script
	if err := model.DB.Where("app_id = ? AND filename = ? AND status = ?", app.ID, filename, model.ScriptStatusActive).First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	// 返回脚本内容（如果加密则需要解密）
	c.Header("Content-Type", "text/plain")
	c.Header("X-Script-Version", script.Version)
	c.Header("X-Script-Hash", script.ContentHash)
	c.Data(200, "text/plain", script.Content)
}

// GetLatestRelease 获取最新版本
func (h *ClientHandler) GetLatestRelease(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	if appKey == "" || machineID == "" {
		response.BadRequest(c, "缺少 app_key 或 machine_id")
		return
	}

	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", appKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}
	if !validateClientDeviceForApp(c, &app, machineID) {
		return
	}

	var release model.AppRelease
	if err := model.DB.Where("app_id = ? AND status = ?", app.ID, model.ReleaseStatusPublished).Order("version_code DESC").First(&release).Error; err != nil {
		response.NotFound(c, "暂无发布版本")
		return
	}
	downloadURL, err := buildClientDownloadURLWithToken(release.DownloadURL, app.ID, machineID, downloadTokenKindRelease)
	if err != nil {
		response.ServerError(c, "生成下载链接失败")
		return
	}

	response.Success(c, gin.H{
		"version":      release.Version,
		"version_code": release.VersionCode,
		"download_url": downloadURL,
		"changelog":    release.Changelog,
		"file_size":    release.FileSize,
		"file_hash":    release.FileHash,
		"force_update": release.ForceUpdate,
	})
}

// ==================== 账号密码模式 ====================

// ClientLoginRequest 客户端登录请求（账号密码模式）
type ClientLoginRequest struct {
	AppKey         string     `json:"app_key" binding:"required"`
	Email          string     `json:"email" binding:"required,email"`
	Password       string     `json:"password" binding:"required"`
	PasswordHashed bool       `json:"password_hashed"` // 标记密码是否已预哈希
	MachineID      string     `json:"machine_id" binding:"required"`
	DeviceInfo     DeviceInfo `json:"device_info"`
}

// ClientLogin 客户端账号登录
func (h *ClientHandler) ClientLogin(c *gin.Context) {
	var req ClientLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	clientIP := c.ClientIP()
	loginLimiter := service.GetLoginLimiter()
	ipLimiter := service.GetIPLoginLimiter()

	// 检查 IP 是否被锁定
	if locked, remaining := ipLimiter.IsLocked(clientIP); locked {
		response.Error(c, 429, fmt.Sprintf("IP 已被临时锁定，请 %d 分钟后再试", int(remaining.Minutes())+1))
		return
	}

	// 检查账号是否被锁定
	if locked, remaining := loginLimiter.IsLocked(req.Email); locked {
		response.Error(c, 429, fmt.Sprintf("账号已被临时锁定，请 %d 分钟后再试", int(remaining.Minutes())+1))
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", req.AppKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	// 验证用户（客户）- 需要在同一租户内查找
	var customer model.Customer
	if err := model.DB.First(&customer, "email = ? AND tenant_id = ?", req.Email, app.TenantID).Error; err != nil {
		loginLimiter.RecordFailure(req.Email)
		ipLimiter.RecordFailure(clientIP)
		response.Error(c, 401, "账号或密码错误")
		return
	}

	// 使用支持预哈希的密码验证
	if !customer.CheckPasswordWithPreHash(req.Password, req.PasswordHashed) {
		locked, lockDuration := loginLimiter.RecordFailure(req.Email)
		ipLimiter.RecordFailure(clientIP)
		if locked {
			response.Error(c, 429, fmt.Sprintf("登录失败次数过多，账号已被锁定 %d 分钟", int(lockDuration.Minutes())))
		} else {
			response.Error(c, 401, "账号或密码错误")
		}
		return
	}

	if customer.Status != model.CustomerStatusActive {
		response.Error(c, 403, "账号已被禁用")
		return
	}

	// 登录成功，清除失败记录
	loginLimiter.RecordSuccess(req.Email)
	ipLimiter.RecordSuccess(clientIP)

	// 检查设备是否在黑名单
	var blacklist model.DeviceBlacklist
	if err := model.DB.Where("tenant_id = ? AND machine_id = ? AND (app_id = ? OR app_id IS NULL)", app.TenantID, req.MachineID, app.ID).First(&blacklist).Error; err == nil {
		response.Error(c, 403, "设备已被禁止使用")
		return
	}

	// 查找客户的订阅
	var subscription model.Subscription
	if err := model.DB.Where("customer_id = ? AND app_id = ? AND status = ?", customer.ID, app.ID, model.SubscriptionStatusActive).First(&subscription).Error; err != nil {
		response.Error(c, 403, "您没有该应用的有效订阅")
		return
	}

	// 检查订阅是否过期
	if !subscription.IsValid() {
		subscription.Status = model.SubscriptionStatusExpired
		model.DB.Save(&subscription)
		response.Error(c, 403, "订阅已过期")
		return
	}

	// 检查设备数量
	var deviceCount int64
	model.DB.Model(&model.Device{}).Where("tenant_id = ? AND subscription_id = ?", app.TenantID, subscription.ID).Count(&deviceCount)

	// 检查该设备是否已绑定
	var existingDevice model.Device
	deviceExists := model.DB.Where("tenant_id = ? AND subscription_id = ? AND machine_id = ?", app.TenantID, subscription.ID, req.MachineID).First(&existingDevice).Error == nil

	if !deviceExists && int(deviceCount) >= subscription.MaxDevices {
		response.Error(c, 403, "设备数量已达上限")
		return
	}

	// 绑定或更新设备
	var device model.Device
	now := time.Now()
	if deviceExists {
		device = existingDevice
		device.DeviceName = req.DeviceInfo.Name
		device.Hostname = req.DeviceInfo.Hostname
		device.OSType = req.DeviceInfo.OS
		device.OSVersion = req.DeviceInfo.OSVersion
		device.AppVersion = req.DeviceInfo.AppVersion
		device.IPAddress = c.ClientIP()
		device.LastActiveAt = &now
		if err := model.DB.Save(&device).Error; err != nil {
			response.ServerError(c, "更新设备失败: "+err.Error())
			return
		}
	} else {
		device = model.Device{
			TenantID:       app.TenantID,
			CustomerID:     customer.ID,
			SubscriptionID: &subscription.ID,
			MachineID:      req.MachineID,
			DeviceName:     req.DeviceInfo.Name,
			Hostname:       req.DeviceInfo.Hostname,
			OSType:         req.DeviceInfo.OS,
			OSVersion:      req.DeviceInfo.OSVersion,
			AppVersion:     req.DeviceInfo.AppVersion,
			IPAddress:      c.ClientIP(),
			Status:         model.DeviceStatusActive,
			LastActiveAt:   &now,
		}
		if err := model.DB.Create(&device).Error; err != nil {
			response.ServerError(c, "创建设备失败: "+err.Error())
			return
		}
	}

	// 解析 features
	var features []string
	if subscription.Features != "" {
		json.Unmarshal([]byte(subscription.Features), &features)
	}

	// 构建响应
	respData := gin.H{
		"valid":           true,
		"customer_id":     customer.ID,
		"subscription_id": subscription.ID,
		"device_id":       device.ID,
		"plan_type":       subscription.PlanType,
		"expire_at":       subscription.ExpireAt,
		"remaining_days":  subscription.RemainingDays(),
		"features":        features,
	}

	// 签名
	dataBytes, _ := json.Marshal(respData)
	signature, _ := crypto.Sign(app.PrivateKey, dataBytes)
	respData["signature"] = signature

	response.Success(c, respData)
}

// ClientRegisterRequest 客户端注册请求
type ClientRegisterRequest struct {
	AppKey         string `json:"app_key" binding:"required"`
	Email          string `json:"email" binding:"required,email"`
	Password       string `json:"password" binding:"required,min=6"`
	PasswordHashed bool   `json:"password_hashed"` // 标记密码是否已预哈希
	Name           string `json:"name"`
}

// ClientRegister 客户端用户注册
func (h *ClientHandler) ClientRegister(c *gin.Context) {
	var req ClientRegisterRequest
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

	// 检查邮箱是否已注册（同一租户内）
	var existingCustomer model.Customer
	if err := model.DB.First(&existingCustomer, "email = ? AND tenant_id = ?", req.Email, app.TenantID).Error; err == nil {
		response.Error(c, 400, "该邮箱已注册")
		return
	}

	// 创建客户
	customer := model.Customer{
		TenantID: app.TenantID,
		Email:    req.Email,
		Name:     req.Name,
		Status:   model.CustomerStatusActive,
	}
	// 使用支持预哈希的密码设置
	if err := customer.SetPasswordWithPreHash(req.Password, req.PasswordHashed); err != nil {
		response.ServerError(c, "密码处理失败")
		return
	}

	if err := model.DB.Create(&customer).Error; err != nil {
		response.ServerError(c, "注册失败")
		return
	}

	// 自动创建免费订阅
	now := time.Now()
	subscription := model.Subscription{
		TenantID:   app.TenantID,
		CustomerID: customer.ID,
		AppID:      app.ID,
		PlanType:   model.PlanTypeFree,
		MaxDevices: app.MaxDevicesDefault,
		Features:   "[]",
		Status:     model.SubscriptionStatusActive,
		StartAt:    &now,
	}
	model.DB.Create(&subscription)

	response.Success(c, gin.H{
		"customer_id":     customer.ID,
		"email":           customer.Email,
		"subscription_id": subscription.ID,
		"plan_type":       subscription.PlanType,
	})
}

// ==================== 订阅模式心跳和验证 ====================

// SubscriptionHeartbeatRequest 订阅模式心跳请求
type SubscriptionHeartbeatRequest struct {
	AppKey     string `json:"app_key" binding:"required"`
	MachineID  string `json:"machine_id" binding:"required"`
	AppVersion string `json:"app_version"`
}

// SubscriptionHeartbeat 订阅模式心跳
func (h *ClientHandler) SubscriptionHeartbeat(c *gin.Context) {
	var req SubscriptionHeartbeatRequest
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

	// 查找设备（订阅模式）- 按 machine_id 查找最新的记录
	var device model.Device
	if err := model.DB.Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ? AND subscription_id IS NOT NULL", req.MachineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 404, "设备未绑定")
		return
	}

	// 检查是否有订阅
	if device.SubscriptionID == nil || *device.SubscriptionID == "" {
		response.Error(c, 404, "设备未绑定订阅")
		return
	}

	if device.Subscription == nil || device.Subscription.AppID != app.ID {
		response.Error(c, 404, "设备未绑定该应用")
		return
	}

	// 检查订阅状态
	if !device.Subscription.IsValid() {
		response.Error(c, 403, "订阅无效或已过期")
		return
	}

	// 更新心跳时间
	now := time.Now()
	device.LastHeartbeatAt = &now
	device.LastActiveAt = &now
	device.IPAddress = c.ClientIP()
	if req.AppVersion != "" {
		device.AppVersion = req.AppVersion
	}
	model.DB.Save(&device)

	response.Success(c, gin.H{
		"valid":          true,
		"remaining_days": device.Subscription.RemainingDays(),
	})
}

// SubscriptionVerifyRequest 订阅模式验证请求
type SubscriptionVerifyRequest struct {
	AppKey    string `json:"app_key" binding:"required"`
	MachineID string `json:"machine_id" binding:"required"`
}

// SubscriptionVerify 订阅模式验证
func (h *ClientHandler) SubscriptionVerify(c *gin.Context) {
	var req SubscriptionVerifyRequest
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

	// 查找设备（订阅模式）- 按 machine_id 查找最新的记录
	var device model.Device
	if err := model.DB.Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ? AND subscription_id IS NOT NULL", req.MachineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 404, "设备未绑定")
		return
	}

	// 检查是否有订阅
	if device.SubscriptionID == nil || *device.SubscriptionID == "" {
		response.Error(c, 404, "设备未绑定订阅")
		return
	}

	// 检查订阅是否属于该应用
	if device.Subscription == nil || device.Subscription.AppID != app.ID {
		response.Error(c, 404, "设备未绑定该应用的订阅")
		return
	}

	subscription := device.Subscription

	// 检查订阅状态
	if subscription.Status == model.SubscriptionStatusCancelled {
		response.Error(c, 403, "订阅已取消")
		return
	}

	if subscription.Status == model.SubscriptionStatusSuspended {
		response.Error(c, 403, "订阅已暂停")
		return
	}

	// 检查是否过期
	if subscription.ExpireAt != nil && time.Now().After(*subscription.ExpireAt) {
		subscription.Status = model.SubscriptionStatusExpired
		model.DB.Save(subscription)
		response.Error(c, 403, "订阅已过期")
		return
	}

	// 更新活跃时间
	now := time.Now()
	device.LastActiveAt = &now
	model.DB.Save(&device)

	// 解析 features
	var features []string
	if subscription.Features != "" {
		json.Unmarshal([]byte(subscription.Features), &features)
	}

	// 构建响应
	respData := gin.H{
		"valid":           true,
		"subscription_id": subscription.ID,
		"plan_type":       subscription.PlanType,
		"expire_at":       subscription.ExpireAt,
		"remaining_days":  subscription.RemainingDays(),
		"features":        features,
	}

	// 签名
	dataBytes, _ := json.Marshal(respData)
	signature, _ := crypto.Sign(app.PrivateKey, dataBytes)
	respData["signature"] = signature

	response.Success(c, respData)
}
