package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/clientauth"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"license-server/internal/service"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ClientHandler struct{}

func NewClientHandler() *ClientHandler {
	return &ClientHandler{}
}

func appAllowsClientAuthMode(app *model.Application, authMode string) bool {
	if app == nil {
		return false
	}
	switch app.AuthMode {
	case "", model.AuthModeBoth:
		return true
	case model.AuthModeLicense:
		return authMode == clientauth.AuthModeLicense
	case model.AuthModeSubscription:
		return authMode == clientauth.AuthModeSubscription
	default:
		return false
	}
}

func signClientPayload(app *model.Application, payload gin.H) error {
	if app == nil {
		return errors.New("invalid app")
	}
	signPayload := gin.H{}
	for key, value := range payload {
		if key != "signature" {
			signPayload[key] = value
		}
	}
	dataBytes, err := json.Marshal(signPayload)
	if err != nil {
		return err
	}
	signature, err := crypto.Sign(app.PrivateKey, dataBytes)
	if err != nil {
		return err
	}
	payload["signature"] = signature
	return nil
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

func updateClientDeviceForActivation(tx *gorm.DB, device *model.Device, customerID string, info DeviceInfo, ip string, now time.Time) error {
	if device.Status == model.DeviceStatusBlacklisted {
		return errors.New("设备已被禁止使用")
	}
	device.CustomerID = customerID
	device.DeviceName = info.Name
	device.Hostname = info.Hostname
	device.OSType = info.OS
	device.OSVersion = info.OSVersion
	device.AppVersion = info.AppVersion
	device.IPAddress = ip
	device.Status = model.DeviceStatusActive
	device.LastActiveAt = &now
	device.DeletedAt = gorm.DeletedAt{}
	return tx.Unscoped().Save(device).Error
}

func restoreDeletedLicenseDevice(tx *gorm.DB, tenantID, licenseID, machineID, customerID string, info DeviceInfo, ip string, now time.Time) (*model.Device, bool, error) {
	var device model.Device
	err := tx.Unscoped().
		Where("tenant_id = ? AND license_id = ? AND machine_id = ?", tenantID, licenseID, machineID).
		Where("deleted_at IS NOT NULL").
		Order("deleted_at DESC").
		First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if device.Status == model.DeviceStatusBlacklisted {
		return nil, false, errors.New("设备已被禁止使用")
	}
	if err := updateClientDeviceForActivation(tx, &device, customerID, info, ip, now); err != nil {
		return nil, false, err
	}
	return &device, true, nil
}

func restoreDeletedSubscriptionDevice(tx *gorm.DB, tenantID, subscriptionID, machineID, customerID string, info DeviceInfo, ip string, now time.Time) (*model.Device, bool, error) {
	var device model.Device
	err := tx.Unscoped().
		Where("tenant_id = ? AND subscription_id = ? AND machine_id = ?", tenantID, subscriptionID, machineID).
		Where("deleted_at IS NOT NULL").
		Order("deleted_at DESC").
		First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if device.Status == model.DeviceStatusBlacklisted {
		return nil, false, errors.New("设备已被禁止使用")
	}
	if err := updateClientDeviceForActivation(tx, &device, customerID, info, ip, now); err != nil {
		return nil, false, err
	}
	return &device, true, nil
}

// ActivateResponse 激活响应
type ActivateResponse struct {
	Valid            bool       `json:"valid"`
	LicenseID        string     `json:"license_id"`
	DeviceID         string     `json:"device_id"`
	Type             string     `json:"type"`
	ExpireAt         *time.Time `json:"expire_at"`
	RemainingDays    int        `json:"remaining_days"`
	Features         []string   `json:"features"`
	Signature        string     `json:"signature"`
	AccessToken      string     `json:"access_token,omitempty"`
	RefreshToken     string     `json:"refresh_token,omitempty"`
	TokenType        string     `json:"token_type,omitempty"`
	ExpiresIn        int        `json:"expires_in,omitempty"`
	RefreshExpiresIn int        `json:"refresh_expires_in,omitempty"`
	AccessExpiresAt  int64      `json:"access_expires_at,omitempty"`
	RefreshExpiresAt int64      `json:"refresh_expires_at,omitempty"`
	SessionID        string     `json:"session_id,omitempty"`
	AuthMode         string     `json:"auth_mode,omitempty"`
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
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeLicense) {
		response.Error(c, 403, "该应用未启用授权码模式")
		return
	}

	if err := ensureDeviceNotBlacklistedForApp(req.MachineID, &app); err != nil {
		response.Error(c, 403, err.Error())
		return
	}

	var license model.License
	var device model.Device
	customerID := ""
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&license, "license_key = ? AND app_id = ?", req.LicenseKey, app.ID).Error; err != nil {
			return err
		}
		if license.CustomerID == nil || *license.CustomerID == "" {
			return errors.New("授权码未绑定客户")
		}
		customerID = *license.CustomerID

		if license.Status == model.LicenseStatusRevoked {
			return errors.New("授权已被吊销")
		}
		if license.Status == model.LicenseStatusSuspended {
			return errors.New("授权已被暂停")
		}

		now := time.Now()
		if license.Status == model.LicenseStatusPending {
			license.ActivatedAt = &now
			if license.DurationDays == -1 {
				license.ExpireAt = nil
			} else {
				expireAt := now.AddDate(0, 0, license.DurationDays)
				license.ExpireAt = &expireAt
				graceExpireAt := expireAt.AddDate(0, 0, app.GracePeriodDays)
				license.GraceExpireAt = &graceExpireAt
			}
			license.Status = model.LicenseStatusActive

			event := model.LicenseEvent{
				LicenseID:    license.ID,
				EventType:    model.LicenseEventActivated,
				OperatorType: "user",
				IPAddress:    c.ClientIP(),
			}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
		}

		if license.ExpireAt != nil && now.After(*license.ExpireAt) {
			if license.GraceExpireAt == nil || now.After(*license.GraceExpireAt) {
				license.Status = model.LicenseStatusExpired
				_ = tx.Save(&license).Error
				return errors.New("授权已过期")
			}
		}

		var deviceCount int64
		if err := tx.Model(&model.Device{}).Where("tenant_id = ? AND license_id = ?", license.TenantID, license.ID).Count(&deviceCount).Error; err != nil {
			return err
		}

		var existingDevice model.Device
		err := tx.Where("tenant_id = ? AND license_id = ? AND machine_id = ?", license.TenantID, license.ID, req.MachineID).First(&existingDevice).Error
		deviceExists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !deviceExists && int(deviceCount) >= license.MaxDevices {
			return errors.New("设备数量已达上限")
		}

		if deviceExists {
			device = existingDevice
			if err := updateClientDeviceForActivation(tx, &device, customerID, req.DeviceInfo, c.ClientIP(), now); err != nil {
				return err
			}
		} else if restoredDevice, restored, err := restoreDeletedLicenseDevice(tx, license.TenantID, license.ID, req.MachineID, customerID, req.DeviceInfo, c.ClientIP(), now); err != nil {
			return err
		} else if restored {
			device = *restoredDevice
		} else {
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
			if err := tx.Create(&device).Error; err != nil {
				return err
			}
		}

		license.LastValidatedAt = &now
		return tx.Save(&license).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, 400, "无效的授权码")
			return
		}
		switch err.Error() {
		case "授权码未绑定客户", "授权已被吊销", "授权已被暂停", "授权已过期", "设备数量已达上限", "设备已被禁止使用":
			response.Error(c, 403, err.Error())
		default:
			response.ServerError(c, "激活授权失败: "+err.Error())
		}
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

	sessionTokens, err := h.issueClientSession(c, &app, &device, customerID, clientauth.AuthModeLicense)
	if err != nil {
		response.ServerError(c, "创建客户端会话失败")
		return
	}
	respData.AccessToken = sessionTokens.AccessToken
	respData.RefreshToken = sessionTokens.RefreshToken
	respData.TokenType = sessionTokens.TokenType
	respData.ExpiresIn = sessionTokens.ExpiresIn
	respData.RefreshExpiresIn = sessionTokens.RefreshExpiresIn
	respData.AccessExpiresAt = sessionTokens.AccessExpiresAt
	respData.RefreshExpiresAt = sessionTokens.RefreshExpiresAt
	respData.SessionID = sessionTokens.SessionID
	respData.AuthMode = sessionTokens.AuthMode

	// 签名响应（使用与 SDK 一致的 key-sorted JSON，且不包含 signature 字段）
	signPayload := map[string]interface{}{
		"valid":              respData.Valid,
		"license_id":         respData.LicenseID,
		"device_id":          respData.DeviceID,
		"type":               respData.Type,
		"expire_at":          respData.ExpireAt,
		"remaining_days":     respData.RemainingDays,
		"features":           respData.Features,
		"access_token":       respData.AccessToken,
		"refresh_token":      respData.RefreshToken,
		"token_type":         respData.TokenType,
		"expires_in":         respData.ExpiresIn,
		"refresh_expires_in": respData.RefreshExpiresIn,
		"access_expires_at":  respData.AccessExpiresAt,
		"refresh_expires_at": respData.RefreshExpiresAt,
		"session_id":         respData.SessionID,
		"auth_mode":          respData.AuthMode,
	}
	dataBytes, _ := json.Marshal(signPayload)
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
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeLicense) {
		response.Error(c, 403, "该应用未启用授权码模式")
		return
	}

	// 查找当前应用下有效授权设备
	device, err := loadActiveLicenseDeviceForApp(req.MachineID, &app)
	if err != nil {
		response.Error(c, 403, err.Error())
		return
	}
	license := device.License

	// 更新验证时间
	now := time.Now()
	license.LastValidatedAt = &now
	device.LastActiveAt = &now
	if err := model.DB.Save(license).Error; err != nil {
		response.ServerError(c, "更新授权状态失败: "+err.Error())
		return
	}
	if err := model.DB.Save(device).Error; err != nil {
		response.ServerError(c, "更新设备状态失败: "+err.Error())
		return
	}

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

	if err := signClientPayload(&app, respData); err != nil {
		response.ServerError(c, "签名响应失败")
		return
	}

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
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeLicense) {
		response.Error(c, 403, "该应用未启用授权码模式")
		return
	}

	// 查找当前应用下有效授权设备
	device, err := loadActiveLicenseDeviceForApp(req.MachineID, &app)
	if err != nil {
		response.Error(c, 403, err.Error())
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
	if err := model.DB.Save(device).Error; err != nil {
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
		AuthMode:   clientauth.AuthModeLicense,
		LicenseID:  *device.LicenseID,
		DeviceID:   device.ID,
		IPAddress:  c.ClientIP(),
		AppVersion: req.AppVersion,
	}
	if err := model.DB.Create(&heartbeat).Error; err != nil {
		response.ServerError(c, "记录心跳失败: "+err.Error())
		return
	}

	respData := gin.H{
		"valid":          true,
		"remaining_days": device.License.RemainingDays(),
	}
	if err := signClientPayload(&app, respData); err != nil {
		response.ServerError(c, "签名响应失败")
		return
	}
	response.Success(c, respData)
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
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeLicense) {
		response.Error(c, 403, "该应用未启用授权码模式")
		return
	}

	// 查找当前应用下有效授权设备
	device, err := loadActiveLicenseDeviceForApp(req.MachineID, &app)
	if err != nil {
		response.Error(c, 403, err.Error())
		return
	}

	now := time.Now()
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if device.LicenseID == nil || *device.LicenseID == "" {
			return errors.New("设备授权信息异常")
		}
		if err := increaseLicenseUnbindUsed(tx, *device.LicenseID, app.TenantID, app.ID); err != nil {
			return err
		}
		if err := tx.Delete(&model.Device{}, "id = ?", device.ID).Error; err != nil {
			return err
		}
		return revokeActiveClientSessionsForDevice(tx, device.ID, now)
	}); err != nil {
		if errors.Is(err, errClientUnbindLimitExceeded) {
			response.Error(c, 400, clientUnbindLimitExceededMessage)
			return
		}
		response.ServerError(c, "解绑失败")
		return
	}

	response.SuccessWithMessage(c, "解绑成功", nil)
}

// GetScriptVersion 获取脚本版本
func (h *ClientHandler) GetScriptVersion(c *gin.Context) {
	app, ok := loadClientAppFromSession(c)
	if !ok {
		return
	}
	device, ok := loadClientDeviceFromSession(c, app)
	if !ok {
		return
	}

	var scripts []model.Script
	model.DB.Where("app_id = ? AND status = ?", app.ID, model.ScriptStatusActive).
		Order("filename ASC").Find(&scripts)

	items := make([]gin.H, 0, len(scripts))
	var lastUpdated time.Time
	for _, script := range scripts {
		if !isMachineInRollout(device.MachineID, script.RolloutPercentage) {
			continue
		}
		if script.UpdatedAt.After(lastUpdated) {
			lastUpdated = script.UpdatedAt
		}
		items = append(items, gin.H{
			"filename":     script.Filename,
			"version":      script.Version,
			"version_code": scriptVersionCode(script.Version),
			"file_size":    script.FileSize,
			"file_hash":    script.ContentHash,
			"updated_at":   script.UpdatedAt,
		})
	}

	lastUpdatedText := ""
	if !lastUpdated.IsZero() {
		lastUpdatedText = lastUpdated.Format(time.RFC3339)
	}
	response.Success(c, gin.H{
		"scripts":      items,
		"total_count":  len(items),
		"last_updated": lastUpdatedText,
	})
}

// DownloadScript 下载脚本
func (h *ClientHandler) DownloadScript(c *gin.Context) {
	filename := c.Param("filename")

	app, ok := loadClientAppFromSession(c)
	if !ok {
		return
	}

	// 验证设备授权或订阅
	device, ok := loadClientDeviceFromSession(c, app)
	if !ok {
		return
	}

	// 获取脚本
	var script model.Script
	if err := model.DB.Where("app_id = ? AND filename = ? AND status = ?", app.ID, filename, model.ScriptStatusActive).First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}
	if !isMachineInRollout(device.MachineID, script.RolloutPercentage) {
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
	app, ok := loadClientAppFromSession(c)
	if !ok {
		return
	}
	device, ok := loadClientDeviceFromSession(c, app)
	if !ok {
		return
	}

	var releases []model.AppRelease
	if err := model.DB.Where("app_id = ? AND status = ?", app.ID, model.ReleaseStatusPublished).
		Order("version_code DESC").Find(&releases).Error; err != nil {
		response.ServerError(c, "查询发布版本失败")
		return
	}
	var release model.AppRelease
	found := false
	for _, candidate := range releases {
		if isMachineInRollout(device.MachineID, candidate.RolloutPercentage) {
			release = candidate
			found = true
			break
		}
	}
	if !found {
		response.NotFound(c, "暂无发布版本")
		return
	}
	downloadURL, err := buildClientDownloadURLWithToken(release.DownloadURL, app.TenantID, app.ID, device.MachineID, downloadTokenKindRelease)
	if err != nil {
		response.ServerError(c, "生成下载链接失败")
		return
	}

	response.Success(c, gin.H{
		"version":        release.Version,
		"version_code":   release.VersionCode,
		"download_url":   downloadURL,
		"changelog":      release.Changelog,
		"file_size":      release.FileSize,
		"file_hash":      release.FileHash,
		"file_signature": release.FileSignature,
		"signature_alg":  fileSignatureAlgorithm,
		"force_update":   release.ForceUpdate,
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
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeSubscription) {
		response.Error(c, 403, "该应用未启用账号订阅模式")
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

	if err := ensureDeviceNotBlacklistedForApp(req.MachineID, &app); err != nil {
		response.Error(c, 403, err.Error())
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
		if err := model.DB.Save(&subscription).Error; err != nil {
			response.ServerError(c, "更新订阅状态失败: "+err.Error())
			return
		}
		response.Error(c, 403, "订阅已过期")
		return
	}

	var device model.Device
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&subscription, "id = ? AND tenant_id = ?", subscription.ID, app.TenantID).Error; err != nil {
			return err
		}
		if !subscription.IsValid() {
			subscription.Status = model.SubscriptionStatusExpired
			_ = tx.Save(&subscription).Error
			return errors.New("订阅已过期")
		}

		var deviceCount int64
		if err := tx.Model(&model.Device{}).Where("tenant_id = ? AND subscription_id = ?", app.TenantID, subscription.ID).Count(&deviceCount).Error; err != nil {
			return err
		}

		var existingDevice model.Device
		err := tx.Where("tenant_id = ? AND subscription_id = ? AND machine_id = ?", app.TenantID, subscription.ID, req.MachineID).First(&existingDevice).Error
		deviceExists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !deviceExists && int(deviceCount) >= subscription.MaxDevices {
			return errors.New("设备数量已达上限")
		}

		now := time.Now()
		if deviceExists {
			device = existingDevice
			return updateClientDeviceForActivation(tx, &device, customer.ID, req.DeviceInfo, c.ClientIP(), now)
		}

		if restoredDevice, restored, err := restoreDeletedSubscriptionDevice(tx, app.TenantID, subscription.ID, req.MachineID, customer.ID, req.DeviceInfo, c.ClientIP(), now); err != nil {
			return err
		} else if restored {
			device = *restoredDevice
			return nil
		}

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
		return tx.Create(&device).Error
	}); err != nil {
		switch err.Error() {
		case "订阅已过期", "设备数量已达上限", "设备已被禁止使用":
			response.Error(c, 403, err.Error())
		default:
			response.ServerError(c, "绑定设备失败: "+err.Error())
		}
		return
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

	sessionTokens, err := h.issueClientSession(c, &app, &device, customer.ID, clientauth.AuthModeSubscription)
	if err != nil {
		response.ServerError(c, "创建客户端会话失败")
		return
	}
	respData["access_token"] = sessionTokens.AccessToken
	respData["refresh_token"] = sessionTokens.RefreshToken
	respData["token_type"] = sessionTokens.TokenType
	respData["expires_in"] = sessionTokens.ExpiresIn
	respData["refresh_expires_in"] = sessionTokens.RefreshExpiresIn
	respData["access_expires_at"] = sessionTokens.AccessExpiresAt
	respData["refresh_expires_at"] = sessionTokens.RefreshExpiresAt
	respData["session_id"] = sessionTokens.SessionID
	respData["auth_mode"] = sessionTokens.AuthMode

	if err := signClientPayload(&app, respData); err != nil {
		response.ServerError(c, "签名响应失败")
		return
	}

	response.Success(c, respData)
}

// ClientRegisterRequest 客户端注册请求
type ClientRegisterRequest struct {
	AppKey         string `json:"app_key" binding:"required"`
	Email          string `json:"email" binding:"required,email"`
	Password       string `json:"password" binding:"required"`
	PasswordHashed bool   `json:"password_hashed"` // 标记密码是否已预哈希
	Name           string `json:"name"`
}

func defaultCustomerOwnerID(tenantID string) string {
	var owner model.TeamMember
	if err := model.DB.
		Where("tenant_id = ? AND role = ? AND status = ?", tenantID, model.RoleOwner, model.MemberStatusActive).
		Order("created_at ASC").
		First(&owner).Error; err == nil {
		return owner.ID
	}
	if err := model.DB.
		Where("tenant_id = ? AND role IN ? AND status = ?", tenantID, []model.TeamMemberRole{model.RoleOwner, model.RoleAdmin}, model.MemberStatusActive).
		Order("created_at ASC").
		First(&owner).Error; err == nil {
		return owner.ID
	}
	return ""
}

// ClientRegister 客户端用户注册
func (h *ClientHandler) ClientRegister(c *gin.Context) {
	var req ClientRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if req.PasswordHashed {
		response.BadRequest(c, "注册必须提交原始密码")
		return
	}
	if err := validatePasswordPolicy(req.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", req.AppKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeSubscription) {
		response.Error(c, 403, "该应用未启用账号订阅模式")
		return
	}

	// 检查邮箱是否已注册（同一租户内）
	var existingCustomer model.Customer
	if err := model.DB.Unscoped().First(&existingCustomer, "email = ? AND tenant_id = ?", req.Email, app.TenantID).Error; err == nil {
		response.Error(c, 400, "该邮箱已注册")
		return
	}

	// 创建客户
	customer := model.Customer{
		TenantID: app.TenantID,
		OwnerID:  defaultCustomerOwnerID(app.TenantID),
		Email:    req.Email,
		Name:     req.Name,
		Status:   model.CustomerStatusActive,
	}
	// 使用支持预哈希的密码设置
	if err := customer.SetPasswordWithPreHash(req.Password, req.PasswordHashed); err != nil {
		response.ServerError(c, "密码处理失败")
		return
	}

	// 自动创建免费订阅
	now := time.Now()
	subscription := model.Subscription{
		TenantID:   app.TenantID,
		AppID:      app.ID,
		PlanType:   model.PlanTypeFree,
		MaxDevices: app.MaxDevicesDefault,
		Features:   "[]",
		Status:     model.SubscriptionStatusActive,
		StartAt:    &now,
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&customer).Error; err != nil {
			return err
		}
		subscription.CustomerID = customer.ID
		return tx.Create(&subscription).Error
	}); err != nil {
		response.ServerError(c, "注册失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"customer_id":     customer.ID,
		"email":           customer.Email,
		"subscription_id": subscription.ID,
		"plan_type":       subscription.PlanType,
	})
}

// ClientChangePassword 修改客户端账号密码
func (h *ClientHandler) ClientChangePassword(c *gin.Context) {
	var req struct {
		OldPassword    string `json:"old_password" binding:"required"`
		NewPassword    string `json:"new_password" binding:"required"`
		PasswordHashed bool   `json:"password_hashed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	if req.PasswordHashed {
		response.BadRequest(c, "修改密码必须提交原始密码")
		return
	}
	if err := validatePasswordPolicy(req.NewPassword); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	tenantID := middleware.GetClientTenantID(c)
	customerID := middleware.GetClientCustomerID(c)
	authMode := middleware.GetClientAuthMode(c)
	if tenantID == "" || customerID == "" {
		response.Unauthorized(c, "会话无效")
		return
	}
	if authMode != clientauth.AuthModeSubscription {
		response.Forbidden(c, "授权码模式不支持修改账号密码")
		return
	}

	var customer model.Customer
	if err := model.DB.First(&customer, "id = ? AND tenant_id = ?", customerID, tenantID).Error; err != nil {
		response.Unauthorized(c, "账号不存在")
		return
	}
	if !customer.CheckPasswordWithPreHash(req.OldPassword, req.PasswordHashed) {
		response.Error(c, 400, "原密码错误")
		return
	}
	if err := customer.SetPasswordWithPreHash(req.NewPassword, req.PasswordHashed); err != nil {
		response.ServerError(c, "密码处理失败")
		return
	}
	if err := model.DB.Save(&customer).Error; err != nil {
		response.ServerError(c, "修改密码失败")
		return
	}

	response.SuccessWithMessage(c, "密码修改成功", nil)
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
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeSubscription) {
		response.Error(c, 403, "该应用未启用账号订阅模式")
		return
	}

	// 查找当前应用下有效订阅设备
	device, err := loadActiveSubscriptionDeviceForApp(req.MachineID, &app)
	if err != nil {
		response.Error(c, 403, err.Error())
		return
	}
	if device.SubscriptionID == nil || *device.SubscriptionID == "" {
		response.Error(c, 400, "设备订阅信息异常")
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
	if err := model.DB.Save(device).Error; err != nil {
		response.ServerError(c, "更新设备状态失败: "+err.Error())
		return
	}
	heartbeat := model.Heartbeat{
		TenantID:       device.TenantID,
		AuthMode:       clientauth.AuthModeSubscription,
		SubscriptionID: *device.SubscriptionID,
		DeviceID:       device.ID,
		IPAddress:      c.ClientIP(),
		AppVersion:     req.AppVersion,
	}
	if err := model.DB.Create(&heartbeat).Error; err != nil {
		response.ServerError(c, "记录心跳失败: "+err.Error())
		return
	}

	respData := gin.H{
		"valid":          true,
		"remaining_days": device.Subscription.RemainingDays(),
	}
	if err := signClientPayload(&app, respData); err != nil {
		response.ServerError(c, "签名响应失败")
		return
	}
	response.Success(c, respData)
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
	if !appAllowsClientAuthMode(&app, clientauth.AuthModeSubscription) {
		response.Error(c, 403, "该应用未启用账号订阅模式")
		return
	}

	// 查找当前应用下有效订阅设备
	device, err := loadActiveSubscriptionDeviceForApp(req.MachineID, &app)
	if err != nil {
		response.Error(c, 403, err.Error())
		return
	}
	subscription := device.Subscription

	// 更新活跃时间
	now := time.Now()
	device.LastActiveAt = &now
	if err := model.DB.Save(device).Error; err != nil {
		response.ServerError(c, "更新设备状态失败: "+err.Error())
		return
	}

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

	if err := signClientPayload(&app, respData); err != nil {
		response.ServerError(c, "签名响应失败")
		return
	}

	response.Success(c, respData)
}
