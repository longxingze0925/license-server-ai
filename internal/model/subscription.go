package model

import "time"

// Subscription 订阅模型（账号密码模式）
type Subscription struct {
	BaseModel
	TenantID     string             `gorm:"type:char(36);index;not null" json:"tenant_id"`   // 所属租户
	CustomerID   string             `gorm:"type:char(36);index;not null" json:"customer_id"` // 关联客户
	ClientUserID *string            `gorm:"type:char(36);index" json:"client_user_id"`       // 历史字段，当前使用 CustomerID
	AppID        string             `gorm:"type:varchar(36);not null;index" json:"app_id"`
	PlanType     PlanType           `gorm:"type:varchar(20);default:free" json:"plan_type"`
	MaxDevices   int                `gorm:"default:1" json:"max_devices"`
	UnbindLimit  int                `gorm:"default:5;not null" json:"unbind_limit"` // 客户端终身可解绑总次数
	UnbindUsed   int                `gorm:"default:0;not null" json:"unbind_used"`  // 客户端已解绑次数
	Features     string             `gorm:"type:json" json:"features"`
	Status       SubscriptionStatus `gorm:"type:varchar(20);default:active" json:"status"`
	StartAt      *time.Time         `json:"start_at"`
	ExpireAt     *time.Time         `json:"expire_at"`
	CancelledAt  *time.Time         `json:"cancelled_at"`
	AutoRenew    bool               `gorm:"default:false" json:"auto_renew"`
	Notes        string             `gorm:"type:text" json:"notes"`
	// 关联
	Tenant      *Tenant      `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Customer    *Customer    `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Application *Application `gorm:"foreignKey:AppID" json:"application,omitempty"`
	Devices     []Device     `gorm:"foreignKey:SubscriptionID" json:"devices,omitempty"`
}

// PlanType 套餐类型
type PlanType string

const (
	PlanTypeFree       PlanType = "free"       // 免费版
	PlanTypeBasic      PlanType = "basic"      // 基础版
	PlanTypePro        PlanType = "pro"        // 专业版
	PlanTypeEnterprise PlanType = "enterprise" // 企业版
)

// SubscriptionStatus 订阅状态
type SubscriptionStatus string

const (
	SubscriptionStatusActive    SubscriptionStatus = "active"    // 有效
	SubscriptionStatusExpired   SubscriptionStatus = "expired"   // 已过期
	SubscriptionStatusCancelled SubscriptionStatus = "cancelled" // 已取消
	SubscriptionStatusSuspended SubscriptionStatus = "suspended" // 已暂停
)

func (Subscription) TableName() string {
	return "subscriptions"
}

// IsValid 检查订阅是否有效
func (s *Subscription) IsValid() bool {
	if s.Status != SubscriptionStatusActive {
		return false
	}
	if s.ExpireAt != nil && time.Now().After(*s.ExpireAt) {
		return false
	}
	return true
}

// RemainingDays 剩余天数
func (s *Subscription) RemainingDays() int {
	if s.ExpireAt == nil {
		return -1 // 永久
	}
	// 计算从现在到过期时间的剩余时长
	now := time.Now()
	if s.ExpireAt.Before(now) {
		return 0
	}
	// 使用日期差值计算，避免浮点数精度问题
	// 将时间归零到当天0点，然后计算天数差
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	expireDate := time.Date(s.ExpireAt.Year(), s.ExpireAt.Month(), s.ExpireAt.Day(), 0, 0, 0, 0, s.ExpireAt.Location())
	days := int(expireDate.Sub(nowDate).Hours() / 24)
	// 如果过期时间在今天之后，至少返回1天
	if days == 0 && s.ExpireAt.After(now) {
		days = 1
	}
	return days
}

// RemainingClientUnbindCount 客户端解绑剩余次数
func (s *Subscription) RemainingClientUnbindCount() int {
	remaining := s.UnbindLimit - s.UnbindUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CanClientUnbind 是否允许客户端继续解绑
func (s *Subscription) CanClientUnbind() bool {
	return s.UnbindUsed < s.UnbindLimit
}
