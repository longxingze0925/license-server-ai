package handler

import (
	"encoding/json"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type SubscriptionHandler struct{}

type subscriptionAccountListItem struct {
	CustomerID        string     `gorm:"column:customer_id"`
	CustomerEmail     string     `gorm:"column:customer_email"`
	CustomerName      string     `gorm:"column:customer_name"`
	SubscriptionCount int64      `gorm:"column:subscription_count"`
	AppNamesRaw       string     `gorm:"column:app_names_raw"`
	ActiveCount       int64      `gorm:"column:active_count"`
	ExpiredCount      int64      `gorm:"column:expired_count"`
	CancelledCount    int64      `gorm:"column:cancelled_count"`
	SuspendedCount    int64      `gorm:"column:suspended_count"`
	PermanentCount    int64      `gorm:"column:permanent_count"`
	NearestExpireAt   *time.Time `gorm:"column:nearest_expire_at"`
	LatestCreatedAt   time.Time  `gorm:"column:latest_created_at"`
}

func NewSubscriptionHandler() *SubscriptionHandler {
	return &SubscriptionHandler{}
}

// CreateSubscriptionRequest 创建订阅请求
type CreateSubscriptionRequest struct {
	CustomerID  string   `json:"customer_id" binding:"required"`
	AppID       string   `json:"app_id" binding:"required"`
	PlanType    string   `json:"plan_type"` // 可选，默认 basic
	MaxDevices  int      `json:"max_devices"`
	UnbindLimit int      `json:"unbind_limit"`
	Features    []string `json:"features"`
	Days        int      `json:"days"` // 有效天数，-1表示永久
	Notes       string   `json:"notes"`
}

// Create 创建订阅
func (h *SubscriptionHandler) Create(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req CreateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证客户（必须属于当前租户）
	var customer model.Customer
	if err := model.DB.Where("id = ? AND tenant_id = ?", req.CustomerID, tenantID).First(&customer).Error; err != nil {
		response.NotFound(c, "客户不存在")
		return
	}

	// 验证应用（必须属于当前租户）
	var app model.Application
	if err := model.DB.Where("id = ? AND tenant_id = ?", req.AppID, tenantID).First(&app).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 检查是否已有该应用的订阅
	var existingSub model.Subscription
	if err := model.DB.Where("customer_id = ? AND app_id = ? AND tenant_id = ? AND status = ?", req.CustomerID, req.AppID, tenantID, model.SubscriptionStatusActive).First(&existingSub).Error; err == nil {
		response.Error(c, 400, "该客户已有此应用的有效订阅")
		return
	}

	// 设置默认值
	if req.MaxDevices == 0 {
		req.MaxDevices = app.MaxDevicesDefault
	}
	if req.PlanType == "" {
		req.PlanType = "basic"
	}
	if req.UnbindLimit < 0 {
		response.BadRequest(c, "解绑次数上限不能小于0")
		return
	}
	if req.UnbindLimit == 0 {
		req.UnbindLimit = 5
	}

	// 序列化 features
	featuresJSON := "[]"
	if len(req.Features) > 0 {
		bytes, _ := json.Marshal(req.Features)
		featuresJSON = string(bytes)
	}

	now := time.Now()
	subscription := model.Subscription{
		TenantID:    tenantID,
		CustomerID:  req.CustomerID,
		AppID:       req.AppID,
		PlanType:    model.PlanType(req.PlanType),
		MaxDevices:  req.MaxDevices,
		UnbindLimit: req.UnbindLimit,
		Features:    featuresJSON,
		Status:      model.SubscriptionStatusActive,
		StartAt:     &now,
		Notes:       req.Notes,
	}

	// 设置过期时间
	if req.Days > 0 {
		expireAt := now.AddDate(0, 0, req.Days)
		subscription.ExpireAt = &expireAt
	}

	if err := model.DB.Create(&subscription).Error; err != nil {
		response.ServerError(c, "创建订阅失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"id":               subscription.ID,
		"tenant_id":        subscription.TenantID,
		"customer_id":      subscription.CustomerID,
		"app_id":           subscription.AppID,
		"plan_type":        subscription.PlanType,
		"status":           subscription.Status,
		"unbind_limit":     subscription.UnbindLimit,
		"unbind_used":      subscription.UnbindUsed,
		"unbind_remaining": subscription.RemainingClientUnbindCount(),
		"expire_at":        subscription.ExpireAt,
		"created_at":       subscription.CreatedAt,
	})
}

// List 获取订阅列表
func (h *SubscriptionHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	customerID := c.Query("customer_id")
	appID := c.Query("app_id")
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := model.DB.Model(&model.Subscription{}).
		Preload("Customer").
		Preload("Application").
		Where("tenant_id = ?", tenantID)

	if customerID != "" {
		query = query.Where("customer_id = ?", customerID)
	}
	if appID != "" {
		query = query.Where("app_id = ?", appID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var subscriptions []model.Subscription
	query.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&subscriptions)

	var result []gin.H
	for _, sub := range subscriptions {
		item := gin.H{
			"id":               sub.ID,
			"customer_id":      sub.CustomerID,
			"app_id":           sub.AppID,
			"plan_type":        sub.PlanType,
			"max_devices":      sub.MaxDevices,
			"unbind_limit":     sub.UnbindLimit,
			"unbind_used":      sub.UnbindUsed,
			"unbind_remaining": sub.RemainingClientUnbindCount(),
			"status":           sub.Status,
			"start_at":         sub.StartAt,
			"expire_at":        sub.ExpireAt,
			"remaining_days":   sub.RemainingDays(),
			"created_at":       sub.CreatedAt,
		}
		if sub.Customer != nil {
			item["customer_email"] = sub.Customer.Email
			item["customer_name"] = sub.Customer.Name
		}
		if sub.Application != nil {
			item["app_name"] = sub.Application.Name
		}
		result = append(result, item)
	}

	response.SuccessPage(c, result, total, page, pageSize)
}

// ListAccounts 获取账号聚合订阅列表
func (h *SubscriptionHandler) ListAccounts(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

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

	baseQuery := model.DB.Model(&model.Subscription{}).
		Joins("LEFT JOIN customers ON customers.id = subscriptions.customer_id").
		Joins("LEFT JOIN applications ON applications.id = subscriptions.app_id").
		Where("subscriptions.tenant_id = ?", tenantID)

	if appID != "" {
		baseQuery = baseQuery.Where("subscriptions.app_id = ?", appID)
	}
	if status != "" {
		baseQuery = baseQuery.Where("subscriptions.status = ?", status)
	}
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		baseQuery = baseQuery.Where(
			"customers.email LIKE ? OR customers.name LIKE ? OR customers.company LIKE ?",
			likeKeyword,
			likeKeyword,
			likeKeyword,
		)
	}

	var total int64
	if err := baseQuery.Distinct("subscriptions.customer_id").Count(&total).Error; err != nil {
		response.ServerError(c, "获取账号聚合列表失败")
		return
	}

	var rows []subscriptionAccountListItem
	if err := baseQuery.
		Select(`
			subscriptions.customer_id AS customer_id,
			COALESCE(customers.email, '') AS customer_email,
			COALESCE(customers.name, '') AS customer_name,
			COUNT(*) AS subscription_count,
			GROUP_CONCAT(DISTINCT applications.name ORDER BY applications.name SEPARATOR '||') AS app_names_raw,
			SUM(CASE WHEN subscriptions.status = 'active' THEN 1 ELSE 0 END) AS active_count,
			SUM(CASE WHEN subscriptions.status = 'expired' THEN 1 ELSE 0 END) AS expired_count,
			SUM(CASE WHEN subscriptions.status = 'cancelled' THEN 1 ELSE 0 END) AS cancelled_count,
			SUM(CASE WHEN subscriptions.status = 'suspended' THEN 1 ELSE 0 END) AS suspended_count,
			SUM(CASE WHEN subscriptions.status = 'active' AND subscriptions.expire_at IS NULL THEN 1 ELSE 0 END) AS permanent_count,
			MIN(CASE WHEN subscriptions.status = 'active' AND subscriptions.expire_at IS NOT NULL THEN subscriptions.expire_at END) AS nearest_expire_at,
			MAX(subscriptions.created_at) AS latest_created_at
		`).
		Group("subscriptions.customer_id, customers.email, customers.name").
		Order("latest_created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&rows).Error; err != nil {
		response.ServerError(c, "获取账号聚合列表失败")
		return
	}

	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		appNames := make([]string, 0)
		if row.AppNamesRaw != "" {
			appNames = strings.Split(row.AppNamesRaw, "||")
		}

		result = append(result, gin.H{
			"customer_id":        row.CustomerID,
			"customer_email":     row.CustomerEmail,
			"customer_name":      row.CustomerName,
			"subscription_count": row.SubscriptionCount,
			"app_names":          appNames,
			"active_count":       row.ActiveCount,
			"expired_count":      row.ExpiredCount,
			"cancelled_count":    row.CancelledCount,
			"suspended_count":    row.SuspendedCount,
			"permanent_count":    row.PermanentCount,
			"nearest_expire_at":  row.NearestExpireAt,
			"latest_created_at":  row.LatestCreatedAt,
		})
	}

	response.SuccessPage(c, result, total, page, pageSize)
}

// Get 获取订阅详情
func (h *SubscriptionHandler) Get(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var subscription model.Subscription
	if err := model.DB.Preload("Customer").Preload("Application").Preload("Devices").
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&subscription).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	// 解析 features
	var features []string
	if subscription.Features != "" {
		json.Unmarshal([]byte(subscription.Features), &features)
	}

	response.Success(c, gin.H{
		"id":               subscription.ID,
		"customer_id":      subscription.CustomerID,
		"app_id":           subscription.AppID,
		"plan_type":        subscription.PlanType,
		"max_devices":      subscription.MaxDevices,
		"unbind_limit":     subscription.UnbindLimit,
		"unbind_used":      subscription.UnbindUsed,
		"unbind_remaining": subscription.RemainingClientUnbindCount(),
		"features":         features,
		"status":           subscription.Status,
		"start_at":         subscription.StartAt,
		"expire_at":        subscription.ExpireAt,
		"remaining_days":   subscription.RemainingDays(),
		"auto_renew":       subscription.AutoRenew,
		"notes":            subscription.Notes,
		"customer":         subscription.Customer,
		"application":      subscription.Application,
		"devices":          subscription.Devices,
		"created_at":       subscription.CreatedAt,
	})
}

// UpdateSubscriptionRequest 更新订阅请求
type UpdateSubscriptionRequest struct {
	PlanType    string   `json:"plan_type"`
	MaxDevices  int      `json:"max_devices"`
	UnbindLimit *int     `json:"unbind_limit"`
	Features    []string `json:"features"`
	Status      string   `json:"status"`
	Notes       *string  `json:"notes"`
}

// Update 更新订阅
func (h *SubscriptionHandler) Update(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var req UpdateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var subscription model.Subscription
	if err := model.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&subscription).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	updates := map[string]interface{}{}
	if req.PlanType != "" {
		updates["plan_type"] = req.PlanType
	}
	if req.MaxDevices > 0 {
		updates["max_devices"] = req.MaxDevices
	}
	if req.UnbindLimit != nil {
		if *req.UnbindLimit < 0 {
			response.BadRequest(c, "解绑次数上限不能小于0")
			return
		}
		updates["unbind_limit"] = *req.UnbindLimit
	}
	if req.Features != nil {
		featuresJSON, _ := json.Marshal(req.Features)
		updates["features"] = string(featuresJSON)
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.Notes != nil {
		updates["notes"] = *req.Notes
	}

	if err := model.DB.Model(&subscription).Updates(updates).Error; err != nil {
		response.ServerError(c, "更新订阅失败")
		return
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

// ResetUnbindCount 重置客户端解绑计数
func (h *SubscriptionHandler) ResetUnbindCount(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var subscription model.Subscription
	if err := model.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&subscription).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	if err := model.DB.Model(&subscription).Update("unbind_used", 0).Error; err != nil {
		response.ServerError(c, "重置解绑次数失败")
		return
	}

	response.SuccessWithMessage(c, "解绑次数已重置", gin.H{
		"unbind_limit":     subscription.UnbindLimit,
		"unbind_used":      0,
		"unbind_remaining": subscription.UnbindLimit,
	})
}

// Renew 续费订阅
func (h *SubscriptionHandler) Renew(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var req struct {
		Days int `json:"days" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var subscription model.Subscription
	if err := model.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&subscription).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	// 计算新的过期时间
	var newExpireAt time.Time
	if subscription.ExpireAt != nil && subscription.ExpireAt.After(time.Now()) {
		newExpireAt = subscription.ExpireAt.AddDate(0, 0, req.Days)
	} else {
		newExpireAt = time.Now().AddDate(0, 0, req.Days)
	}

	subscription.ExpireAt = &newExpireAt
	subscription.Status = model.SubscriptionStatusActive
	model.DB.Save(&subscription)

	response.Success(c, gin.H{
		"expire_at":      subscription.ExpireAt,
		"remaining_days": subscription.RemainingDays(),
	})
}

// Cancel 取消订阅
func (h *SubscriptionHandler) Cancel(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var subscription model.Subscription
	if err := model.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&subscription).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	now := time.Now()
	subscription.Status = model.SubscriptionStatusCancelled
	subscription.CancelledAt = &now
	model.DB.Save(&subscription)

	response.SuccessWithMessage(c, "订阅已取消", nil)
}

// Delete 删除订阅
func (h *SubscriptionHandler) Delete(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var subscription model.Subscription
	if err := model.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&subscription).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	// 删除关联的设备
	model.DB.Where("subscription_id = ? AND tenant_id = ?", id, tenantID).Delete(&model.Device{})

	if err := model.DB.Delete(&subscription).Error; err != nil {
		response.ServerError(c, "删除订阅失败")
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
}
