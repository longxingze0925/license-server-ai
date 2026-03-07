package handler

import (
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"strconv"

	"github.com/gin-gonic/gin"
)

type DeviceHandler struct{}

func NewDeviceHandler() *DeviceHandler {
	return &DeviceHandler{}
}

// List 获取设备列表
func (h *DeviceHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	appID := c.Query("app_id")
	licenseID := c.Query("license_id")
	subscriptionID := c.Query("subscription_id")
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := model.DB.Model(&model.Device{}).
		Where("devices.tenant_id = ?", tenantID).
		Preload("License").
		Preload("License.Application").
		Preload("Subscription").
		Preload("Subscription.Application").
		Preload("Customer")

	if licenseID != "" {
		query = query.Where("license_id = ?", licenseID)
	}
	if subscriptionID != "" {
		query = query.Where("subscription_id = ?", subscriptionID)
	}
	if appID != "" {
		// 同时支持授权码模式和订阅模式的设备
		query = query.Where(
			"(license_id IN (SELECT id FROM licenses WHERE app_id = ? AND tenant_id = ?)) OR (subscription_id IN (SELECT id FROM subscriptions WHERE app_id = ? AND tenant_id = ?))",
			appID, tenantID, appID, tenantID,
		)
	}
	if status != "" {
		query = query.Where("devices.status = ?", status)
	}

	var total int64
	query.Count(&total)

	var devices []model.Device
	query.Offset((page - 1) * pageSize).Limit(pageSize).Order("devices.last_active_at DESC").Find(&devices)

	var result []gin.H
	for _, device := range devices {
		item := gin.H{
			"id":                device.ID,
			"machine_id":        device.MachineID,
			"device_name":       device.DeviceName,
			"hostname":          device.Hostname,
			"os_type":           device.OSType,
			"os_version":        device.OSVersion,
			"app_version":       device.AppVersion,
			"ip_address":        device.IPAddress,
			"ip_country":        device.IPCountry,
			"ip_city":           device.IPCity,
			"status":            device.Status,
			"last_heartbeat_at": device.LastHeartbeatAt,
			"last_active_at":    device.LastActiveAt,
			"created_at":        device.CreatedAt,
		}
		// 授权码模式
		if device.License != nil {
			item["license_id"] = device.License.ID
			item["license_key"] = device.License.LicenseKey
			item["license_status"] = device.License.Status
			if device.License.Application != nil {
				item["app_id"] = device.License.Application.ID
				item["app_name"] = device.License.Application.Name
			}
		}
		// 订阅模式
		if device.Subscription != nil {
			item["subscription_id"] = device.Subscription.ID
			item["plan_type"] = device.Subscription.PlanType
			item["subscription_status"] = device.Subscription.Status
			if device.Subscription.Application != nil {
				item["app_id"] = device.Subscription.Application.ID
				item["app_name"] = device.Subscription.Application.Name
			}
		}
		if device.Customer != nil {
			item["customer_id"] = device.Customer.ID
			item["customer_name"] = device.Customer.Name
			item["customer_email"] = device.Customer.Email
		}
		result = append(result, item)
	}

	response.SuccessPage(c, result, total, page, pageSize)
}

// Get 获取设备详情
func (h *DeviceHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var device model.Device
	if err := model.DB.Preload("License").Preload("License.Application").Preload("Customer").
		First(&device, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	// 获取最近心跳记录
	var heartbeats []model.Heartbeat
	model.DB.Where("device_id = ? AND tenant_id = ?", id, tenantID).Order("created_at DESC").Limit(10).Find(&heartbeats)

	response.Success(c, gin.H{
		"id":                device.ID,
		"license_id":        device.LicenseID,
		"customer_id":       device.CustomerID,
		"machine_id":        device.MachineID,
		"device_name":       device.DeviceName,
		"hostname":          device.Hostname,
		"os_type":           device.OSType,
		"os_version":        device.OSVersion,
		"app_version":       device.AppVersion,
		"ip_address":        device.IPAddress,
		"ip_country":        device.IPCountry,
		"ip_city":           device.IPCity,
		"status":            device.Status,
		"last_heartbeat_at": device.LastHeartbeatAt,
		"last_active_at":    device.LastActiveAt,
		"created_at":        device.CreatedAt,
		"license":           device.License,
		"customer":          device.Customer,
		"recent_heartbeats": heartbeats,
	})
}

// Unbind 解绑设备
func (h *DeviceHandler) Unbind(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var device model.Device
	if err := model.DB.First(&device, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	model.DB.Delete(&device)

	response.SuccessWithMessage(c, "解绑成功", nil)
}

// Blacklist 加入黑名单
func (h *DeviceHandler) Blacklist(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var device model.Device
	if err := model.DB.Preload("License").First(&device, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	c.ShouldBindJSON(&req)

	// 添加到黑名单
	blacklist := model.DeviceBlacklist{
		TenantID:  tenantID,
		MachineID: device.MachineID,
		Reason:    req.Reason,
	}
	if device.License != nil {
		blacklist.AppID = device.License.AppID
	}

	model.DB.Create(&blacklist)

	// 更新设备状态
	device.Status = model.DeviceStatusBlacklisted
	model.DB.Save(&device)

	response.SuccessWithMessage(c, "已加入黑名单", nil)
}

// Unblacklist 按设备 ID 从黑名单移除
func (h *DeviceHandler) Unblacklist(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var device model.Device
	if err := model.DB.First(&device, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	result := model.DB.Where("machine_id = ? AND tenant_id = ?", device.MachineID, tenantID).Delete(&model.DeviceBlacklist{})
	if result.RowsAffected == 0 {
		response.NotFound(c, "黑名单记录不存在")
		return
	}

	if device.Status == model.DeviceStatusBlacklisted {
		model.DB.Model(&device).Update("status", model.DeviceStatusActive)
	}

	response.SuccessWithMessage(c, "已从黑名单移除", nil)
}

// RemoveFromBlacklist 从黑名单移除
func (h *DeviceHandler) RemoveFromBlacklist(c *gin.Context) {
	machineID := c.Param("machine_id")
	tenantID := middleware.GetTenantID(c)

	result := model.DB.Where("machine_id = ? AND tenant_id = ?", machineID, tenantID).Delete(&model.DeviceBlacklist{})
	if result.RowsAffected == 0 {
		response.NotFound(c, "黑名单记录不存在")
		return
	}

	// 更新相关设备状态
	model.DB.Model(&model.Device{}).Where("machine_id = ? AND tenant_id = ? AND status = ?", machineID, tenantID, model.DeviceStatusBlacklisted).
		Update("status", model.DeviceStatusActive)

	response.SuccessWithMessage(c, "已从黑名单移除", nil)
}

// GetBlacklist 获取黑名单列表
func (h *DeviceHandler) GetBlacklist(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	appID := c.Query("app_id")

	query := model.DB.Model(&model.DeviceBlacklist{}).Where("tenant_id = ?", tenantID)
	if appID != "" {
		query = query.Where("app_id = ?", appID)
	}

	var total int64
	query.Count(&total)

	var blacklist []model.DeviceBlacklist
	query.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&blacklist)

	response.SuccessPage(c, blacklist, total, page, pageSize)
}
