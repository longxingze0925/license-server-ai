package handler

import (
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TeamMemberHandler struct{}

func NewTeamMemberHandler() *TeamMemberHandler {
	return &TeamMemberHandler{}
}

// List 获取团队成员列表
func (h *TeamMemberHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	role := c.Query("role")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := model.DB.Model(&model.TeamMember{}).Where("tenant_id = ?", tenantID)

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if role != "" {
		query = query.Where("role = ?", role)
	}

	var total int64
	query.Count(&total)

	var members []model.TeamMember
	query.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&members)

	result := make([]gin.H, 0, len(members))
	for _, m := range members {
		result = append(result, gin.H{
			"id":            m.ID,
			"email":         m.Email,
			"name":          m.Name,
			"phone":         m.Phone,
			"avatar":        m.Avatar,
			"role":          m.Role,
			"status":        m.Status,
			"last_login_at": m.LastLoginAt,
			"created_at":    m.CreatedAt,
		})
	}

	response.SuccessPage(c, result, total, page, pageSize)
}

// Get 获取成员详情
func (h *TeamMemberHandler) Get(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var member model.TeamMember
	if err := model.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&member).Error; err != nil {
		response.NotFound(c, "成员不存在")
		return
	}

	response.Success(c, gin.H{
		"id":                 member.ID,
		"email":              member.Email,
		"name":               member.Name,
		"phone":              member.Phone,
		"avatar":             member.Avatar,
		"role":               member.Role,
		"status":             member.Status,
		"email_verified":     member.EmailVerified,
		"two_factor_enabled": member.TwoFactorEnabled,
		"last_login_at":      member.LastLoginAt,
		"last_login_ip":      member.LastLoginIP,
		"created_at":         member.CreatedAt,
	})
}

// TeamInviteMemberRequest 邀请成员请求
type TeamInviteMemberRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required"`
	Name  string `json:"name"`
}

// CreateMemberRequest 创建成员请求
type CreateMemberRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Role     string `json:"role" binding:"required"`
	Phone    string `json:"phone"`
}

// Create 直接创建成员
func (h *TeamMemberHandler) Create(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userRole := middleware.GetUserRole(c)

	var req CreateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validatePasswordPolicy(req.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 验证角色
	validRoles := map[string]bool{
		string(model.RoleAdmin):     true,
		string(model.RoleDeveloper): true,
		string(model.RoleViewer):    true,
	}
	if !validRoles[req.Role] {
		response.BadRequest(c, "无效的角色")
		return
	}

	// 只有 Owner 可以创建 Admin
	if req.Role == string(model.RoleAdmin) && userRole != string(model.RoleOwner) {
		response.Forbidden(c, "只有所有者可以创建管理员")
		return
	}

	// 检查邮箱是否已存在
	var existingMember model.TeamMember
	if err := model.DB.Where("email = ?", req.Email).First(&existingMember).Error; err == nil {
		response.Error(c, 400, "该邮箱已被注册")
		return
	}

	// 创建成员
	member := model.TeamMember{
		TenantID: tenantID,
		Email:    req.Email,
		Name:     req.Name,
		Phone:    req.Phone,
		Role:     model.TeamMemberRole(req.Role),
		Status:   model.MemberStatusActive,
	}

	if err := member.SetPassword(req.Password); err != nil {
		response.ServerError(c, "密码加密失败")
		return
	}

	if err := model.DB.Create(&member).Error; err != nil {
		response.ServerError(c, "创建成员失败")
		return
	}

	response.Success(c, gin.H{
		"id":         member.ID,
		"email":      member.Email,
		"name":       member.Name,
		"role":       member.Role,
		"status":     member.Status,
		"created_at": member.CreatedAt,
	})
}

// UpdateMemberRequest 更新成员请求
type UpdateMemberRequest struct {
	Email *string `json:"email" binding:"omitempty,email"`
	Name  *string `json:"name"`
	Phone *string `json:"phone"`
}

// Update 更新成员信息
func (h *TeamMemberHandler) Update(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	userRole := middleware.GetUserRole(c)
	memberID := c.Param("id")

	var req UpdateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var member model.TeamMember
	if err := model.DB.Where("id = ? AND tenant_id = ?", memberID, tenantID).First(&member).Error; err != nil {
		response.NotFound(c, "成员不存在")
		return
	}

	// 只能修改自己的信息，或者 Owner/Admin 可以修改其他人
	if memberID != userID && userRole != string(model.RoleOwner) && userRole != string(model.RoleAdmin) {
		response.Forbidden(c, "没有权限修改该成员信息")
		return
	}

	// Admin 不能修改 Owner 的信息
	if member.Role == model.RoleOwner && userRole != string(model.RoleOwner) {
		response.Forbidden(c, "不能修改所有者的信息")
		return
	}

	// 如果修改邮箱，检查是否已存在
	if req.Email != nil && *req.Email == "" {
		response.BadRequest(c, "邮箱不能为空")
		return
	}
	if req.Email != nil && *req.Email != member.Email {
		var existingMember model.TeamMember
		if err := model.DB.Where("email = ? AND id != ?", *req.Email, memberID).First(&existingMember).Error; err == nil {
			response.Error(c, 400, "该邮箱已被使用")
			return
		}
		member.Email = *req.Email
	}

	if req.Name != nil {
		member.Name = *req.Name
	}
	if req.Phone != nil {
		member.Phone = *req.Phone
	}

	if err := model.DB.Save(&member).Error; err != nil {
		response.ServerError(c, "更新成员失败")
		return
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

// ResetPasswordRequest 重置密码请求
type ResetPasswordRequest struct {
	Password string `json:"password" binding:"required"`
}

// ResetPassword 重置成员密码
func (h *TeamMemberHandler) ResetPassword(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	userRole := middleware.GetUserRole(c)
	memberID := c.Param("id")

	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validatePasswordPolicy(req.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var member model.TeamMember
	if err := model.DB.Where("id = ? AND tenant_id = ?", memberID, tenantID).First(&member).Error; err != nil {
		response.NotFound(c, "成员不存在")
		return
	}

	// 只能修改自己的密码，或者 Owner/Admin 可以重置其他人的密码
	if memberID != userID && userRole != string(model.RoleOwner) && userRole != string(model.RoleAdmin) {
		response.Forbidden(c, "没有权限重置该成员密码")
		return
	}

	// Admin 不能重置 Owner 的密码
	if member.Role == model.RoleOwner && memberID != userID && userRole != string(model.RoleOwner) {
		response.Forbidden(c, "不能重置所有者的密码")
		return
	}

	if err := member.SetPassword(req.Password); err != nil {
		response.ServerError(c, "密码加密失败")
		return
	}

	if err := model.DB.Save(&member).Error; err != nil {
		response.ServerError(c, "重置密码失败")
		return
	}

	response.SuccessWithMessage(c, "密码已重置", nil)
}

// Invite 邀请成员
func (h *TeamMemberHandler) Invite(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	userRole := middleware.GetUserRole(c)

	var req TeamInviteMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证角色
	validRoles := map[string]bool{
		string(model.RoleAdmin):     true,
		string(model.RoleDeveloper): true,
		string(model.RoleViewer):    true,
	}
	if !validRoles[req.Role] {
		response.BadRequest(c, "无效的角色")
		return
	}

	// 只有 Owner 可以邀请 Admin
	if req.Role == string(model.RoleAdmin) && userRole != string(model.RoleOwner) {
		response.Forbidden(c, "只有所有者可以邀请管理员")
		return
	}

	// 检查邮箱是否已存在
	var existingMember model.TeamMember
	if err := model.DB.Where("email = ?", req.Email).First(&existingMember).Error; err == nil {
		response.Error(c, 400, "该邮箱已被注册")
		return
	}

	// 检查是否已有待处理的邀请
	var existingInvite model.TeamInvitation
	if err := model.DB.Where("email = ? AND tenant_id = ? AND status = ?", req.Email, tenantID, model.InviteStatusPending).First(&existingInvite).Error; err == nil {
		response.Error(c, 400, "该邮箱已有待处理的邀请")
		return
	}

	// 创建邀请
	invitation := model.TeamInvitation{
		TenantID:  tenantID,
		Email:     req.Email,
		Role:      model.TeamMemberRole(req.Role),
		Token:     uuid.New().String(),
		InvitedBy: userID,
		Status:    model.InviteStatusPending,
		ExpireAt:  time.Now().AddDate(0, 0, 7), // 7天有效期
	}

	if err := model.DB.Create(&invitation).Error; err != nil {
		response.ServerError(c, "创建邀请失败")
		return
	}

	// TODO: 发送邀请邮件

	response.Success(c, gin.H{
		"id":        invitation.ID,
		"email":     invitation.Email,
		"role":      invitation.Role,
		"token":     invitation.Token,
		"expire_at": invitation.ExpireAt,
	})
}

// UpdateRoleRequest 更新角色请求
type UpdateRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// UpdateRole 更新成员角色
func (h *TeamMemberHandler) UpdateRole(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	userRole := middleware.GetUserRole(c)
	memberID := c.Param("id")

	var req UpdateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 不能修改自己的角色
	if memberID == userID {
		response.Error(c, 400, "不能修改自己的角色")
		return
	}

	var member model.TeamMember
	if err := model.DB.Where("id = ? AND tenant_id = ?", memberID, tenantID).First(&member).Error; err != nil {
		response.NotFound(c, "成员不存在")
		return
	}

	// 不能修改 Owner 的角色
	if member.Role == model.RoleOwner {
		response.Error(c, 400, "不能修改所有者的角色")
		return
	}

	// 只有 Owner 可以设置 Admin 角色
	if req.Role == string(model.RoleAdmin) && userRole != string(model.RoleOwner) {
		response.Forbidden(c, "只有所有者可以设置管理员角色")
		return
	}

	// Admin 不能修改其他 Admin 的角色
	if member.Role == model.RoleAdmin && userRole != string(model.RoleOwner) {
		response.Forbidden(c, "只有所有者可以修改管理员的角色")
		return
	}

	// 验证角色
	validRoles := map[string]bool{
		string(model.RoleAdmin):     true,
		string(model.RoleDeveloper): true,
		string(model.RoleViewer):    true,
	}
	if !validRoles[req.Role] {
		response.BadRequest(c, "无效的角色")
		return
	}

	member.Role = model.TeamMemberRole(req.Role)
	if err := model.DB.Save(&member).Error; err != nil {
		response.ServerError(c, "更新角色失败")
		return
	}

	response.SuccessWithMessage(c, "角色已更新", nil)
}

// Remove 移除成员
func (h *TeamMemberHandler) Remove(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	userRole := middleware.GetUserRole(c)
	memberID := c.Param("id")

	// 不能移除自己
	if memberID == userID {
		response.Error(c, 400, "不能移除自己")
		return
	}

	var member model.TeamMember
	if err := model.DB.Where("id = ? AND tenant_id = ?", memberID, tenantID).First(&member).Error; err != nil {
		response.NotFound(c, "成员不存在")
		return
	}

	// 不能移除 Owner
	if member.Role == model.RoleOwner {
		response.Error(c, 400, "不能移除所有者")
		return
	}

	// Admin 不能移除其他 Admin
	if member.Role == model.RoleAdmin && userRole != string(model.RoleOwner) {
		response.Forbidden(c, "只有所有者可以移除管理员")
		return
	}

	if err := model.DB.Delete(&member).Error; err != nil {
		response.ServerError(c, "移除成员失败")
		return
	}

	response.SuccessWithMessage(c, "成员已移除", nil)
}

// ListInvitations 获取邀请列表
func (h *TeamMemberHandler) ListInvitations(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var invitations []model.TeamInvitation
	model.DB.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&invitations)

	var result []gin.H
	for _, inv := range invitations {
		result = append(result, gin.H{
			"id":         inv.ID,
			"email":      inv.Email,
			"role":       inv.Role,
			"status":     inv.Status,
			"expire_at":  inv.ExpireAt,
			"created_at": inv.CreatedAt,
		})
	}

	response.Success(c, result)
}

// RevokeInvitation 撤销邀请
func (h *TeamMemberHandler) RevokeInvitation(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	inviteID := c.Param("id")

	var invitation model.TeamInvitation
	if err := model.DB.Where("id = ? AND tenant_id = ?", inviteID, tenantID).First(&invitation).Error; err != nil {
		response.NotFound(c, "邀请不存在")
		return
	}

	if invitation.Status != model.InviteStatusPending {
		response.Error(c, 400, "只能撤销待处理的邀请")
		return
	}

	invitation.Status = model.InviteStatusRevoked
	if err := model.DB.Save(&invitation).Error; err != nil {
		response.ServerError(c, "撤销邀请失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "邀请已撤销", nil)
}

// ResendInvitation 重发邀请
func (h *TeamMemberHandler) ResendInvitation(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	inviteID := c.Param("id")

	var invitation model.TeamInvitation
	if err := model.DB.Where("id = ? AND tenant_id = ?", inviteID, tenantID).First(&invitation).Error; err != nil {
		response.NotFound(c, "邀请不存在")
		return
	}

	if invitation.Status != model.InviteStatusPending {
		response.Error(c, 400, "只能重发待处理的邀请")
		return
	}

	// 更新过期时间和 token
	invitation.Token = uuid.New().String()
	invitation.ExpireAt = time.Now().AddDate(0, 0, 7)
	if err := model.DB.Save(&invitation).Error; err != nil {
		response.ServerError(c, "重发邀请失败: "+err.Error())
		return
	}

	// TODO: 重新发送邀请邮件

	response.SuccessWithMessage(c, "邀请已重发", nil)
}

// AcceptInviteRequest 接受邀请请求
type AcceptInviteRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required"`
	Name     string `json:"name" binding:"required"`
}

// AcceptInvite 接受邀请（公开接口）
func (h *TeamMemberHandler) AcceptInvite(c *gin.Context) {
	var req AcceptInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validatePasswordPolicy(req.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var invitation model.TeamInvitation
	if err := model.DB.Preload("Tenant").Where("token = ?", req.Token).First(&invitation).Error; err != nil {
		response.NotFound(c, "邀请不存在或已失效")
		return
	}

	if invitation.Status != model.InviteStatusPending {
		response.Error(c, 400, "邀请已被处理")
		return
	}

	if invitation.IsExpired() {
		invitation.Status = model.InviteStatusExpired
		if err := model.DB.Save(&invitation).Error; err != nil {
			response.ServerError(c, "更新邀请状态失败: "+err.Error())
			return
		}
		response.Error(c, 400, "邀请已过期")
		return
	}

	// 检查邮箱是否已被注册
	var existingMember model.TeamMember
	if err := model.DB.Where("email = ?", invitation.Email).First(&existingMember).Error; err == nil {
		response.Error(c, 400, "该邮箱已被注册")
		return
	}

	// 创建团队成员
	member := model.TeamMember{
		TenantID:  invitation.TenantID,
		Email:     invitation.Email,
		Name:      req.Name,
		Role:      invitation.Role,
		Status:    model.MemberStatusActive,
		InvitedBy: &invitation.InvitedBy,
	}
	now := time.Now()
	member.InvitedAt = &now

	if err := member.SetPassword(req.Password); err != nil {
		response.ServerError(c, "密码加密失败")
		return
	}

	tx := model.DB.Begin()

	if err := tx.Create(&member).Error; err != nil {
		tx.Rollback()
		response.ServerError(c, "创建成员失败")
		return
	}

	// 更新邀请状态
	invitation.Status = model.InviteStatusAccepted
	if err := tx.Save(&invitation).Error; err != nil {
		tx.Rollback()
		response.ServerError(c, "更新邀请状态失败")
		return
	}

	tx.Commit()

	response.Success(c, gin.H{
		"message": "加入成功",
		"tenant": gin.H{
			"id":   invitation.Tenant.ID,
			"name": invitation.Tenant.Name,
		},
	})
}
