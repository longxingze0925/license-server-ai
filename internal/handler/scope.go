package handler

import (
	"license-server/internal/middleware"
	"license-server/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func scopedLicenseQuery(c *gin.Context) *gorm.DB {
	query := model.DB.Model(&model.License{}).
		Joins("LEFT JOIN customers ON customers.id = licenses.customer_id").
		Where("licenses.tenant_id = ?", middleware.GetTenantID(c))
	if !isTenantAdminRole(middleware.GetUserRole(c)) {
		query = query.Where("customers.owner_id = ?", middleware.GetUserID(c))
	}
	return query
}

func scopedDeviceQuery(c *gin.Context) *gorm.DB {
	query := model.DB.Model(&model.Device{}).
		Joins("LEFT JOIN customers ON customers.id = devices.customer_id").
		Where("devices.tenant_id = ?", middleware.GetTenantID(c))
	if !isTenantAdminRole(middleware.GetUserRole(c)) {
		query = query.Where("customers.owner_id = ?", middleware.GetUserID(c))
	}
	return query
}

func scopedDeviceBlacklistQuery(c *gin.Context) *gorm.DB {
	query := model.DB.Model(&model.DeviceBlacklist{}).
		Where("device_blacklist.tenant_id = ?", middleware.GetTenantID(c))
	if !isTenantAdminRole(middleware.GetUserRole(c)) {
		query = query.Where(
			`EXISTS (
				SELECT 1 FROM devices
				JOIN customers ON customers.id = devices.customer_id
				LEFT JOIN licenses ON licenses.id = devices.license_id
				LEFT JOIN subscriptions ON subscriptions.id = devices.subscription_id
				WHERE devices.tenant_id = device_blacklist.tenant_id
					AND devices.machine_id = device_blacklist.machine_id
					AND customers.owner_id = ?
					AND (
						device_blacklist.app_id = ''
						OR device_blacklist.app_id IS NULL
						OR licenses.app_id = device_blacklist.app_id
						OR subscriptions.app_id = device_blacklist.app_id
					)
			)`,
			middleware.GetUserID(c),
		)
	}
	return query
}

func scopedHeartbeatQuery(c *gin.Context) *gorm.DB {
	query := model.DB.Model(&model.Heartbeat{}).
		Joins("LEFT JOIN devices ON devices.id = heartbeats.device_id").
		Where("heartbeats.tenant_id = ?", middleware.GetTenantID(c))
	if !isTenantAdminRole(middleware.GetUserRole(c)) {
		query = query.
			Joins("LEFT JOIN customers ON customers.id = devices.customer_id").
			Where("customers.owner_id = ?", middleware.GetUserID(c))
	}
	return query
}
