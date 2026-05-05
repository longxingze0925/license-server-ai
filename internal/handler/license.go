package handler

import (
	"encoding/json"
	"errors"
	"io"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"license-server/internal/pkg/utils"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LicenseHandler struct{}

func NewLicenseHandler() *LicenseHandler {
	return &LicenseHandler{}
}

// CreateLicenseRequest 创建授权请求
type CreateLicenseRequest struct {
	AppID        string   `json:"app_id" binding:"required"`
	CustomerID   string   `json:"customer_id" binding:"required"` // 授权码必须关联客户
	Type         string   `json:"type"`
	DurationDays int      `json:"duration_days" binding:"required"`
	MaxDevices   int      `json:"max_devices"`
	UnbindLimit  *int     `json:"unbind_limit"`
	Features     []string `json:"features"`
	Notes        string   `json:"notes"`
	Count        int      `json:"count"` // 批量生成数量
}

// Create 创建授权码
func (h *LicenseHandler) Create(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	userRole := middleware.GetUserRole(c)

	var req CreateLicenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证应用是否存在（必须属于当前租户）
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", req.AppID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 验证客户是否存在
	var customerID *string
	var customer model.Customer
	customerQuery := model.DB.Where("id = ? AND tenant_id = ?", req.CustomerID, tenantID)
	if !isTenantAdminRole(userRole) {
		customerQuery = customerQuery.Where("owner_id = ?", userID)
	}
	if err := customerQuery.First(&customer).Error; err != nil {
		response.NotFound(c, "客户不存在")
		return
	}
	customerID = &req.CustomerID

	// 设置默认值
	if req.Type == "" {
		req.Type = string(model.LicenseTypeSubscription)
	}
	if !isValidLicenseType(req.Type) {
		response.BadRequest(c, "授权类型不支持")
		return
	}
	if req.DurationDays < -1 {
		response.BadRequest(c, "有效天数不能小于 -1")
		return
	}
	if req.MaxDevices < 0 {
		response.BadRequest(c, "最大设备数不能小于0")
		return
	}
	if req.MaxDevices == 0 {
		req.MaxDevices = app.MaxDevicesDefault
	}
	unbindLimit := 5
	if req.UnbindLimit != nil {
		unbindLimit = *req.UnbindLimit
	}
	if unbindLimit < 0 {
		response.BadRequest(c, "解绑次数上限不能小于0")
		return
	}
	if req.Count == 0 {
		req.Count = 1
	}
	if req.Count < 0 {
		response.BadRequest(c, "生成数量不能小于0")
		return
	}
	if req.Count > 100 {
		req.Count = 100 // 最多一次生成100个
	}

	// 序列化 features
	featuresJSON := "[]" // 默认空数组
	if len(req.Features) > 0 {
		bytes, _ := json.Marshal(req.Features)
		featuresJSON = string(bytes)
	}

	// 批量创建授权码
	var licenses []model.License
	for i := 0; i < req.Count; i++ {
		license := model.License{
			TenantID:     tenantID,
			LicenseKey:   utils.GenerateLicenseKey(),
			AppID:        req.AppID,
			CustomerID:   customerID,
			Type:         model.LicenseType(req.Type),
			DurationDays: req.DurationDays,
			MaxDevices:   req.MaxDevices,
			UnbindLimit:  unbindLimit,
			Features:     featuresJSON,
			Metadata:     "{}",
			Notes:        req.Notes,
			Status:       model.LicenseStatusPending,
		}
		licenses = append(licenses, license)
	}

	if err := model.DB.Select(
		"id",
		"tenant_id",
		"license_key",
		"app_id",
		"customer_id",
		"type",
		"duration_days",
		"max_devices",
		"unbind_limit",
		"features",
		"metadata",
		"notes",
		"status",
	).Create(&licenses).Error; err != nil {
		response.ServerError(c, "创建授权码失败: "+err.Error())
		return
	}
	if req.UnbindLimit != nil {
		licenseIDs := make([]string, 0, len(licenses))
		for i := range licenses {
			licenseIDs = append(licenseIDs, licenses[i].ID)
			licenses[i].UnbindLimit = unbindLimit
		}
		if err := model.DB.Model(&model.License{}).Where("id IN ?", licenseIDs).Update("unbind_limit", unbindLimit).Error; err != nil {
			response.ServerError(c, "设置解绑次数上限失败: "+err.Error())
			return
		}
	}

	// 记录事件
	for _, license := range licenses {
		event := model.LicenseEvent{
			LicenseID:    license.ID,
			EventType:    model.LicenseEventCreated,
			OperatorType: "admin",
			IPAddress:    c.ClientIP(),
		}
		if err := model.DB.Create(&event).Error; err != nil {
			response.ServerError(c, "记录授权事件失败: "+err.Error())
			return
		}
	}

	// 返回结果
	var result []gin.H
	for _, license := range licenses {
		result = append(result, gin.H{
			"id":               license.ID,
			"license_key":      license.LicenseKey,
			"type":             license.Type,
			"duration_days":    license.DurationDays,
			"max_devices":      license.MaxDevices,
			"unbind_limit":     license.UnbindLimit,
			"unbind_used":      license.UnbindUsed,
			"unbind_remaining": license.RemainingClientUnbindCount(),
			"status":           license.Status,
			"created_at":       license.CreatedAt,
		})
	}

	response.Success(c, result)
}

// List 获取授权列表
func (h *LicenseHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	appID := c.Query("app_id")
	status := c.Query("status")
	keyword := strings.TrimSpace(c.Query("keyword"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := scopedLicenseQuery(c).
		Preload("Application").
		Preload("Customer")

	if appID != "" {
		query = query.Where("licenses.app_id = ?", appID)
	}
	if status != "" {
		query = query.Where("licenses.status = ?", status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"licenses.license_key LIKE ? OR customers.email LIKE ? OR customers.name LIKE ?",
			like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.ServerError(c, "获取授权数量失败")
		return
	}

	var licenses []model.License
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Order("licenses.created_at DESC").Find(&licenses).Error; err != nil {
		response.ServerError(c, "获取授权列表失败")
		return
	}

	var result []gin.H
	for _, license := range licenses {
		item := gin.H{
			"id":               license.ID,
			"license_key":      license.LicenseKey,
			"app_id":           license.AppID,
			"license_type":     license.Type,
			"duration_days":    license.DurationDays,
			"max_devices":      license.MaxDevices,
			"unbind_limit":     license.UnbindLimit,
			"unbind_used":      license.UnbindUsed,
			"unbind_remaining": license.RemainingClientUnbindCount(),
			"status":           license.Status,
			"activated_at":     license.ActivatedAt,
			"expires_at":       license.ExpireAt,
			"remaining_days":   license.RemainingDays(),
			"created_at":       license.CreatedAt,
		}
		if license.Application != nil {
			item["app_name"] = license.Application.Name
		}
		if license.Customer != nil {
			item["customer_name"] = license.Customer.Name
			item["customer_email"] = license.Customer.Email
		}
		result = append(result, item)
	}

	response.SuccessPage(c, result, total, page, pageSize)
}

// Get 获取授权详情
func (h *LicenseHandler) Get(c *gin.Context) {
	id := c.Param("id")

	var license model.License
	if err := scopedLicenseQuery(c).Preload("Application").Preload("Customer").Preload("Devices").
		Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	result := gin.H{
		"id":               license.ID,
		"license_key":      license.LicenseKey,
		"app_id":           license.AppID,
		"customer_id":      license.CustomerID,
		"license_type":     license.Type,
		"duration_days":    license.DurationDays,
		"max_devices":      license.MaxDevices,
		"unbind_limit":     license.UnbindLimit,
		"unbind_used":      license.UnbindUsed,
		"unbind_remaining": license.RemainingClientUnbindCount(),
		"features":         license.Features,
		"status":           license.Status,
		"activated_at":     license.ActivatedAt,
		"expires_at":       license.ExpireAt,
		"grace_expire_at":  license.GraceExpireAt,
		"remaining_days":   license.RemainingDays(),
		"notes":            license.Notes,
		"used_devices":     len(license.Devices),
		"devices":          license.Devices,
		"created_at":       license.CreatedAt,
	}

	if license.Application != nil {
		result["app_name"] = license.Application.Name
	}
	if license.Customer != nil {
		result["customer_name"] = license.Customer.Name
		result["customer_email"] = license.Customer.Email
	}

	response.Success(c, result)
}

// RenewRequest 续费请求
type RenewRequest struct {
	Days int `json:"days" binding:"required,min=1"`
}

// Renew 续费
func (h *LicenseHandler) Renew(c *gin.Context) {
	id := c.Param("id")

	var req RenewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	if license.Status == model.LicenseStatusRevoked {
		response.Error(c, 400, "授权已被吊销，无法续费")
		return
	}

	if license.DurationDays == -1 {
		response.Error(c, 400, "永久授权无需续费")
		return
	}

	// 记录旧值
	oldExpireAt := license.ExpireAt

	// 计算新的到期时间
	var newExpireAt time.Time
	if license.Status == model.LicenseStatusExpired || (license.ExpireAt != nil && time.Now().After(*license.ExpireAt)) {
		// 已过期，从当前时间开始计算
		newExpireAt = time.Now().AddDate(0, 0, req.Days)
		license.Status = model.LicenseStatusActive
	} else if license.ExpireAt != nil {
		// 未过期，在原到期时间基础上增加
		newExpireAt = license.ExpireAt.AddDate(0, 0, req.Days)
	} else {
		newExpireAt = time.Now().AddDate(0, 0, req.Days)
	}

	license.ExpireAt = &newExpireAt
	license.DurationDays += req.Days

	if err := model.DB.Save(&license).Error; err != nil {
		response.ServerError(c, "续费失败")
		return
	}

	// 记录事件
	fromValue, _ := json.Marshal(gin.H{"expire_at": oldExpireAt})
	toValue, _ := json.Marshal(gin.H{"expire_at": newExpireAt})
	event := model.LicenseEvent{
		LicenseID:    license.ID,
		EventType:    model.LicenseEventRenewed,
		FromValue:    string(fromValue),
		ToValue:      string(toValue),
		OperatorType: "admin",
		IPAddress:    c.ClientIP(),
		Notes:        "续费 " + strconv.Itoa(req.Days) + " 天",
	}
	if err := model.DB.Create(&event).Error; err != nil {
		response.ServerError(c, "记录授权事件失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"id":             license.ID,
		"expire_at":      license.ExpireAt,
		"remaining_days": license.RemainingDays(),
	})
}

// Revoke 吊销授权
func (h *LicenseHandler) Revoke(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	if license.Status == model.LicenseStatusRevoked {
		response.Error(c, 400, "授权已被吊销")
		return
	}

	oldStatus := license.Status
	license.Status = model.LicenseStatusRevoked
	license.RevokedReason = req.Reason

	if err := model.DB.Save(&license).Error; err != nil {
		response.ServerError(c, "吊销失败")
		return
	}

	// 记录事件
	fromValue, _ := json.Marshal(gin.H{"status": oldStatus})
	toValue, _ := json.Marshal(gin.H{"status": model.LicenseStatusRevoked})
	event := model.LicenseEvent{
		LicenseID:    license.ID,
		EventType:    model.LicenseEventRevoked,
		FromValue:    string(fromValue),
		ToValue:      string(toValue),
		OperatorType: "admin",
		IPAddress:    c.ClientIP(),
		Notes:        req.Reason,
	}
	if err := model.DB.Create(&event).Error; err != nil {
		response.ServerError(c, "记录授权事件失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "吊销成功", nil)
}

// Suspend 暂停授权
func (h *LicenseHandler) Suspend(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	if license.Status != model.LicenseStatusActive {
		response.Error(c, 400, "只能暂停激活状态的授权")
		return
	}

	license.Status = model.LicenseStatusSuspended
	license.SuspendedReason = req.Reason

	if err := model.DB.Save(&license).Error; err != nil {
		response.ServerError(c, "暂停失败")
		return
	}

	// 记录事件
	event := model.LicenseEvent{
		LicenseID:    license.ID,
		EventType:    model.LicenseEventSuspended,
		OperatorType: "admin",
		IPAddress:    c.ClientIP(),
		Notes:        req.Reason,
	}
	if err := model.DB.Create(&event).Error; err != nil {
		response.ServerError(c, "记录授权事件失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "暂停成功", nil)
}

// Resume 恢复授权
func (h *LicenseHandler) Resume(c *gin.Context) {
	id := c.Param("id")

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	if license.Status != model.LicenseStatusSuspended {
		response.Error(c, 400, "只能恢复暂停状态的授权")
		return
	}

	license.Status = model.LicenseStatusActive
	license.SuspendedReason = ""

	if err := model.DB.Save(&license).Error; err != nil {
		response.ServerError(c, "恢复失败")
		return
	}

	// 记录事件
	event := model.LicenseEvent{
		LicenseID:    license.ID,
		EventType:    model.LicenseEventResumed,
		OperatorType: "admin",
		IPAddress:    c.ClientIP(),
	}
	if err := model.DB.Create(&event).Error; err != nil {
		response.ServerError(c, "记录授权事件失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "恢复成功", nil)
}

// UpdateLicenseRequest 更新授权请求
type UpdateLicenseRequest struct {
	MaxDevices  *int     `json:"max_devices"`
	UnbindLimit *int     `json:"unbind_limit"`
	Notes       *string  `json:"notes"`
	Features    []string `json:"features"`
}

// Update 更新授权
func (h *LicenseHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateLicenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	// 更新字段
	updates := make(map[string]interface{})
	if req.MaxDevices != nil {
		if *req.MaxDevices <= 0 {
			response.BadRequest(c, "最大设备数必须大于0")
			return
		}
		updates["max_devices"] = *req.MaxDevices
	}
	if req.UnbindLimit != nil {
		if *req.UnbindLimit < 0 {
			response.BadRequest(c, "解绑次数上限不能小于0")
			return
		}
		updates["unbind_limit"] = *req.UnbindLimit
	}
	if req.Notes != nil {
		updates["notes"] = *req.Notes
	}
	if req.Features != nil {
		featuresJSON, err := json.Marshal(req.Features)
		if err != nil {
			response.BadRequest(c, "功能权限格式错误")
			return
		}
		updates["features"] = string(featuresJSON)
	}

	if len(updates) > 0 {
		if err := model.DB.Model(&license).Updates(updates).Error; err != nil {
			response.ServerError(c, "更新失败")
			return
		}
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

func isValidLicenseType(licenseType string) bool {
	switch model.LicenseType(licenseType) {
	case model.LicenseTypeTrial, model.LicenseTypeSubscription, model.LicenseTypePerpetual, model.LicenseTypeNodeLocked:
		return true
	default:
		return false
	}
}

// ResetUnbindCount 重置客户端解绑计数
func (h *LicenseHandler) ResetUnbindCount(c *gin.Context) {
	id := c.Param("id")

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	if err := model.DB.Model(&license).Update("unbind_used", 0).Error; err != nil {
		response.ServerError(c, "重置解绑次数失败")
		return
	}

	response.SuccessWithMessage(c, "解绑次数已重置", gin.H{
		"unbind_limit":     license.UnbindLimit,
		"unbind_used":      0,
		"unbind_remaining": license.UnbindLimit,
	})
}

// Delete 删除授权
func (h *LicenseHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	// 只能删除未激活的授权
	if license.Status != model.LicenseStatusPending {
		response.Error(c, 400, "只能删除未激活的授权")
		return
	}

	// 删除相关事件
	if err := model.DB.Where("license_id = ?", id).Delete(&model.LicenseEvent{}).Error; err != nil {
		response.ServerError(c, "删除授权事件失败: "+err.Error())
		return
	}

	// 删除授权
	if err := model.DB.Delete(&license).Error; err != nil {
		response.ServerError(c, "删除失败")
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
}

// ResetDevices 重置设备绑定
func (h *LicenseHandler) ResetDevices(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var license model.License
	if err := scopedLicenseQuery(c).Where("licenses.id = ?", id).First(&license).Error; err != nil {
		response.NotFound(c, "授权不存在")
		return
	}

	var deviceIDs []string
	if err := model.DB.Model(&model.Device{}).Where("license_id = ? AND tenant_id = ?", id, tenantID).Pluck("id", &deviceIDs).Error; err != nil {
		response.ServerError(c, "查询授权设备失败: "+err.Error())
		return
	}
	now := time.Now()
	var deletedCount int64
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := revokeActiveClientSessionsForDevices(tx, deviceIDs, now); err != nil {
			return err
		}
		result := tx.Where("license_id = ? AND tenant_id = ?", id, tenantID).Delete(&model.Device{})
		if result.Error != nil {
			return result.Error
		}
		deletedCount = result.RowsAffected
		return nil
	}); err != nil {
		response.ServerError(c, "重置失败: "+err.Error())
		return
	}

	// 记录事件
	event := model.LicenseEvent{
		LicenseID:    license.ID,
		EventType:    "devices_reset",
		OperatorType: "admin",
		IPAddress:    c.ClientIP(),
		Notes:        "重置设备绑定，删除 " + strconv.FormatInt(deletedCount, 10) + " 个设备",
	}
	if err := model.DB.Create(&event).Error; err != nil {
		response.ServerError(c, "记录授权事件失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "设备已重置", gin.H{
		"deleted_count": deletedCount,
	})
}
