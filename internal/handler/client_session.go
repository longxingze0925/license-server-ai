package handler

import (
	"errors"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/clientauth"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type clientSessionTokenPayload struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	AccessExpiresAt  int64  `json:"access_expires_at"`
	RefreshExpiresAt int64  `json:"refresh_expires_at"`
	SessionID        string `json:"session_id"`
	AuthMode         string `json:"auth_mode"`
}

func (h *ClientHandler) issueClientSession(c *gin.Context, app *model.Application, device *model.Device, customerID, authMode string) (*clientSessionTokenPayload, error) {
	if app == nil || device == nil || authMode == "" {
		return nil, errors.New("invalid session context")
	}

	now := time.Now()
	_ = model.DB.Model(&model.ClientSession{}).
		Where("device_id = ? AND revoked_at IS NULL", device.ID).
		Updates(map[string]interface{}{
			"revoked_at":   now,
			"last_used_at": now,
		}).Error

	refreshToken, refreshTokenHash, refreshExpiresAt := clientauth.GenerateRefreshToken()
	session := model.ClientSession{
		TenantID:         app.TenantID,
		AppID:            app.ID,
		CustomerID:       customerID,
		DeviceID:         device.ID,
		MachineID:        device.MachineID,
		AuthMode:         authMode,
		RefreshTokenHash: refreshTokenHash,
		UserAgent:        c.Request.UserAgent(),
		ClientIP:         c.ClientIP(),
		LastUsedAt:       &now,
		ExpiresAt:        refreshExpiresAt,
	}

	if err := model.DB.Create(&session).Error; err != nil {
		return nil, err
	}

	accessToken, accessExpiresAt, err := clientauth.GenerateAccessToken(
		session.ID,
		app.TenantID,
		app.ID,
		customerID,
		device.ID,
		device.MachineID,
		authMode,
	)
	if err != nil {
		_ = model.DB.Delete(&session).Error
		return nil, err
	}

	return &clientSessionTokenPayload{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        clientauth.GetAccessTokenExpireSeconds(),
		RefreshExpiresIn: clientauth.GetRefreshTokenExpireSeconds(),
		AccessExpiresAt:  accessExpiresAt.Unix(),
		RefreshExpiresAt: refreshExpiresAt.Unix(),
		SessionID:        session.ID,
		AuthMode:         authMode,
	}, nil
}

// ClientRefresh 客户端刷新 access token
func (h *ClientHandler) ClientRefresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	refreshHash := crypto.SHA256HashString(req.RefreshToken)
	var session model.ClientSession
	if err := model.DB.First(&session, "refresh_token_hash = ? AND revoked_at IS NULL", refreshHash).Error; err != nil {
		response.Unauthorized(c, "refresh token 无效")
		return
	}

	now := time.Now()
	if session.IsExpired(now) {
		_ = model.DB.Model(&model.ClientSession{}).
			Where("id = ?", session.ID).
			Update("revoked_at", now).Error
		response.Unauthorized(c, "refresh token 已过期，请重新登录")
		return
	}

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND status = ?", session.AppID, model.AppStatusActive).Error; err != nil {
		response.Unauthorized(c, "应用已失效，请重新登录")
		return
	}

	var device model.Device
	if err := model.DB.First(&device, "id = ? AND tenant_id = ?", session.DeviceID, session.TenantID).Error; err != nil {
		response.Unauthorized(c, "设备已解绑，请重新登录")
		return
	}
	if device.MachineID != session.MachineID {
		_ = model.DB.Model(&model.ClientSession{}).
			Where("id = ?", session.ID).
			Update("revoked_at", now).Error
		response.Unauthorized(c, "设备信息不一致，请重新登录")
		return
	}
	if device.Status == model.DeviceStatusBlacklisted {
		_ = model.DB.Model(&model.ClientSession{}).
			Where("id = ?", session.ID).
			Update("revoked_at", now).Error
		response.Unauthorized(c, "设备已被禁止使用")
		return
	}
	if err := ensureDeviceNotBlacklistedForApp(session.MachineID, &app); err != nil {
		_ = model.DB.Model(&model.ClientSession{}).
			Where("id = ?", session.ID).
			Update("revoked_at", now).Error
		response.Unauthorized(c, err.Error())
		return
	}

	switch session.AuthMode {
	case clientauth.AuthModeLicense:
		if device.LicenseID == nil || *device.LicenseID == "" {
			_ = model.DB.Model(&model.ClientSession{}).
				Where("id = ?", session.ID).
				Update("revoked_at", now).Error
			response.Unauthorized(c, "授权已失效，请重新激活")
			return
		}
		var license model.License
		if err := model.DB.First(&license, "id = ? AND tenant_id = ?", *device.LicenseID, session.TenantID).Error; err != nil {
			_ = model.DB.Model(&model.ClientSession{}).
				Where("id = ?", session.ID).
				Update("revoked_at", now).Error
			response.Unauthorized(c, "授权已失效，请重新激活")
			return
		}
		if license.AppID != session.AppID || !license.IsValid() {
			_ = model.DB.Model(&model.ClientSession{}).
				Where("id = ?", session.ID).
				Update("revoked_at", now).Error
			response.Unauthorized(c, "授权无效，请重新激活")
			return
		}
	case clientauth.AuthModeSubscription:
		if device.SubscriptionID == nil || *device.SubscriptionID == "" {
			_ = model.DB.Model(&model.ClientSession{}).
				Where("id = ?", session.ID).
				Update("revoked_at", now).Error
			response.Unauthorized(c, "订阅已失效，请重新登录")
			return
		}
		var subscription model.Subscription
		if err := model.DB.First(&subscription, "id = ? AND tenant_id = ?", *device.SubscriptionID, session.TenantID).Error; err != nil {
			_ = model.DB.Model(&model.ClientSession{}).
				Where("id = ?", session.ID).
				Update("revoked_at", now).Error
			response.Unauthorized(c, "订阅已失效，请重新登录")
			return
		}
		if subscription.AppID != session.AppID || !subscription.IsValid() {
			_ = model.DB.Model(&model.ClientSession{}).
				Where("id = ?", session.ID).
				Update("revoked_at", now).Error
			response.Unauthorized(c, "订阅无效，请重新登录")
			return
		}
		if session.CustomerID != "" && subscription.CustomerID != session.CustomerID {
			_ = model.DB.Model(&model.ClientSession{}).
				Where("id = ?", session.ID).
				Update("revoked_at", now).Error
			response.Unauthorized(c, "订阅归属已变更，请重新登录")
			return
		}
	default:
		_ = model.DB.Model(&model.ClientSession{}).
			Where("id = ?", session.ID).
			Update("revoked_at", now).Error
		response.Unauthorized(c, "会话模式无效，请重新登录")
		return
	}

	newRefreshToken, newRefreshHash, newRefreshExpiresAt := clientauth.GenerateRefreshToken()
	accessToken, accessExpiresAt, err := clientauth.GenerateAccessToken(
		session.ID,
		session.TenantID,
		session.AppID,
		session.CustomerID,
		session.DeviceID,
		session.MachineID,
		session.AuthMode,
	)
	if err != nil {
		response.ServerError(c, "生成访问令牌失败")
		return
	}

	if err := model.DB.Model(&model.ClientSession{}).
		Where("id = ? AND revoked_at IS NULL", session.ID).
		Updates(map[string]interface{}{
			"refresh_token_hash": newRefreshHash,
			"expires_at":         newRefreshExpiresAt,
			"last_used_at":       now,
			"client_ip":          c.ClientIP(),
			"user_agent":         c.Request.UserAgent(),
		}).Error; err != nil {
		response.ServerError(c, "刷新会话失败")
		return
	}

	response.Success(c, gin.H{
		"access_token":       accessToken,
		"refresh_token":      newRefreshToken,
		"token_type":         "Bearer",
		"expires_in":         clientauth.GetAccessTokenExpireSeconds(),
		"refresh_expires_in": clientauth.GetRefreshTokenExpireSeconds(),
		"access_expires_at":  accessExpiresAt.Unix(),
		"refresh_expires_at": newRefreshExpiresAt.Unix(),
		"session_id":         session.ID,
		"auth_mode":          session.AuthMode,
		"device_id":          device.ID,
	})
}

// ClientLogout 客户端注销当前会话
func (h *ClientHandler) ClientLogout(c *gin.Context) {
	sessionID := middleware.GetClientSessionID(c)
	if sessionID == "" {
		response.Unauthorized(c, "会话无效")
		return
	}

	now := time.Now()
	if err := model.DB.Model(&model.ClientSession{}).
		Where("id = ? AND revoked_at IS NULL", sessionID).
		Updates(map[string]interface{}{
			"revoked_at":   now,
			"last_used_at": now,
		}).Error; err != nil {
		response.ServerError(c, "注销失败")
		return
	}

	response.SuccessWithMessage(c, "注销成功", nil)
}

// UnbindCurrentDevice 客户端自助解绑当前设备
func (h *ClientHandler) UnbindCurrentDevice(c *gin.Context) {
	var req struct {
		Password       string `json:"password"`
		PasswordHashed bool   `json:"password_hashed"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "参数错误: "+err.Error())
			return
		}
	}

	tenantID := middleware.GetClientTenantID(c)
	appID := middleware.GetClientAppID(c)
	deviceID := middleware.GetClientDeviceID(c)
	customerID := middleware.GetClientCustomerID(c)
	authMode := middleware.GetClientAuthMode(c)
	if tenantID == "" || appID == "" || deviceID == "" {
		response.Unauthorized(c, "会话无效")
		return
	}

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ? AND status = ?", appID, tenantID, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}
	if !appAllowsClientAuthMode(&app, authMode) {
		response.Forbidden(c, "该应用未启用当前会话模式")
		return
	}

	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		First(&device, "id = ? AND tenant_id = ?", deviceID, tenantID).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	if authMode == clientauth.AuthModeSubscription {
		if customerID == "" {
			response.Unauthorized(c, "订阅会话缺少用户信息")
			return
		}
		if req.Password == "" {
			response.BadRequest(c, "订阅解绑需要提供账号密码")
			return
		}

		var customer model.Customer
		if err := model.DB.First(&customer, "id = ? AND tenant_id = ?", customerID, tenantID).Error; err != nil {
			response.Unauthorized(c, "账号不存在")
			return
		}
		if !customer.CheckPasswordWithPreHash(req.Password, req.PasswordHashed) {
			response.Unauthorized(c, "账号或密码错误")
			return
		}
		if device.CustomerID != customerID {
			response.Forbidden(c, "当前账号无权解绑该设备")
			return
		}

		if device.Subscription == nil || device.Subscription.AppID != app.ID {
			response.Forbidden(c, "设备未绑定当前应用订阅")
			return
		}
	} else {
		if device.License == nil || device.License.AppID != app.ID {
			response.Forbidden(c, "设备未绑定当前应用授权")
			return
		}
	}

	now := time.Now()
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if authMode == clientauth.AuthModeSubscription {
			if device.SubscriptionID == nil || *device.SubscriptionID == "" {
				return errors.New("设备订阅信息异常")
			}
			if err := increaseSubscriptionUnbindUsed(tx, *device.SubscriptionID, tenantID, app.ID); err != nil {
				return err
			}
		} else {
			if device.LicenseID == nil || *device.LicenseID == "" {
				return errors.New("设备授权信息异常")
			}
			if err := increaseLicenseUnbindUsed(tx, *device.LicenseID, tenantID, app.ID); err != nil {
				return err
			}
		}

		if err := tx.Delete(&model.Device{}, "id = ?", device.ID).Error; err != nil {
			return err
		}
		return tx.Model(&model.ClientSession{}).
			Where("device_id = ? AND revoked_at IS NULL", device.ID).
			Updates(map[string]interface{}{
				"revoked_at":   now,
				"last_used_at": now,
			}).Error
	}); err != nil {
		if errors.Is(err, errClientUnbindLimitExceeded) {
			response.Error(c, 400, clientUnbindLimitExceededMessage)
			return
		}
		response.ServerError(c, "解绑失败")
		return
	}

	response.SuccessWithMessage(c, "解绑成功", gin.H{
		"mode":      authMode,
		"device_id": device.ID,
	})
}
