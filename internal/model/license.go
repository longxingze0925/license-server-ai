package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// License 授权模型
type License struct {
	BaseModel
	TenantID        string        `gorm:"type:char(36);index;not null" json:"tenant_id"` // 所属租户
	AppID           string        `gorm:"type:varchar(36);index;not null" json:"app_id"` // 所属应用
	CustomerID      *string       `gorm:"type:char(36);index" json:"customer_id"`        // 关联客户（可选）
	LicenseKey      string        `gorm:"type:varchar(64);uniqueIndex;not null" json:"license_key"`
	Type            LicenseType   `gorm:"type:varchar(20);default:subscription" json:"type"`
	DurationDays    int           `gorm:"not null" json:"duration_days"` // -1 表示永久
	MaxDevices      int           `gorm:"default:1" json:"max_devices"`
	UnbindLimit     int           `gorm:"default:5;not null" json:"unbind_limit"` // 客户端终身可解绑总次数
	UnbindUsed      int           `gorm:"default:0;not null" json:"unbind_used"`  // 客户端已解绑次数
	MaxConcurrent   int           `gorm:"default:0" json:"max_concurrent"`        // 最大并发数，0表示不限制
	Features        string        `gorm:"type:json" json:"features"`              // 功能列表 JSON数组
	ActivatedAt     *time.Time    `json:"activated_at"`
	ExpireAt        *time.Time    `json:"expire_at"`
	GraceExpireAt   *time.Time    `json:"grace_expire_at"` // 宽限期到期时间
	LastValidatedAt *time.Time    `json:"last_validated_at"`
	Status          LicenseStatus `gorm:"type:varchar(20);default:pending" json:"status"`
	SuspendedReason string        `gorm:"type:varchar(255)" json:"suspended_reason"`
	RevokedReason   string        `gorm:"type:varchar(255)" json:"revoked_reason"`
	Metadata        string        `gorm:"type:json" json:"metadata"` // 自定义元数据
	Notes           string        `gorm:"type:text" json:"notes"`    // 备注
	// 关联
	Tenant      *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Application *Application   `gorm:"foreignKey:AppID" json:"application,omitempty"`
	Customer    *Customer      `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Devices     []Device       `gorm:"foreignKey:LicenseID" json:"devices,omitempty"`
	Events      []LicenseEvent `gorm:"foreignKey:LicenseID" json:"events,omitempty"`
}

type LicenseType string

const (
	LicenseTypeTrial        LicenseType = "trial"
	LicenseTypeSubscription LicenseType = "subscription"
	LicenseTypePerpetual    LicenseType = "perpetual"
	LicenseTypeNodeLocked   LicenseType = "node_locked"
)

type LicenseStatus string

const (
	LicenseStatusPending   LicenseStatus = "pending"
	LicenseStatusActive    LicenseStatus = "active"
	LicenseStatusExpired   LicenseStatus = "expired"
	LicenseStatusSuspended LicenseStatus = "suspended"
	LicenseStatusRevoked   LicenseStatus = "revoked"
)

func (License) TableName() string {
	return "licenses"
}

// IsValid 检查授权是否有效
func (l *License) IsValid() bool {
	if l.Status != LicenseStatusActive {
		return false
	}
	if l.ExpireAt != nil && time.Now().After(*l.ExpireAt) {
		// 检查宽限期
		if l.GraceExpireAt != nil && time.Now().Before(*l.GraceExpireAt) {
			return true
		}
		return false
	}
	return true
}

// RemainingDays 剩余天数
func (l *License) RemainingDays() int {
	if l.DurationDays == -1 {
		return -1 // 永久
	}
	if l.ExpireAt == nil {
		return 0
	}
	remaining := time.Until(*l.ExpireAt)
	if remaining < 0 {
		return 0
	}
	return int(remaining.Hours() / 24)
}

// RemainingClientUnbindCount 客户端解绑剩余次数
func (l *License) RemainingClientUnbindCount() int {
	remaining := l.UnbindLimit - l.UnbindUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CanClientUnbind 是否允许客户端继续解绑
func (l *License) CanClientUnbind() bool {
	return l.UnbindUsed < l.UnbindLimit
}

// LicenseEvent 授权事件记录
type LicenseEvent struct {
	BaseModel
	LicenseID    string           `gorm:"type:varchar(36);not null" json:"license_id"`
	EventType    LicenseEventType `gorm:"type:varchar(20);not null" json:"event_type"`
	FromValue    string           `gorm:"type:json" json:"from_value"`
	ToValue      string           `gorm:"type:json" json:"to_value"`
	OperatorID   string           `gorm:"type:varchar(36)" json:"operator_id"`
	OperatorType string           `gorm:"type:varchar(20);default:system" json:"operator_type"`
	IPAddress    string           `gorm:"type:varchar(45)" json:"ip_address"`
	Notes        string           `gorm:"type:text" json:"notes"`
}

func (e *LicenseEvent) BeforeSave(tx *gorm.DB) error {
	if strings.TrimSpace(e.FromValue) == "" {
		e.FromValue = "{}"
	}
	if strings.TrimSpace(e.ToValue) == "" {
		e.ToValue = "{}"
	}
	return nil
}

type LicenseEventType string

const (
	LicenseEventCreated     LicenseEventType = "created"
	LicenseEventActivated   LicenseEventType = "activated"
	LicenseEventRenewed     LicenseEventType = "renewed"
	LicenseEventUpgraded    LicenseEventType = "upgraded"
	LicenseEventTransferred LicenseEventType = "transferred"
	LicenseEventSuspended   LicenseEventType = "suspended"
	LicenseEventResumed     LicenseEventType = "resumed"
	LicenseEventRevoked     LicenseEventType = "revoked"
	LicenseEventExpired     LicenseEventType = "expired"
)

func (LicenseEvent) TableName() string {
	return "license_events"
}
