package handler

import (
	"errors"
	"strings"

	"license-server/internal/model"

	"gorm.io/gorm"
)

func ensureDeviceNotBlacklistedForApp(machineID string, app *model.Application) error {
	var blacklist model.DeviceBlacklist
	if err := model.DB.Where(
		"tenant_id = ? AND machine_id = ? AND (app_id = ? OR app_id IS NULL OR app_id = '')",
		app.TenantID,
		machineID,
		app.ID,
	).First(&blacklist).Error; err == nil {
		return errors.New("设备已被禁止使用")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("检查设备黑名单失败")
	}
	return nil
}

func loadActiveDeviceForApp(machineID string, app *model.Application) (*model.Device, error) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return nil, errors.New("缺少机器码")
	}

	if err := ensureDeviceNotBlacklistedForApp(machineID, app); err != nil {
		return nil, err
	}

	var devices []model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		Joins("LEFT JOIN licenses ON licenses.id = devices.license_id").
		Joins("LEFT JOIN subscriptions ON subscriptions.id = devices.subscription_id").
		Where("devices.machine_id = ? AND devices.tenant_id = ?", machineID, app.TenantID).
		Where("(licenses.app_id = ? OR subscriptions.app_id = ?)", app.ID, app.ID).
		Order("devices.created_at DESC").
		Find(&devices).Error; err != nil {
		return nil, err
	}

	if len(devices) == 0 {
		var count int64
		model.DB.Model(&model.Device{}).Where("machine_id = ? AND tenant_id = ?", machineID, app.TenantID).Count(&count)
		if count > 0 {
			return nil, errors.New("设备未绑定当前应用")
		}
		return nil, errors.New("设备未授权")
	}

	hasExpiredEntitlement := false
	for i := range devices {
		device := &devices[i]
		if device.Status == model.DeviceStatusBlacklisted {
			return nil, errors.New("设备已被禁止使用")
		}
		if device.License != nil && device.License.AppID == app.ID {
			if device.License.IsValid() {
				return device, nil
			}
			hasExpiredEntitlement = true
		}
		if device.Subscription != nil && device.Subscription.AppID == app.ID {
			if device.Subscription.IsValid() {
				return device, nil
			}
			hasExpiredEntitlement = true
		}
	}

	if hasExpiredEntitlement {
		return nil, errors.New("授权或订阅已失效")
	}
	return nil, errors.New("设备未绑定当前应用")
}

func loadActiveLicenseDeviceForApp(machineID string, app *model.Application) (*model.Device, error) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return nil, errors.New("缺少机器码")
	}
	if err := ensureDeviceNotBlacklistedForApp(machineID, app); err != nil {
		return nil, err
	}

	var devices []model.Device
	if err := model.DB.Preload("License").
		Joins("JOIN licenses ON licenses.id = devices.license_id").
		Where("devices.machine_id = ? AND devices.tenant_id = ? AND licenses.app_id = ?", machineID, app.TenantID, app.ID).
		Order("devices.created_at DESC").
		Find(&devices).Error; err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, errors.New("设备未绑定当前应用授权")
	}

	hasExpiredEntitlement := false
	for i := range devices {
		device := &devices[i]
		if device.Status == model.DeviceStatusBlacklisted {
			return nil, errors.New("设备已被禁止使用")
		}
		if device.License != nil && device.License.AppID == app.ID {
			if device.License.IsValid() {
				return device, nil
			}
			hasExpiredEntitlement = true
		}
	}
	if hasExpiredEntitlement {
		return nil, errors.New("授权已失效")
	}
	return nil, errors.New("设备未绑定当前应用授权")
}

func loadActiveSubscriptionDeviceForApp(machineID string, app *model.Application) (*model.Device, error) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return nil, errors.New("缺少机器码")
	}
	if err := ensureDeviceNotBlacklistedForApp(machineID, app); err != nil {
		return nil, err
	}

	var devices []model.Device
	if err := model.DB.Preload("Subscription").
		Joins("JOIN subscriptions ON subscriptions.id = devices.subscription_id").
		Where("devices.machine_id = ? AND devices.tenant_id = ? AND subscriptions.app_id = ?", machineID, app.TenantID, app.ID).
		Order("devices.created_at DESC").
		Find(&devices).Error; err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, errors.New("设备未绑定当前应用订阅")
	}

	hasExpiredEntitlement := false
	for i := range devices {
		device := &devices[i]
		if device.Status == model.DeviceStatusBlacklisted {
			return nil, errors.New("设备已被禁止使用")
		}
		if device.Subscription != nil && device.Subscription.AppID == app.ID {
			if device.Subscription.IsValid() {
				return device, nil
			}
			hasExpiredEntitlement = true
		}
	}
	if hasExpiredEntitlement {
		return nil, errors.New("订阅已失效")
	}
	return nil, errors.New("设备未绑定当前应用订阅")
}

func normalizeClientScriptDeliveryStatus(status string) (model.ScriptDeliveryStatus, bool) {
	switch model.ScriptDeliveryStatus(strings.ToLower(strings.TrimSpace(status))) {
	case model.ScriptDeliveryStatusExecuting:
		return model.ScriptDeliveryStatusExecuting, true
	case model.ScriptDeliveryStatusSuccess:
		return model.ScriptDeliveryStatusSuccess, true
	case model.ScriptDeliveryStatusFailed:
		return model.ScriptDeliveryStatusFailed, true
	default:
		return "", false
	}
}
