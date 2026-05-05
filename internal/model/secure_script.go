package model

import "time"

// SecureScript 安全脚本模型
type SecureScript struct {
	BaseModel
	AppID       string           `gorm:"type:varchar(36);not null;index" json:"app_id"`
	Name        string           `gorm:"type:varchar(100);not null" json:"name"`
	Description string           `gorm:"type:text" json:"description"`
	Version     string           `gorm:"type:varchar(20);not null" json:"version"`
	ScriptType  SecureScriptType `gorm:"type:varchar(20);default:python" json:"script_type"` // python/lua/instruction
	EntryPoint  string           `gorm:"type:varchar(100)" json:"entry_point"`               // 入口函数名

	// 加密存储
	EncryptedContent []byte `gorm:"type:longblob;not null" json:"-"`       // AES加密后的脚本内容
	ContentHash      string `gorm:"type:varchar(64);not null" json:"hash"` // 原始内容SHA256
	StorageKey       string `gorm:"type:varchar(500);not null" json:"-"`   // 存储密钥(RSA加密)
	OriginalSize     int64  `json:"original_size"`                         // 原始大小
	EncryptedSize    int64  `json:"encrypted_size"`                        // 加密后大小

	// 执行配置
	Timeout     int    `gorm:"default:300" json:"timeout"`      // 执行超时(秒)
	MemoryLimit int    `gorm:"default:512" json:"memory_limit"` // 内存限制(MB)
	Parameters  string `gorm:"type:json" json:"parameters"`     // 参数定义 JSON

	// 权限控制
	RequiredFeatures string `gorm:"type:json" json:"required_features"` // 需要的功能权限 JSON数组
	AllowedDevices   string `gorm:"type:json" json:"allowed_devices"`   // 允许的设备ID JSON数组 (空=全部)

	// 灰度与状态
	RolloutPercentage int                `gorm:"default:100" json:"rollout_percentage"`
	Status            SecureScriptStatus `gorm:"type:varchar(20);default:draft" json:"status"`
	PublishedAt       *time.Time         `json:"published_at"`
	ExpiresAt         *time.Time         `json:"expires_at"` // 脚本过期时间 (可选)

	// 统计
	DeliveryCount int64 `gorm:"default:0" json:"delivery_count"` // 下发次数
	ExecuteCount  int64 `gorm:"default:0" json:"execute_count"`  // 执行次数
	SuccessCount  int64 `gorm:"default:0" json:"success_count"`  // 成功次数
	FailCount     int64 `gorm:"default:0" json:"fail_count"`     // 失败次数

	// 关联
	Application *Application `gorm:"foreignKey:AppID" json:"application,omitempty"`
}

type SecureScriptType string

const (
	SecureScriptTypePython      SecureScriptType = "python"
	SecureScriptTypeLua         SecureScriptType = "lua"
	SecureScriptTypeInstruction SecureScriptType = "instruction"
)

type SecureScriptStatus string

const (
	SecureScriptStatusDraft      SecureScriptStatus = "draft"
	SecureScriptStatusPublished  SecureScriptStatus = "published"
	SecureScriptStatusDeprecated SecureScriptStatus = "deprecated"
)

func (SecureScript) TableName() string {
	return "secure_scripts"
}

// ScriptDelivery 脚本下发记录
type ScriptDelivery struct {
	BaseModel
	ScriptID  string `gorm:"type:varchar(36);not null;index" json:"script_id"`
	DeviceID  string `gorm:"type:varchar(36);index" json:"device_id"`
	MachineID string `gorm:"type:varchar(64);not null;index" json:"machine_id"`
	LicenseID string `gorm:"type:varchar(36);index" json:"license_id"`

	// 下发信息
	DeliveryKey string    `gorm:"type:varchar(64);not null" json:"-"` // 本次下发的密钥提示
	ExpiresAt   time.Time `json:"expires_at"`                         // 本次下发过期时间
	IPAddress   string    `gorm:"type:varchar(45)" json:"ip_address"`

	// 执行状态
	Status       ScriptDeliveryStatus `gorm:"type:varchar(20);default:pending" json:"status"`
	ExecutedAt   *time.Time           `json:"executed_at"`
	CompletedAt  *time.Time           `json:"completed_at"`
	Duration     int                  `json:"duration"`                // 执行耗时(毫秒)
	Result       string               `gorm:"type:text" json:"result"` // 执行结果摘要
	ErrorMessage string               `gorm:"type:text" json:"error_message"`

	// 关联
	SecureScript *SecureScript `gorm:"foreignKey:ScriptID" json:"script,omitempty"`
}

type ScriptDeliveryStatus string

const (
	ScriptDeliveryStatusPending   ScriptDeliveryStatus = "pending"   // 已下发待执行
	ScriptDeliveryStatusExecuting ScriptDeliveryStatus = "executing" // 执行中
	ScriptDeliveryStatusSuccess   ScriptDeliveryStatus = "success"   // 执行成功
	ScriptDeliveryStatusFailed    ScriptDeliveryStatus = "failed"    // 执行失败
	ScriptDeliveryStatusExpired   ScriptDeliveryStatus = "expired"   // 已过期
)

func (ScriptDelivery) TableName() string {
	return "script_deliveries"
}

// RealtimeInstruction 实时指令
type RealtimeInstruction struct {
	BaseModel
	AppID    string `gorm:"type:varchar(36);not null;index" json:"app_id"`
	DeviceID string `gorm:"type:varchar(36);index" json:"device_id"` // 目标设备 (空=广播)

	// 指令内容
	Type     InstructionType `gorm:"type:varchar(30);not null" json:"type"`
	Payload  string          `gorm:"type:json;not null" json:"payload"` // JSON 参数
	Priority int             `gorm:"default:0" json:"priority"`         // 优先级 (越大越优先)

	// 安全
	Signature string `gorm:"type:varchar(500);not null" json:"signature"` // RSA签名
	Timestamp int64  `gorm:"not null" json:"timestamp"`                   // 时间戳
	Nonce     string `gorm:"type:varchar(32);not null" json:"nonce"`      // 随机数

	// 状态
	Status    InstructionStatus `gorm:"type:varchar(20);default:pending" json:"status"`
	ExpiresAt time.Time         `json:"expires_at"` // 过期时间
	SentAt    *time.Time        `json:"sent_at"`
	AckedAt   *time.Time        `json:"acked_at"` // 客户端确认时间
	Result    string            `gorm:"type:text" json:"result"`

	// 关联
	Application *Application `gorm:"foreignKey:AppID" json:"application,omitempty"`
}

type InstructionType string

const (
	// 基础操作
	InstructionTypeClick       InstructionType = "click"
	InstructionTypeDoubleClick InstructionType = "double_click"
	InstructionTypeRightClick  InstructionType = "right_click"
	InstructionTypeInput       InstructionType = "input"
	InstructionTypeKeyPress    InstructionType = "key_press"
	InstructionTypeScroll      InstructionType = "scroll"

	// 屏幕操作
	InstructionTypeScreenshot InstructionType = "screenshot"
	InstructionTypeFindImage  InstructionType = "find_image"
	InstructionTypeOCR        InstructionType = "ocr"

	// 流程控制
	InstructionTypeWait      InstructionType = "wait"
	InstructionTypeCondition InstructionType = "condition"

	// 脚本执行
	InstructionTypeExecScript InstructionType = "exec_script"

	// 系统
	InstructionTypeGetStatus InstructionType = "get_status"
	InstructionTypeRestart   InstructionType = "restart"
	InstructionTypeShutdown  InstructionType = "shutdown"

	// 自定义
	InstructionTypeCustom InstructionType = "custom"
)

type InstructionStatus string

const (
	InstructionStatusPending  InstructionStatus = "pending"  // 待发送
	InstructionStatusSent     InstructionStatus = "sent"     // 已发送
	InstructionStatusAcked    InstructionStatus = "acked"    // 已确认
	InstructionStatusExecuted InstructionStatus = "executed" // 已执行
	InstructionStatusFailed   InstructionStatus = "failed"   // 失败
	InstructionStatusExpired  InstructionStatus = "expired"  // 已过期
)

func (RealtimeInstruction) TableName() string {
	return "realtime_instructions"
}

// RealtimeInstructionResult 实时指令按设备执行结果
type RealtimeInstructionResult struct {
	BaseModel
	InstructionID string            `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_instruction_device" json:"instruction_id"`
	AppID         string            `gorm:"type:varchar(36);not null;index" json:"app_id"`
	DeviceID      string            `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_instruction_device" json:"device_id"`
	MachineID     string            `gorm:"type:varchar(128);not null;index" json:"machine_id"`
	Status        InstructionStatus `gorm:"type:varchar(20);not null" json:"status"`
	Result        string            `gorm:"type:text" json:"result"`
	AckedAt       *time.Time        `json:"acked_at"`

	Instruction *RealtimeInstruction `gorm:"foreignKey:InstructionID" json:"instruction,omitempty"`
}

func (RealtimeInstructionResult) TableName() string {
	return "realtime_instruction_results"
}

// DeviceConnection 设备连接状态 (用于 WebSocket)
type DeviceConnection struct {
	BaseModel
	AppID       string    `gorm:"type:varchar(36);not null;index" json:"app_id"`
	DeviceID    string    `gorm:"type:varchar(36);not null;index" json:"device_id"`
	MachineID   string    `gorm:"type:varchar(64);not null;index" json:"machine_id"`
	SessionID   string    `gorm:"type:varchar(64);not null;uniqueIndex" json:"session_id"`
	IPAddress   string    `gorm:"type:varchar(45)" json:"ip_address"`
	UserAgent   string    `gorm:"type:varchar(500)" json:"user_agent"`
	ConnectedAt time.Time `json:"connected_at"`
	LastPingAt  time.Time `json:"last_ping_at"`
	Status      string    `gorm:"type:varchar(20);default:connected" json:"status"` // connected/disconnected
}

func (DeviceConnection) TableName() string {
	return "device_connections"
}
