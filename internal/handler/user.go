package handler

import (
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type UserHandler struct{}

func NewUserHandler() *UserHandler {
	return &UserHandler{}
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Role     string `json:"role"`
}

// Create 创建管理员用户
func (h *UserHandler) Create(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validatePasswordPolicy(req.Password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 检查邮箱是否已存在
	var existingUser model.User
	if err := model.DB.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
		response.Error(c, 400, "该邮箱已被注册")
		return
	}

	// 设置默认角色
	role := model.UserRoleUser
	if req.Role == "admin" {
		role = model.UserRoleAdmin
	}

	user := model.User{
		Email:  req.Email,
		Name:   req.Name,
		Role:   role,
		Status: model.UserStatusActive,
	}

	if err := user.SetPassword(req.Password); err != nil {
		response.ServerError(c, "密码加密失败")
		return
	}

	if err := model.DB.Create(&user).Error; err != nil {
		response.ServerError(c, "创建用户失败")
		return
	}

	response.Success(c, gin.H{
		"id":         user.ID,
		"email":      user.Email,
		"name":       user.Name,
		"role":       user.Role,
		"status":     user.Status,
		"created_at": user.CreatedAt,
	})
}

// List 获取用户列表
func (h *UserHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	keyword := c.Query("keyword")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := model.DB.Model(&model.User{})

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("name LIKE ? OR email LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	query.Count(&total)

	var users []model.User
	query.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&users)

	var result []gin.H
	for _, user := range users {
		result = append(result, gin.H{
			"id":         user.ID,
			"email":      user.Email,
			"name":       user.Name,
			"avatar":     user.Avatar,
			"status":     user.Status,
			"role":       user.Role,
			"created_at": user.CreatedAt,
		})
	}

	response.SuccessPage(c, result, total, page, pageSize)
}

// Get 获取用户详情
func (h *UserHandler) Get(c *gin.Context) {
	id := c.Param("id")

	var user model.User
	if err := model.DB.First(&user, "id = ?", id).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	// 获取用户的组织
	var orgUsers []model.OrganizationUser
	model.DB.Preload("Organization").Where("user_id = ?", id).Find(&orgUsers)

	var orgs []gin.H
	for _, ou := range orgUsers {
		if ou.Organization != nil {
			orgs = append(orgs, gin.H{
				"id":   ou.Organization.ID,
				"name": ou.Organization.Name,
				"role": ou.Role,
			})
		}
	}

	response.Success(c, gin.H{
		"id":            user.ID,
		"email":         user.Email,
		"name":          user.Name,
		"avatar":        user.Avatar,
		"status":        user.Status,
		"role":          user.Role,
		"organizations": orgs,
		"created_at":    user.CreatedAt,
		"updated_at":    user.UpdatedAt,
	})
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Role   string `json:"role"`
}

// Update 更新用户
func (h *UserHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var user model.User
	if err := model.DB.First(&user, "id = ?", id).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.Role != "" {
		updates["role"] = req.Role
	}

	if len(updates) > 0 {
		if err := model.DB.Model(&user).Updates(updates).Error; err != nil {
			response.ServerError(c, "更新失败")
			return
		}
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

// UserResetPasswordRequest 重置用户密码请求
type UserResetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
}

// ResetPassword 重置用户密码
func (h *UserHandler) ResetPassword(c *gin.Context) {
	id := c.Param("id")

	var req UserResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validatePasswordPolicy(req.NewPassword); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var user model.User
	if err := model.DB.First(&user, "id = ?", id).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		response.ServerError(c, "密码加密失败")
		return
	}

	if err := model.DB.Model(&user).Update("password", string(hashedPassword)).Error; err != nil {
		response.ServerError(c, "重置密码失败")
		return
	}

	response.SuccessWithMessage(c, "密码已重置", nil)
}

// Delete 删除用户
func (h *UserHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	var user model.User
	if err := model.DB.First(&user, "id = ?", id).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	// 检查是否有关联数据
	var licenseCount int64
	if err := model.DB.Model(&model.License{}).Where("user_id = ?", id).Count(&licenseCount).Error; err != nil {
		response.ServerError(c, "检查用户关联授权失败: "+err.Error())
		return
	}
	if licenseCount > 0 {
		response.Error(c, 400, "用户有关联的授权，无法删除")
		return
	}

	// 删除用户的组织关联
	if err := model.DB.Where("user_id = ?", id).Delete(&model.OrganizationUser{}).Error; err != nil {
		response.ServerError(c, "删除用户组织关联失败: "+err.Error())
		return
	}

	// 删除用户
	if err := model.DB.Delete(&user).Error; err != nil {
		response.ServerError(c, "删除失败")
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
}

// Enable 启用用户
func (h *UserHandler) Enable(c *gin.Context) {
	id := c.Param("id")

	var user model.User
	if err := model.DB.First(&user, "id = ?", id).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	if err := model.DB.Model(&user).Update("status", "active").Error; err != nil {
		response.ServerError(c, "启用失败")
		return
	}

	response.SuccessWithMessage(c, "用户已启用", nil)
}

// Disable 禁用用户
func (h *UserHandler) Disable(c *gin.Context) {
	id := c.Param("id")

	var user model.User
	if err := model.DB.First(&user, "id = ?", id).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	if err := model.DB.Model(&user).Update("status", "disabled").Error; err != nil {
		response.ServerError(c, "禁用失败")
		return
	}

	response.SuccessWithMessage(c, "用户已禁用", nil)
}
