package model

import (
	"time"
)

// Device 设备模型
type Device struct {
	BaseModel
	TenantID        string       `gorm:"type:char(36);index;not null;uniqueIndex:idx_devices_license_machine;uniqueIndex:idx_devices_subscription_machine" json:"tenant_id"` // 所属租户
	CustomerID      string       `gorm:"type:char(36);index;not null" json:"customer_id"`                                                                                    // 关联客户
	LicenseID       *string      `gorm:"type:varchar(36);uniqueIndex:idx_devices_license_machine" json:"license_id"`                                                         // 授权码模式（可为空）
	SubscriptionID  *string      `gorm:"type:varchar(36);uniqueIndex:idx_devices_subscription_machine" json:"subscription_id"`                                               // 订阅模式（可为空）
	MachineID       string       `gorm:"type:varchar(128);not null;uniqueIndex:idx_devices_license_machine;uniqueIndex:idx_devices_subscription_machine" json:"machine_id"`
	DeviceName      string       `gorm:"type:varchar(100)" json:"device_name"`
	Hostname        string       `gorm:"type:varchar(100)" json:"hostname"`
	OSType          string       `gorm:"type:varchar(50)" json:"os_type"`
	OSVersion       string       `gorm:"type:varchar(50)" json:"os_version"`
	AppVersion      string       `gorm:"type:varchar(20)" json:"app_version"`
	IPAddress       string       `gorm:"type:varchar(45)" json:"ip_address"`
	IPCountry       string       `gorm:"type:varchar(50)" json:"ip_country"`
	IPCity          string       `gorm:"type:varchar(100)" json:"ip_city"`
	Status          DeviceStatus `gorm:"type:varchar(20);default:active" json:"status"`
	LastHeartbeatAt *time.Time   `json:"last_heartbeat_at"`
	LastActiveAt    *time.Time   `json:"last_active_at"`
	// 关联
	Tenant       *Tenant       `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Customer     *Customer     `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	License      *License      `gorm:"foreignKey:LicenseID;constraint:OnDelete:SET NULL" json:"license,omitempty"`
	Subscription *Subscription `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:SET NULL" json:"subscription,omitempty"`
}

type DeviceStatus string

const (
	DeviceStatusActive      DeviceStatus = "active"
	DeviceStatusInactive    DeviceStatus = "inactive"
	DeviceStatusBlacklisted DeviceStatus = "blacklisted"
)

func (Device) TableName() string {
	return "devices"
}

// DeviceBlacklist 设备黑名单
type DeviceBlacklist struct {
	BaseModel
	TenantID  string `gorm:"type:char(36);index;not null" json:"tenant_id"` // 所属租户
	MachineID string `gorm:"type:varchar(128);not null" json:"machine_id"`
	AppID     string `gorm:"type:varchar(36)" json:"app_id"`
	Reason    string `gorm:"type:varchar(255)" json:"reason"`
	CreatedBy string `gorm:"type:varchar(36)" json:"created_by"`
}

func (DeviceBlacklist) TableName() string {
	return "device_blacklist"
}

// Heartbeat 心跳记录
type Heartbeat struct {
	BaseModel
	TenantID       string `gorm:"type:char(36);index;not null" json:"tenant_id"` // 所属租户
	AuthMode       string `gorm:"type:varchar(20);not null;default:license;index" json:"auth_mode"`
	LicenseID      string `gorm:"type:varchar(36);index" json:"license_id"`
	SubscriptionID string `gorm:"type:varchar(36);index" json:"subscription_id"`
	DeviceID       string `gorm:"type:varchar(36);not null;index" json:"device_id"`
	IPAddress      string `gorm:"type:varchar(45)" json:"ip_address"`
	AppVersion     string `gorm:"type:varchar(20)" json:"app_version"`
}

func (Heartbeat) TableName() string {
	return "heartbeats"
}
