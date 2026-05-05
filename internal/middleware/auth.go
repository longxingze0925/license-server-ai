package middleware

import (
	"strings"

	"license-server/internal/config"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware JWT 认证中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Unauthorized(c, "缺少认证信息")
			c.Abort()
			return
		}

		// Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.Unauthorized(c, "认证格式错误")
			c.Abort()
			return
		}

		token := parts[1]
		claims, err := crypto.ParseToken(token, config.Get().JWT.Secret)
		if err != nil {
			response.Unauthorized(c, "无效的认证信息")
			c.Abort()
			return
		}

		var member model.TeamMember
		if err := model.DB.Where("id = ? AND tenant_id = ?", claims.UserID, claims.TenantID).First(&member).Error; err != nil {
			response.Forbidden(c, "用户不存在")
			c.Abort()
			return
		}
		if member.Status != model.MemberStatusActive {
			response.Forbidden(c, "账号已被禁用")
			c.Abort()
			return
		}

		// 将用户信息存入上下文
		c.Set("user_id", member.ID)
		c.Set("tenant_id", member.TenantID)
		c.Set("email", member.Email)
		c.Set("role", string(member.Role))
		c.Set("team_member", member)

		c.Next()
	}
}

// TenantMiddleware 租户隔离中间件 - 确保所有操作都在租户范围内
func TenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, exists := c.Get("tenant_id")
		if !exists || tenantID == "" {
			response.Forbidden(c, "缺少租户信息")
			c.Abort()
			return
		}

		// 验证租户状态
		var tenant model.Tenant
		if err := model.DB.First(&tenant, "id = ?", tenantID).Error; err != nil {
			response.Forbidden(c, "租户不存在")
			c.Abort()
			return
		}

		if tenant.Status != model.TenantStatusActive {
			response.Forbidden(c, "租户已被暂停")
			c.Abort()
			return
		}

		c.Next()
	}
}

// PermissionMiddleware 权限检查中间件
func PermissionMiddleware(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		tenantID, _ := c.Get("tenant_id")

		// 获取用户信息
		var member model.TeamMember
		if cached, ok := c.Get("team_member"); ok {
			if m, ok := cached.(model.TeamMember); ok {
				member = m
			}
		}
		if member.ID == "" {
			if err := model.DB.Where("id = ? AND tenant_id = ?", userID, tenantID).First(&member).Error; err != nil {
				response.Forbidden(c, "用户不存在")
				c.Abort()
				return
			}
		}
		if member.Status != model.MemberStatusActive {
			response.Forbidden(c, "账号已被禁用")
			c.Abort()
			return
		}

		// 检查权限
		if !member.HasPermission(permission) {
			response.Forbidden(c, "没有操作权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

// AdminMiddleware 管理员权限中间件（Owner 或 Admin）
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			response.Forbidden(c, "需要管理员权限")
			c.Abort()
			return
		}

		roleStr := role.(string)
		if roleStr != string(model.RoleOwner) && roleStr != string(model.RoleAdmin) {
			response.Forbidden(c, "需要管理员权限")
			c.Abort()
			return
		}
		c.Next()
	}
}

// OwnerMiddleware 所有者权限中间件
func OwnerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists || role != string(model.RoleOwner) {
			response.Forbidden(c, "需要所有者权限")
			c.Abort()
			return
		}
		c.Next()
	}
}

// GetUserID 从上下文获取用户 ID
func GetUserID(c *gin.Context) string {
	userID, _ := c.Get("user_id")
	if id, ok := userID.(string); ok {
		return id
	}
	return ""
}

// GetTenantID 从上下文获取租户 ID
func GetTenantID(c *gin.Context) string {
	tenantID, _ := c.Get("tenant_id")
	if id, ok := tenantID.(string); ok {
		return id
	}
	return ""
}

// GetUserEmail 从上下文获取用户邮箱
func GetUserEmail(c *gin.Context) string {
	email, _ := c.Get("email")
	if e, ok := email.(string); ok {
		return e
	}
	return ""
}

// GetUserRole 从上下文获取用户角色
func GetUserRole(c *gin.Context) string {
	role, _ := c.Get("role")
	if r, ok := role.(string); ok {
		return r
	}
	return ""
}
