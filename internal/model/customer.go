package model

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"license-server/internal/pkg/crypto"
)

// Customer 客户 - 软件最终用户
type Customer struct {
	ID        string         `gorm:"type:char(36);primaryKey" json:"id"`
	TenantID  string         `gorm:"type:char(36);index;not null;uniqueIndex:idx_customers_tenant_email" json:"tenant_id"`
	OwnerID   string         `gorm:"type:char(36);index" json:"owner_id"`                                            // 所属团队成员ID
	Email     string         `gorm:"type:varchar(100);not null;uniqueIndex:idx_customers_tenant_email" json:"email"` // 同一租户内唯一
	Password  string         `gorm:"type:varchar(255)" json:"-"`                                                     // 可为空（仅授权码模式不需要密码）
	Name      string         `gorm:"type:varchar(100)" json:"name"`
	Phone     string         `gorm:"type:varchar(20)" json:"phone"`
	Company   string         `gorm:"type:varchar(100)" json:"company"` // 客户所属公司
	Avatar    string         `gorm:"type:varchar(500)" json:"avatar"`
	Status    CustomerStatus `gorm:"type:varchar(20);default:active" json:"status"`
	Metadata  string         `gorm:"type:text" json:"metadata"` // 自定义JSON数据
	Remark    string         `gorm:"type:text" json:"remark"`   // 备注
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 登录追踪
	LastLoginAt *time.Time `json:"last_login_at"`
	LastLoginIP string     `gorm:"type:varchar(45)" json:"last_login_ip"`

	// 关联
	Tenant        *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Owner         *TeamMember    `gorm:"foreignKey:OwnerID" json:"owner,omitempty"` // 所属团队成员
	Licenses      []License      `gorm:"foreignKey:CustomerID" json:"licenses,omitempty"`
	Subscriptions []Subscription `gorm:"foreignKey:CustomerID" json:"subscriptions,omitempty"`
	Devices       []Device       `gorm:"foreignKey:CustomerID" json:"devices,omitempty"`
}

// CustomerStatus 客户状态
type CustomerStatus string

const (
	CustomerStatusActive   CustomerStatus = "active"   // 正常
	CustomerStatusDisabled CustomerStatus = "disabled" // 已禁用
	CustomerStatusBanned   CustomerStatus = "banned"   // 已封禁
	CustomerStatusPending  CustomerStatus = "pending"  // 待验证
)

func (Customer) TableName() string {
	return "customers"
}

// BeforeCreate 创建前钩子
func (c *Customer) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// SetPassword 设置密码（加密）
func (c *Customer) SetPassword(password string) error {
	if password == "" {
		return nil // 允许空密码（授权码模式）
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	c.Password = string(hashedPassword)
	return nil
}

// SetPasswordWithPreHash 设置密码（支持客户端预哈希）
func (c *Customer) SetPasswordWithPreHash(password string, isPreHashed bool) error {
	if password == "" {
		return nil
	}
	hashedPassword, err := crypto.PreparePasswordForStorage(password, c.Email, isPreHashed)
	if err != nil {
		return err
	}
	c.Password = hashedPassword
	return nil
}

// CheckPassword 验证密码
func (c *Customer) CheckPassword(password string) bool {
	if c.Password == "" {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(c.Password), []byte(password))
	return err == nil
}

// CheckPasswordWithPreHash 验证密码（支持客户端预哈希）
func (c *Customer) CheckPasswordWithPreHash(password string, isPreHashed bool) bool {
	if c.Password == "" {
		return false
	}
	return crypto.CheckPassword(password, c.Password, c.Email, isPreHashed)
}

// HasPassword 是否设置了密码
func (c *Customer) HasPassword() bool {
	return c.Password != ""
}

// IsActive 是否处于活跃状态
func (c *Customer) IsActive() bool {
	return c.Status == CustomerStatusActive
}
