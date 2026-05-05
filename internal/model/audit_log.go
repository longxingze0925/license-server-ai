package model

// AuditLog 操作日志模型
type AuditLog struct {
	BaseModel
	TenantID     string `gorm:"type:char(36);index" json:"tenant_id"` // 所属租户
	UserID       string `gorm:"type:varchar(36);index" json:"user_id"`
	UserEmail    string `gorm:"type:varchar(100)" json:"user_email"`
	Action       string `gorm:"type:varchar(50);not null" json:"action"`
	Resource     string `gorm:"type:varchar(50);not null" json:"resource"`
	ResourceID   string `gorm:"type:varchar(36)" json:"resource_id"`
	Description  string `gorm:"type:varchar(500)" json:"description"`
	IPAddress    string `gorm:"type:varchar(45)" json:"ip_address"`
	UserAgent    string `gorm:"type:varchar(500)" json:"user_agent"`
	RequestBody  string `gorm:"type:text" json:"request_body"`
	ResponseCode int    `gorm:"type:int" json:"response_code"`
	Duration     int64  `gorm:"type:bigint" json:"duration"` // 毫秒
}

func (AuditLog) TableName() string {
	return "audit_logs"
}

// 操作类型常量
const (
	ActionCreate    = "create"
	ActionUpdate    = "update"
	ActionDelete    = "delete"
	ActionLogin     = "login"
	ActionLogout    = "logout"
	ActionExport    = "export"
	ActionRevoke    = "revoke"
	ActionReset     = "reset"
	ActionPublish   = "publish"
	ActionDeprecate = "deprecate"
	ActionRollback  = "rollback"
)

// 资源类型常量
const (
	ResourceUser         = "user"
	ResourceTeamMember   = "team_member"
	ResourceCustomer     = "customer"
	ResourceTenant       = "tenant"
	ResourceApp          = "application"
	ResourceLicense      = "license"
	ResourceSubscription = "subscription"
	ResourceDevice       = "device"
	ResourceScript       = "script"
	ResourceRelease      = "release"
	ResourceHotUpdate    = "hotupdate"
	ResourcePublishTask  = "publish_task"
	ResourceProviderCred = "provider_credential"
	ResourcePricingRule  = "pricing_rule"
	ResourceCredit       = "credit"
	ResourceInstruction  = "instruction"
)
