package model

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"license-server/internal/pkg/crypto"
)

// TeamMember 团队成员 - 管理后台用户
type TeamMember struct {
	ID        string         `gorm:"type:char(36);primaryKey" json:"id"`
	TenantID  string         `gorm:"type:char(36);index;not null" json:"tenant_id"`
	Email     string         `gorm:"type:varchar(100);uniqueIndex;not null" json:"email"`
	Password  string         `gorm:"type:varchar(255);not null" json:"-"`
	Name      string         `gorm:"type:varchar(50)" json:"name"`
	Phone     string         `gorm:"type:varchar(20)" json:"phone"`
	Avatar    string         `gorm:"type:varchar(500)" json:"avatar"`
	Role      TeamMemberRole `gorm:"type:varchar(20);not null;default:viewer" json:"role"`
	Status    MemberStatus   `gorm:"type:varchar(20);default:active" json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 安全相关
	LastLoginAt      *time.Time `json:"last_login_at"`
	LastLoginIP      string     `gorm:"type:varchar(45)" json:"last_login_ip"`
	TwoFactorEnabled bool       `gorm:"default:false" json:"two_factor_enabled"`
	TwoFactorSecret  string     `gorm:"type:varchar(100)" json:"-"`
	EmailVerified    bool       `gorm:"default:false" json:"email_verified"`

	// 邀请相关
	InvitedBy   *string    `gorm:"type:char(36)" json:"invited_by"`
	InvitedAt   *time.Time `json:"invited_at"`
	InviteToken string     `gorm:"type:varchar(100);index" json:"-"` // 邀请令牌

	// 关联
	Tenant *Tenant `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
}

// TeamMemberRole 团队成员角色
type TeamMemberRole string

const (
	RoleOwner     TeamMemberRole = "owner"     // 所有者：全部权限，可删除租户
	RoleAdmin     TeamMemberRole = "admin"     // 管理员：管理成员，管理所有资源
	RoleDeveloper TeamMemberRole = "developer" // 开发者：管理应用、授权、客户
	RoleViewer    TeamMemberRole = "viewer"    // 查看者：只读权限
)

// MemberStatus 成员状态
type MemberStatus string

const (
	MemberStatusActive   MemberStatus = "active"   // 正常
	MemberStatusInvited  MemberStatus = "invited"  // 已邀请待接受
	MemberStatusDisabled MemberStatus = "disabled" // 已禁用
)

func (TeamMember) TableName() string {
	return "team_members"
}

// BeforeCreate 创建前钩子
func (m *TeamMember) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// SetPassword 设置密码（加密）
func (m *TeamMember) SetPassword(password string) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	m.Password = string(hashedPassword)
	return nil
}

// SetPasswordWithPreHash 设置密码（支持客户端预哈希）
func (m *TeamMember) SetPasswordWithPreHash(password string, isPreHashed bool) error {
	hashedPassword, err := crypto.PreparePasswordForStorage(password, m.Email, isPreHashed)
	if err != nil {
		return err
	}
	m.Password = hashedPassword
	return nil
}

// CheckPassword 验证密码
func (m *TeamMember) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(m.Password), []byte(password))
	return err == nil
}

// CheckPasswordWithPreHash 验证密码（支持客户端预哈希）
func (m *TeamMember) CheckPasswordWithPreHash(password string, isPreHashed bool) bool {
	return crypto.CheckPassword(password, m.Password, m.Email, isPreHashed)
}

// HasPermission 检查是否有指定权限
func (m *TeamMember) HasPermission(permission string) bool {
	return RolePermissions[m.Role][permission]
}

// IsOwner 是否是所有者
func (m *TeamMember) IsOwner() bool {
	return m.Role == RoleOwner
}

// IsAdmin 是否是管理员（包括所有者）
func (m *TeamMember) IsAdmin() bool {
	return m.Role == RoleOwner || m.Role == RoleAdmin
}

// CanManageMembers 是否可以管理成员
func (m *TeamMember) CanManageMembers() bool {
	return m.Role == RoleOwner || m.Role == RoleAdmin
}

// CanManageResources 是否可以管理资源（应用、授权等）
func (m *TeamMember) CanManageResources() bool {
	return m.Role == RoleOwner || m.Role == RoleAdmin || m.Role == RoleDeveloper
}

// RolePermissions 角色权限映射
var RolePermissions = map[TeamMemberRole]map[string]bool{
	RoleOwner: {
		// 租户管理
		"tenant:read":   true,
		"tenant:update": true,
		"tenant:delete": true,
		// 团队成员
		"member:read":   true,
		"member:invite": true,
		"member:update": true,
		"member:delete": true,
		// 应用管理
		"app:read":   true,
		"app:create": true,
		"app:update": true,
		"app:delete": true,
		// 客户管理
		"customer:read":   true,
		"customer:create": true,
		"customer:update": true,
		"customer:delete": true,
		// 授权管理
		"license:read":   true,
		"license:create": true,
		"license:update": true,
		"license:delete": true,
		// 订阅管理
		"subscription:read":   true,
		"subscription:create": true,
		"subscription:update": true,
		"subscription:delete": true,
		// 设备管理
		"device:read":   true,
		"device:update": true,
		"device:delete": true,
		// 统计与导出
		"stats:read":    true,
		"export:read":   true,
		"audit:read":    true,
		"backup:read":   true,
		"backup:update": true,
		// AI 平台能力
		"proxy_credential:read":   true,
		"proxy_credential:create": true,
		"proxy_credential:update": true,
		"proxy_credential:delete": true,
		"pricing:read":            true,
		"pricing:create":          true,
		"pricing:update":          true,
		"pricing:delete":          true,
		"credit:read":             true,
		"credit:update":           true,
	},
	RoleAdmin: {
		// 租户管理
		"tenant:read":   true,
		"tenant:update": true,
		"tenant:delete": false,
		// 团队成员
		"member:read":   true,
		"member:invite": true,
		"member:update": true,
		"member:delete": true,
		// 应用管理
		"app:read":   true,
		"app:create": true,
		"app:update": true,
		"app:delete": true,
		// 客户管理
		"customer:read":   true,
		"customer:create": true,
		"customer:update": true,
		"customer:delete": true,
		// 授权管理
		"license:read":   true,
		"license:create": true,
		"license:update": true,
		"license:delete": true,
		// 订阅管理
		"subscription:read":   true,
		"subscription:create": true,
		"subscription:update": true,
		"subscription:delete": true,
		// 设备管理
		"device:read":   true,
		"device:update": true,
		"device:delete": true,
		// 统计与导出
		"stats:read":    true,
		"export:read":   true,
		"audit:read":    true,
		"backup:read":   true,
		"backup:update": true,
		// AI 平台能力
		"proxy_credential:read":   true,
		"proxy_credential:create": true,
		"proxy_credential:update": true,
		"proxy_credential:delete": true,
		"pricing:read":            true,
		"pricing:create":          true,
		"pricing:update":          true,
		"pricing:delete":          true,
		"credit:read":             true,
		"credit:update":           true,
	},
	RoleDeveloper: {
		// 租户管理
		"tenant:read":   true,
		"tenant:update": false,
		"tenant:delete": false,
		// 团队成员
		"member:read":   true,
		"member:invite": false,
		"member:update": false,
		"member:delete": false,
		// 应用管理
		"app:read":   true,
		"app:create": true,
		"app:update": true,
		"app:delete": false,
		// 客户管理
		"customer:read":   true,
		"customer:create": true,
		"customer:update": true,
		"customer:delete": false,
		// 授权管理
		"license:read":   true,
		"license:create": true,
		"license:update": true,
		"license:delete": true,
		// 订阅管理
		"subscription:read":   true,
		"subscription:create": true,
		"subscription:update": true,
		"subscription:delete": true,
		// 设备管理
		"device:read":   true,
		"device:update": true,
		"device:delete": true,
		// 统计与导出
		"stats:read":    true,
		"export:read":   true,
		"audit:read":    false,
		"backup:read":   false,
		"backup:update": false,
		// AI 平台能力
		"proxy_credential:read":   false,
		"proxy_credential:create": false,
		"proxy_credential:update": false,
		"proxy_credential:delete": false,
		"pricing:read":            false,
		"pricing:create":          false,
		"pricing:update":          false,
		"pricing:delete":          false,
		"credit:read":             false,
		"credit:update":           false,
	},
	RoleViewer: {
		// 租户管理
		"tenant:read":   true,
		"tenant:update": false,
		"tenant:delete": false,
		// 团队成员
		"member:read":   true,
		"member:invite": false,
		"member:update": false,
		"member:delete": false,
		// 应用管理
		"app:read":   true,
		"app:create": false,
		"app:update": false,
		"app:delete": false,
		// 客户管理
		"customer:read":   true,
		"customer:create": false,
		"customer:update": false,
		"customer:delete": false,
		// 授权管理
		"license:read":   true,
		"license:create": false,
		"license:update": false,
		"license:delete": false,
		// 订阅管理
		"subscription:read":   true,
		"subscription:create": false,
		"subscription:update": false,
		"subscription:delete": false,
		// 设备管理
		"device:read":   true,
		"device:update": false,
		"device:delete": false,
		// 统计与导出
		"stats:read":    true,
		"export:read":   false,
		"audit:read":    false,
		"backup:read":   false,
		"backup:update": false,
		// AI 平台能力
		"proxy_credential:read":   false,
		"proxy_credential:create": false,
		"proxy_credential:update": false,
		"proxy_credential:delete": false,
		"pricing:read":            false,
		"pricing:create":          false,
		"pricing:update":          false,
		"pricing:delete":          false,
		"credit:read":             false,
		"credit:update":           false,
	},
}

// TeamInvitation 团队邀请
type TeamInvitation struct {
	ID        string         `gorm:"type:char(36);primaryKey" json:"id"`
	TenantID  string         `gorm:"type:char(36);index;not null" json:"tenant_id"`
	Email     string         `gorm:"type:varchar(100);not null" json:"email"`
	Role      TeamMemberRole `gorm:"type:varchar(20);default:viewer" json:"role"`
	Token     string         `gorm:"type:varchar(100);uniqueIndex;not null" json:"-"`
	InvitedBy string         `gorm:"type:char(36);not null" json:"invited_by"`
	Status    InviteStatus   `gorm:"type:varchar(20);default:pending" json:"status"`
	ExpireAt  time.Time      `gorm:"not null" json:"expire_at"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联 - 不使用外键约束，避免迁移顺序问题
	Tenant  *Tenant     `gorm:"foreignKey:TenantID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"tenant,omitempty"`
	Inviter *TeamMember `gorm:"-" json:"inviter,omitempty"` // 手动加载，不创建外键
}

// InviteStatus 邀请状态
type InviteStatus string

const (
	InviteStatusPending  InviteStatus = "pending"  // 待接受
	InviteStatusAccepted InviteStatus = "accepted" // 已接受
	InviteStatusExpired  InviteStatus = "expired"  // 已过期
	InviteStatusRevoked  InviteStatus = "revoked"  // 已撤销
)

func (TeamInvitation) TableName() string {
	return "team_invitations"
}

// BeforeCreate 创建前钩子
func (i *TeamInvitation) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}

// IsExpired 是否已过期
func (i *TeamInvitation) IsExpired() bool {
	return time.Now().After(i.ExpireAt)
}
