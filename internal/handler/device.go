package handler

import (
	"errors"
	"io"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

	query := scopedDeviceQuery(c).
		Preload("License").
		Preload("License.Application").
		Preload("Subscription").
		Preload("Subscription.Application").
		Preload("Customer")

	if licenseID != "" {
		query = query.Where("devices.license_id = ?", licenseID)
	}
	if subscriptionID != "" {
		query = query.Where("devices.subscription_id = ?", subscriptionID)
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
	if err := query.Count(&total).Error; err != nil {
		response.ServerError(c, "获取设备数量失败")
		return
	}

	var devices []model.Device
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Order("devices.last_active_at DESC").Find(&devices).Error; err != nil {
		response.ServerError(c, "获取设备列表失败")
		return
	}

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
	if err := scopedDeviceQuery(c).Preload("License").Preload("License.Application").
		Preload("Subscription").Preload("Subscription.Application").
		Preload("Customer").
		Where("devices.id = ?", id).First(&device).Error; err != nil {
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
		"subscription":      device.Subscription,
		"customer":          device.Customer,
		"recent_heartbeats": heartbeats,
	})
}

// Unbind 解绑设备
func (h *DeviceHandler) Unbind(c *gin.Context) {
	id := c.Param("id")

	var device model.Device
	if err := scopedDeviceQuery(c).Where("devices.id = ?", id).First(&device).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	now := time.Now()
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&device).Error; err != nil {
			return err
		}
		return revokeActiveClientSessionsForDevice(tx, device.ID, now)
	}); err != nil {
		response.ServerError(c, "解绑失败")
		return
	}

	response.SuccessWithMessage(c, "解绑成功", nil)
}

// Blacklist 加入黑名单
func (h *DeviceHandler) Blacklist(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var device model.Device
	if err := scopedDeviceQuery(c).Preload("License").Preload("Subscription").Where("devices.id = ?", id).First(&device).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 添加到黑名单
	blacklist := model.DeviceBlacklist{
		TenantID:  tenantID,
		MachineID: device.MachineID,
		Reason:    req.Reason,
		CreatedBy: middleware.GetUserID(c),
	}
	if device.License != nil {
		blacklist.AppID = device.License.AppID
	} else if device.Subscription != nil {
		blacklist.AppID = device.Subscription.AppID
	}

	now := time.Now()
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&blacklist).Error; err != nil {
			return err
		}
		if err := tx.Model(&device).Update("status", model.DeviceStatusBlacklisted).Error; err != nil {
			return err
		}
		return revokeActiveClientSessionsForDevice(tx, device.ID, now)
	}); err != nil {
		response.ServerError(c, "加入黑名单失败")
		return
	}

	response.SuccessWithMessage(c, "已加入黑名单", nil)
}

// Unblacklist 按设备 ID 从黑名单移除
func (h *DeviceHandler) Unblacklist(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var device model.Device
	if err := scopedDeviceQuery(c).Preload("License").Preload("Subscription").
		Where("devices.id = ?", id).First(&device).Error; err != nil {
		response.NotFound(c, "设备不存在")
		return
	}

	appID := deviceAppID(device)
	query := model.DB.Where("machine_id = ? AND tenant_id = ?", device.MachineID, tenantID)
	if appID != "" {
		query = query.Where("(app_id = ? OR app_id IS NULL OR app_id = '')", appID)
	} else {
		query = query.Where("(app_id IS NULL OR app_id = '')")
	}
	result := query.Delete(&model.DeviceBlacklist{})
	if result.Error != nil {
		response.ServerError(c, "移除黑名单失败: "+result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		response.NotFound(c, "黑名单记录不存在")
		return
	}

	if device.Status == model.DeviceStatusBlacklisted {
		if err := model.DB.Model(&device).Update("status", model.DeviceStatusActive).Error; err != nil {
			response.ServerError(c, "恢复设备状态失败: "+err.Error())
			return
		}
	}

	response.SuccessWithMessage(c, "已从黑名单移除", nil)
}

// RemoveFromBlacklist 从黑名单移除
func (h *DeviceHandler) RemoveFromBlacklist(c *gin.Context) {
	machineID := c.Param("machine_id")
	tenantID := middleware.GetTenantID(c)
	appID := c.Query("app_id")
	scope := c.Query("scope")

	deviceAccessQuery := scopedDeviceQuery(c).Where("devices.machine_id = ?", machineID)
	if appID != "" {
		deviceAccessQuery = deviceAccessQuery.Where(
			"(license_id IN (SELECT id FROM licenses WHERE app_id = ? AND tenant_id = ?)) OR (subscription_id IN (SELECT id FROM subscriptions WHERE app_id = ? AND tenant_id = ?))",
			appID, tenantID, appID, tenantID,
		)
	}
	var accessibleDeviceCount int64
	if err := deviceAccessQuery.Count(&accessibleDeviceCount).Error; err != nil {
		response.ServerError(c, "校验设备权限失败")
		return
	}
	if accessibleDeviceCount == 0 {
		response.NotFound(c, "黑名单记录不存在")
		return
	}

	var deleted int64
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Where("machine_id = ? AND tenant_id = ?", machineID, tenantID)
		if appID != "" {
			query = query.Where("app_id = ?", appID)
		} else if scope == "global" {
			query = query.Where("(app_id IS NULL OR app_id = '')")
		}
		result := query.Delete(&model.DeviceBlacklist{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected
		if deleted == 0 {
			return nil
		}

		deviceQuery := tx.Preload("License").Preload("Subscription").
			Where("machine_id = ? AND tenant_id = ? AND status = ?", machineID, tenantID, model.DeviceStatusBlacklisted)
		if appID != "" {
			deviceQuery = deviceQuery.Where(
				"(license_id IN (SELECT id FROM licenses WHERE app_id = ? AND tenant_id = ?)) OR (subscription_id IN (SELECT id FROM subscriptions WHERE app_id = ? AND tenant_id = ?))",
				appID, tenantID, appID, tenantID,
			)
		}

		var devices []model.Device
		if err := deviceQuery.Find(&devices).Error; err != nil {
			return err
		}
		for i := range devices {
			device := &devices[i]
			stillBlacklisted, err := deviceStillBlacklisted(tx, device, tenantID)
			if err != nil {
				return err
			}
			if stillBlacklisted {
				continue
			}
			if err := tx.Model(device).Update("status", model.DeviceStatusActive).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		response.ServerError(c, "移出黑名单失败")
		return
	}
	if deleted == 0 {
		response.NotFound(c, "黑名单记录不存在")
		return
	}

	response.SuccessWithMessage(c, "已从黑名单移除", nil)
}

func deviceStillBlacklisted(tx *gorm.DB, device *model.Device, tenantID string) (bool, error) {
	appID := deviceAppID(*device)
	query := tx.Model(&model.DeviceBlacklist{}).
		Where("tenant_id = ? AND machine_id = ?", tenantID, device.MachineID)
	if appID != "" {
		query = query.Where("(app_id = ? OR app_id IS NULL OR app_id = '')", appID)
	} else {
		query = query.Where("(app_id IS NULL OR app_id = '')")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func deviceAppID(device model.Device) string {
	if device.License != nil {
		return device.License.AppID
	}
	if device.Subscription != nil {
		return device.Subscription.AppID
	}
	return ""
}

// GetBlacklist 获取黑名单列表
func (h *DeviceHandler) GetBlacklist(c *gin.Context) {
	page, pageSize := parsePageParams(c, 20, 100)
	appID := c.Query("app_id")

	query := scopedDeviceBlacklistQuery(c)
	if appID != "" {
		query = query.Where("device_blacklist.app_id = ?", appID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.ServerError(c, "获取黑名单数量失败")
		return
	}

	type blacklistRow struct {
		model.DeviceBlacklist
		CustomerID    string `json:"customer_id"`
		CustomerName  string `json:"customer_name"`
		CustomerEmail string `json:"customer_email"`
	}

	var blacklist []blacklistRow
	if err := query.
		Select(`
			device_blacklist.*,
			COALESCE(MAX(customers.id), '') AS customer_id,
			COALESCE(MAX(customers.name), '') AS customer_name,
			COALESCE(MAX(customers.email), '') AS customer_email
		`).
		Joins(`
			LEFT JOIN devices ON devices.tenant_id = device_blacklist.tenant_id
				AND devices.machine_id = device_blacklist.machine_id
				AND devices.deleted_at IS NULL
				AND (
					device_blacklist.app_id = ''
					OR device_blacklist.app_id IS NULL
					OR devices.license_id IN (
						SELECT id FROM licenses WHERE licenses.app_id = device_blacklist.app_id AND licenses.tenant_id = device_blacklist.tenant_id
					)
					OR devices.subscription_id IN (
						SELECT id FROM subscriptions WHERE subscriptions.app_id = device_blacklist.app_id AND subscriptions.tenant_id = device_blacklist.tenant_id
					)
				)
		`).
		Joins("LEFT JOIN customers ON customers.id = devices.customer_id AND customers.deleted_at IS NULL").
		Group("device_blacklist.id").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Order("device_blacklist.created_at DESC").
		Find(&blacklist).Error; err != nil {
		response.ServerError(c, "获取黑名单列表失败")
		return
	}

	response.SuccessPage(c, blacklist, total, page, pageSize)
}
