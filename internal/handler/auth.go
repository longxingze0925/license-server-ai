package handler

import (
	"fmt"
	"license-server/internal/config"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"license-server/internal/pkg/utils"
	"license-server/internal/service"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AuthHandler struct{}

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{}
}

// RegisterRequest 注册请求（创建新租户）
type RegisterRequest struct {
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required"`
	Name       string `json:"name" binding:"required"`
	TenantName string `json:"tenant_name"` // 可选，默认使用用户名
}

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// Register 注册新租户（创建租户和Owner）
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validatePasswordPolicy(req.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 检查邮箱是否已存在
	var existingMember model.TeamMember
	if err := model.DB.Where("email = ?", req.Email).First(&existingMember).Error; err == nil {
		response.Error(c, 400, "邮箱已被注册")
		return
	}

	// 生成租户 slug
	tenantName := req.TenantName
	if tenantName == "" {
		tenantName = req.Name + "的团队"
	}
	slug := generateSlug(tenantName)

	// 检查 slug 是否已存在
	var existingTenant model.Tenant
	if err := model.DB.Where("slug = ?", slug).First(&existingTenant).Error; err == nil {
		// 如果存在，添加随机后缀
		slug = slug + "-" + uuid.New().String()[:8]
	}

	tenant := model.Tenant{
		Name:   tenantName,
		Slug:   slug,
		Status: model.TenantStatusActive,
		Plan:   model.TenantPlanFree,
	}
	member := model.TeamMember{
		Email:  req.Email,
		Name:   req.Name,
		Role:   model.RoleOwner,
		Status: model.MemberStatusActive,
	}
	if err := member.SetPassword(req.Password); err != nil {
		response.ServerError(c, "密码加密失败")
		return
	}

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&tenant).Error; err != nil {
			return err
		}
		member.TenantID = tenant.ID
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		return service.NewPricingService().EnsureDefaultRulesForTenantTx(tx, tenant.ID)
	}); err != nil {
		response.ServerError(c, "注册失败: "+err.Error())
		return
	}

	// 生成 Token（包含租户ID）
	token, err := crypto.GenerateTokenWithTenant(member.ID, tenant.ID, member.Email, string(member.Role), config.Get().JWT.Secret, config.Get().JWT.ExpireHours)
	if err != nil {
		response.ServerError(c, "生成Token失败")
		return
	}

	response.Success(c, gin.H{
		"token": token,
		"user": gin.H{
			"id":    member.ID,
			"email": member.Email,
			"name":  member.Name,
			"role":  member.Role,
		},
		"tenant": gin.H{
			"id":   tenant.ID,
			"name": tenant.Name,
			"slug": tenant.Slug,
			"plan": tenant.Plan,
		},
	})
}

// Login 团队成员登录
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
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

	// 查找团队成员
	var member model.TeamMember
	if err := model.DB.Preload("Tenant").Where("email = ?", req.Email).First(&member).Error; err != nil {
		// 记录失败
		loginLimiter.RecordFailure(req.Email)
		ipLimiter.RecordFailure(clientIP)
		remainingAttempts := loginLimiter.GetRemainingAttempts(req.Email)
		if remainingAttempts > 0 {
			response.Error(c, 401, fmt.Sprintf("邮箱或密码错误，还剩 %d 次尝试机会", remainingAttempts))
		} else {
			response.Error(c, 401, "邮箱或密码错误")
		}
		return
	}

	// 验证密码
	if !member.CheckPassword(req.Password) {
		// 记录失败
		locked, lockDuration := loginLimiter.RecordFailure(req.Email)
		ipLimiter.RecordFailure(clientIP)
		if locked {
			response.Error(c, 429, fmt.Sprintf("登录失败次数过多，账号已被锁定 %d 分钟", int(lockDuration.Minutes())))
		} else {
			remainingAttempts := loginLimiter.GetRemainingAttempts(req.Email)
			response.Error(c, 401, fmt.Sprintf("邮箱或密码错误，还剩 %d 次尝试机会", remainingAttempts))
		}
		return
	}

	// 检查成员状态
	if member.Status != model.MemberStatusActive {
		response.Error(c, 403, "账号已被禁用")
		return
	}

	// 检查租户状态
	if member.Tenant != nil && member.Tenant.Status != model.TenantStatusActive {
		response.Error(c, 403, "租户已被暂停")
		return
	}

	// 登录成功，清除失败记录
	loginLimiter.RecordSuccess(req.Email)
	ipLimiter.RecordSuccess(clientIP)

	// 更新最后登录时间和IP
	now := time.Now()
	model.DB.Model(&member).Updates(map[string]interface{}{
		"last_login_at": now,
		"last_login_ip": clientIP,
	})

	// 生成 Token（包含租户ID）
	token, err := crypto.GenerateTokenWithTenant(member.ID, member.TenantID, member.Email, string(member.Role), config.Get().JWT.Secret, config.Get().JWT.ExpireHours)
	if err != nil {
		response.ServerError(c, "生成Token失败")
		return
	}

	response.Success(c, gin.H{
		"token": token,
		"user": gin.H{
			"id":    member.ID,
			"email": member.Email,
			"name":  member.Name,
			"role":  member.Role,
		},
		"tenant": gin.H{
			"id":   member.Tenant.ID,
			"name": member.Tenant.Name,
			"slug": member.Tenant.Slug,
			"plan": member.Tenant.Plan,
		},
	})
}

// GetProfile 获取当前用户信息
func (h *AuthHandler) GetProfile(c *gin.Context) {
	userID, _ := c.Get("user_id")
	tenantID, _ := c.Get("tenant_id")

	var member model.TeamMember
	if err := model.DB.Preload("Tenant").First(&member, "id = ?", userID).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	// 获取租户信息
	var tenant model.Tenant
	model.DB.First(&tenant, "id = ?", tenantID)

	response.Success(c, gin.H{
		"user": gin.H{
			"id":                 member.ID,
			"email":              member.Email,
			"name":               member.Name,
			"phone":              member.Phone,
			"avatar":             member.Avatar,
			"role":               member.Role,
			"email_verified":     member.EmailVerified,
			"two_factor_enabled": member.TwoFactorEnabled,
			"created_at":         member.CreatedAt,
			"last_login_at":      member.LastLoginAt,
		},
		"tenant": gin.H{
			"id":               tenant.ID,
			"name":             tenant.Name,
			"slug":             tenant.Slug,
			"plan":             tenant.Plan,
			"max_applications": tenant.MaxApplications,
			"max_team_members": tenant.MaxTeamMembers,
			"max_customers":    tenant.MaxCustomers,
		},
	})
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ChangePassword 修改密码
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validatePasswordPolicy(req.NewPassword); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var member model.TeamMember
	if err := model.DB.First(&member, "id = ?", userID).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	// 验证旧密码
	if !member.CheckPassword(req.OldPassword) {
		response.Error(c, 400, "原密码错误")
		return
	}

	// 设置新密码
	if err := member.SetPassword(req.NewPassword); err != nil {
		response.ServerError(c, "密码加密失败")
		return
	}

	if err := model.DB.Save(&member).Error; err != nil {
		response.ServerError(c, "修改密码失败")
		return
	}

	response.SuccessWithMessage(c, "密码修改成功", nil)
}

// generateSlug 生成 URL 友好的 slug
func generateSlug(name string) string {
	// 简单处理：转小写，替换空格为连字符
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "的", "-")
	// 移除非字母数字和连字符的字符
	var result strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r > 127 {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// 占位：用于 utils 包的引用
var _ = utils.GenerateUUID
