package handler

import (
	"time"

	"license-server/internal/model"

	"gorm.io/gorm"
)

func revokeActiveClientSessionsForDevice(tx *gorm.DB, deviceID string, now time.Time) error {
	if tx == nil {
		tx = model.DB
	}
	return tx.Model(&model.ClientSession{}).
		Where("device_id = ? AND revoked_at IS NULL", deviceID).
		Updates(map[string]interface{}{
			"revoked_at":   now,
			"last_used_at": now,
		}).Error
}

func revokeActiveClientSessionsForDevices(tx *gorm.DB, deviceIDs []string, now time.Time) error {
	if len(deviceIDs) == 0 {
		return nil
	}
	if tx == nil {
		tx = model.DB
	}
	return tx.Model(&model.ClientSession{}).
		Where("device_id IN ? AND revoked_at IS NULL", deviceIDs).
		Updates(map[string]interface{}{
			"revoked_at":   now,
			"last_used_at": now,
		}).Error
}

func revokeActiveClientSessionsForCustomer(tx *gorm.DB, tenantID, customerID string, now time.Time) error {
	if tx == nil {
		tx = model.DB
	}
	return tx.Model(&model.ClientSession{}).
		Where("tenant_id = ? AND customer_id = ? AND revoked_at IS NULL", tenantID, customerID).
		Updates(map[string]interface{}{
			"revoked_at":   now,
			"last_used_at": now,
		}).Error
}
