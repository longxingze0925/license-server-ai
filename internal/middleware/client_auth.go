package middleware

import (
	"errors"
	"strings"
	"time"

	"license-server/internal/model"
	"license-server/internal/pkg/clientauth"
	"license-server/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

const (
	clientCtxSessionID  = "client_session_id"
	clientCtxTenantID   = "client_tenant_id"
	clientCtxAppID      = "client_app_id"
	clientCtxCustomerID = "client_customer_id"
	clientCtxEmail      = "client_email"
	clientCtxDeviceID   = "client_device_id"
	clientCtxMachineID  = "client_machine_id"
	clientCtxAuthMode   = "client_auth_mode"
	clientCtxSession    = "client_session"
)

// ClientAuthMiddleware 客户端访问令牌认证中间件
func ClientAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		session, err := AuthenticateClientRequest(c)
		if err != nil {
			response.Unauthorized(c, err.Error())
			c.Abort()
			return
		}

		SetClientSessionContext(c, session)
		c.Next()
	}
}

// ClientUserContextMiddleware 将客户端 SDK 会话映射到通用 user/tenant 上下文。
//
// 客户端订阅登录后，业务用户是 customers.id；额度、AI 生成任务、文件归属都按这个 ID 记账。
// 管理后台 JWT 仍继续使用 team_members.id，不经过这个中间件。
func ClientUserContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := GetClientTenantID(c)
		customerID := GetClientCustomerID(c)
		if tenantID == "" || customerID == "" {
			response.Unauthorized(c, "客户端会话无效")
			c.Abort()
			return
		}

		var customer model.Customer
		if err := model.DB.First(&customer, "id = ? AND tenant_id = ?", customerID, tenantID).Error; err != nil {
			response.Forbidden(c, "客户账号不存在")
			c.Abort()
			return
		}
		if customer.Status != model.CustomerStatusActive {
			response.Forbidden(c, "客户账号已被禁用")
			c.Abort()
			return
		}

		c.Set("user_id", customer.ID)
		c.Set("tenant_id", tenantID)
		c.Set("email", customer.Email)
		c.Set("role", "client")
		c.Set("app_id", GetClientAppID(c))
		c.Set("customer", customer)
		c.Set(clientCtxEmail, customer.Email)

		c.Next()
	}
}

func AuthenticateClientRequest(c *gin.Context) (*model.ClientSession, error) {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader == "" {
		return nil, errors.New("缺少认证信息")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, errors.New("认证格式错误")
	}

	claims, err := clientauth.ParseAccessToken(parts[1])
	if err != nil {
		return nil, errors.New("无效的客户端令牌")
	}

	var session model.ClientSession
	if err := model.DB.First(&session, "id = ?", claims.SessionID).Error; err != nil {
		return nil, errors.New("会话不存在或已失效")
	}

	now := time.Now()
	if session.IsRevoked() || session.IsExpired(now) {
		return nil, errors.New("会话已过期，请重新登录")
	}

	if session.TenantID != claims.TenantID ||
		session.AppID != claims.AppID ||
		session.DeviceID != claims.DeviceID ||
		session.MachineID != claims.MachineID {
		return nil, errors.New("会话与设备不匹配")
	}
	if claims.CustomerID != "" && session.CustomerID != claims.CustomerID {
		return nil, errors.New("会话与用户不匹配")
	}
	if claims.AuthMode != "" && session.AuthMode != claims.AuthMode {
		return nil, errors.New("会话模式无效")
	}

	var device model.Device
	if err := model.DB.First(&device, "id = ? AND tenant_id = ?", session.DeviceID, session.TenantID).Error; err != nil {
		return nil, errors.New("设备已解绑，请重新登录")
	}
	if device.Status == model.DeviceStatusBlacklisted {
		return nil, errors.New("设备已被禁止使用")
	}
	var blacklist model.DeviceBlacklist
	if err := model.DB.Where(
		"tenant_id = ? AND machine_id = ? AND (app_id = ? OR app_id IS NULL OR app_id = '')",
		session.TenantID,
		session.MachineID,
		session.AppID,
	).First(&blacklist).Error; err == nil {
		return nil, errors.New("设备已被禁止使用")
	}

	return &session, nil
}

func SetClientSessionContext(c *gin.Context, session *model.ClientSession) {
	c.Set(clientCtxSessionID, session.ID)
	c.Set(clientCtxTenantID, session.TenantID)
	c.Set(clientCtxAppID, session.AppID)
	c.Set(clientCtxCustomerID, session.CustomerID)
	c.Set(clientCtxDeviceID, session.DeviceID)
	c.Set(clientCtxMachineID, session.MachineID)
	c.Set(clientCtxAuthMode, session.AuthMode)
	c.Set(clientCtxSession, session)
}

func GetClientSessionID(c *gin.Context) string {
	v, _ := c.Get(clientCtxSessionID)
	id, _ := v.(string)
	return id
}

func GetClientTenantID(c *gin.Context) string {
	v, _ := c.Get(clientCtxTenantID)
	id, _ := v.(string)
	return id
}

func GetClientAppID(c *gin.Context) string {
	v, _ := c.Get(clientCtxAppID)
	id, _ := v.(string)
	return id
}

func GetClientCustomerID(c *gin.Context) string {
	v, _ := c.Get(clientCtxCustomerID)
	id, _ := v.(string)
	return id
}

func GetClientEmail(c *gin.Context) string {
	v, _ := c.Get(clientCtxEmail)
	id, _ := v.(string)
	return id
}

func GetClientDeviceID(c *gin.Context) string {
	v, _ := c.Get(clientCtxDeviceID)
	id, _ := v.(string)
	return id
}

func GetClientMachineID(c *gin.Context) string {
	v, _ := c.Get(clientCtxMachineID)
	id, _ := v.(string)
	return id
}

func GetClientAuthMode(c *gin.Context) string {
	v, _ := c.Get(clientCtxAuthMode)
	id, _ := v.(string)
	return id
}

func GetClientSession(c *gin.Context) *model.ClientSession {
	v, ok := c.Get(clientCtxSession)
	if !ok {
		return nil
	}
	session, _ := v.(*model.ClientSession)
	return session
}

func GetAppID(c *gin.Context) string {
	v, _ := c.Get("app_id")
	id, _ := v.(string)
	return id
}
