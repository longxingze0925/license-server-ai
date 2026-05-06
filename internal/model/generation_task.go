package model

import "time"

// GenerationStatus 生成任务状态
type GenerationStatus int8

const (
	GenerationPending   GenerationStatus = 0
	GenerationRunning   GenerationStatus = 1
	GenerationSucceeded GenerationStatus = 2
	GenerationFailed    GenerationStatus = 3
	// 不支持中途取消，预留 4 = canceled 仅用于异常清理
	GenerationCanceled GenerationStatus = 4
)

type GenerationRefundStatus string

const (
	GenerationRefundNone    GenerationRefundStatus = "none"
	GenerationRefunded      GenerationRefundStatus = "refunded"
	GenerationRefundFailed  GenerationRefundStatus = "refund_failed"
	GenerationRefundSkipped GenerationRefundStatus = "skipped"
)

// GenerationTask 服务端权威任务记录
type GenerationTask struct {
	BaseModel
	UserID         string                 `gorm:"type:varchar(36);not null;index:idx_user_status" json:"user_id"`
	TenantID       string                 `gorm:"type:varchar(36);index" json:"tenant_id,omitempty"`
	AppID          string                 `gorm:"type:varchar(36);not null" json:"app_id"`
	Provider       ProviderKind           `gorm:"type:varchar(16);not null" json:"provider"`
	Mode           string                 `gorm:"type:varchar(32);not null" json:"mode"`
	CredentialID   string                 `gorm:"type:varchar(36)" json:"credential_id,omitempty"`
	UpstreamTaskID string                 `gorm:"type:varchar(128);index:idx_upstream" json:"upstream_task_id,omitempty"`
	Status         GenerationStatus       `gorm:"not null;index:idx_user_status" json:"status"`
	Progress       float32                `gorm:"not null;default:0" json:"progress"`
	RuleID         int64                  `json:"rule_id,omitempty"`
	Cost           int                    `gorm:"not null;default:0" json:"cost"` // 扣点数（成功保留，失败已退）
	RequestJSON    string                 `gorm:"type:mediumtext;not null" json:"request_json"`
	ResultJSON     string                 `gorm:"type:mediumtext" json:"result_json,omitempty"`
	ErrorJSON      string                 `gorm:"type:text" json:"error_json,omitempty"`
	NextPollAt     *time.Time             `gorm:"index" json:"next_poll_at,omitempty"`
	PollInterval   int                    `gorm:"not null;default:1" json:"poll_interval_seconds"`
	UpstreamStatus string                 `gorm:"type:varchar(64)" json:"upstream_status,omitempty"`
	UpstreamError  string                 `gorm:"type:text" json:"upstream_error,omitempty"`
	RefundStatus   GenerationRefundStatus `gorm:"type:varchar(20);not null;default:'none'" json:"refund_status"`
	RefundAmount   int64                  `gorm:"not null;default:0" json:"refund_amount"`
	RefundedAt     *time.Time             `json:"refunded_at,omitempty"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
}

func (GenerationTask) TableName() string {
	return "generation_tasks"
}

// GenerationFileKind 生成文件类型
type GenerationFileKind string

const (
	FileKindVideo     GenerationFileKind = "video"
	FileKindImage     GenerationFileKind = "image"
	FileKindThumbnail GenerationFileKind = "thumbnail"
)

// GenerationFile 生成结果文件元数据（实际文件存本机磁盘 storage/generations/...）
//
// 保留策略：created_at + 15 天 → 自动清理（cleanup job 扫 expires_at < NOW()）。
// 无收藏机制；用户长期保留只能在客户端"另存为本地"。
type GenerationFile struct {
	BaseModel
	TaskID     string             `gorm:"type:varchar(36);not null;index:idx_task" json:"task_id"`
	UserID     string             `gorm:"type:varchar(36);not null;index:idx_user_created" json:"user_id"`
	Kind       GenerationFileKind `gorm:"type:varchar(16);not null" json:"kind"`
	Storage    string             `gorm:"type:varchar(16);not null;default:'local'" json:"storage"` // local|oss|s3
	Path       string             `gorm:"type:varchar(512);not null" json:"path"`                   // 相对 storage_root
	SizeBytes  int64              `gorm:"not null" json:"size_bytes"`
	MimeType   string             `gorm:"type:varchar(64);not null" json:"mime_type"`
	DurationMs int                `json:"duration_ms,omitempty"`
	Width      int                `json:"width,omitempty"`
	Height     int                `json:"height,omitempty"`
	ExpiresAt  time.Time          `gorm:"not null;index:idx_expires" json:"expires_at"`
}

func (GenerationFile) TableName() string {
	return "generation_files"
}
