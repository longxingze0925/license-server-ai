package model

import "time"

// AuthMode 授权模式
type AuthMode string

const (
	AuthModeLicense      AuthMode = "license"      // 仅授权码模式
	AuthModeSubscription AuthMode = "subscription" // 仅订阅模式
	AuthModeBoth         AuthMode = "both"         // 两者都支持
)

// Application 应用模型
type Application struct {
	BaseModel
	TenantID    string `gorm:"type:char(36);index;not null" json:"tenant_id"` // 所属租户
	Name        string `gorm:"type:varchar(100);not null" json:"name"`
	Slug        string `gorm:"type:varchar(50);index" json:"slug"` // URL友好标识
	AppKey      string `gorm:"type:varchar(64);uniqueIndex;not null" json:"app_key"`
	AppSecret   string `gorm:"type:varchar(128);not null" json:"-"`
	PublicKey   string `gorm:"type:text;not null" json:"public_key"`
	PrivateKey  string `gorm:"type:text;not null" json:"-"`
	Description string `gorm:"type:text" json:"description"`
	Icon        string `gorm:"type:varchar(500)" json:"icon"`
	// 授权配置
	AuthMode          AuthMode  `gorm:"type:varchar(20);default:both" json:"auth_mode"` // 授权模式
	HeartbeatInterval int       `gorm:"default:3600" json:"heartbeat_interval"`         // 心跳间隔(秒)
	OfflineTolerance  int       `gorm:"default:86400" json:"offline_tolerance"`         // 离线容忍(秒)
	MaxDevicesDefault int       `gorm:"default:1" json:"max_devices_default"`           // 默认设备数
	GracePeriodDays   int       `gorm:"default:3" json:"grace_period_days"`             // 宽限期(天)
	Features          string    `gorm:"type:json" json:"features"`                      // 可用功能列表 JSON数组
	Status            AppStatus `gorm:"type:varchar(20);default:active" json:"status"`
	// 关联
	Tenant        *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Releases      []AppRelease   `gorm:"foreignKey:AppID" json:"releases,omitempty"`
	Scripts       []Script       `gorm:"foreignKey:AppID" json:"scripts,omitempty"`
	Licenses      []License      `gorm:"foreignKey:AppID" json:"licenses,omitempty"`
	Subscriptions []Subscription `gorm:"foreignKey:AppID" json:"subscriptions,omitempty"`
}

type AppStatus string

const (
	AppStatusActive   AppStatus = "active"
	AppStatusDisabled AppStatus = "disabled"
)

func (Application) TableName() string {
	return "applications"
}

// AppRelease 应用版本发布
type AppRelease struct {
	BaseModel
	AppID             string        `gorm:"type:varchar(36);not null" json:"app_id"`
	Version           string        `gorm:"type:varchar(20);not null" json:"version"`
	VersionCode       int           `gorm:"not null" json:"version_code"`
	DownloadURL       string        `gorm:"type:varchar(500)" json:"download_url"`
	Changelog         string        `gorm:"type:text" json:"changelog"`
	FileSize          int64         `json:"file_size"`
	FileHash          string        `gorm:"type:varchar(64)" json:"file_hash"`
	FileSignature     string        `gorm:"type:text" json:"file_signature"`
	ForceUpdate       bool          `gorm:"default:false" json:"force_update"`
	RolloutPercentage int           `gorm:"default:100" json:"rollout_percentage"` // 灰度比例
	Status            ReleaseStatus `gorm:"type:varchar(20);default:draft" json:"status"`
	PublishedAt       *time.Time    `json:"published_at"`
	// 关联
	Application *Application `gorm:"foreignKey:AppID" json:"application,omitempty"`
}

type ReleaseStatus string

const (
	ReleaseStatusDraft      ReleaseStatus = "draft"
	ReleaseStatusPublished  ReleaseStatus = "published"
	ReleaseStatusDeprecated ReleaseStatus = "deprecated"
)

func (AppRelease) TableName() string {
	return "app_releases"
}

// Script 脚本模型
type Script struct {
	BaseModel
	AppID             string       `gorm:"type:varchar(36);not null" json:"app_id"`
	Filename          string       `gorm:"type:varchar(255);not null" json:"filename"`
	Version           string       `gorm:"type:varchar(20);not null" json:"version"`
	Content           []byte       `gorm:"type:longblob;not null" json:"-"`
	ContentHash       string       `gorm:"type:varchar(64);not null" json:"content_hash"`
	FileSize          int64        `json:"file_size"`
	IsEncrypted       bool         `gorm:"default:true" json:"is_encrypted"`
	RolloutPercentage int          `gorm:"default:100" json:"rollout_percentage"`
	Status            ScriptStatus `gorm:"type:varchar(20);default:active" json:"status"`
	// 关联
	Application *Application `gorm:"foreignKey:AppID" json:"application,omitempty"`
}

// HotUpdate 热更新包模型
type HotUpdate struct {
	BaseModel
	AppID           string          `gorm:"type:varchar(36);not null;index" json:"app_id"`
	FromVersion     string          `gorm:"type:varchar(20);not null" json:"from_version"`                // 源版本（* 表示任意版本）
	ToVersion       string          `gorm:"type:varchar(20);not null" json:"to_version"`                  // 目标版本
	VersionCode     int             `gorm:"default:0" json:"version_code"`                                // 版本代码
	PatchType       HotUpdateType   `gorm:"type:varchar(20);default:full" json:"patch_type"`              // 更新类型
	UpdateMode      string          `gorm:"type:varchar(20);default:mixed" json:"update_mode"`            // 更新模式: exe/script/resource/mixed
	PatchURL        string          `gorm:"type:varchar(500)" json:"patch_url"`                           // 补丁下载地址
	PatchSize       int64           `json:"patch_size"`                                                   // 补丁大小
	PatchHash       string          `gorm:"type:varchar(64)" json:"patch_hash"`                           // 补丁哈希
	PatchSignature  string          `gorm:"type:text" json:"patch_signature"`                             // 补丁签名
	FullURL         string          `gorm:"type:varchar(500)" json:"full_url"`                            // 完整包下载地址
	FullSize        int64           `json:"full_size"`                                                    // 完整包大小
	FullHash        string          `gorm:"type:varchar(64)" json:"full_hash"`                            // 完整包哈希
	FullSignature   string          `gorm:"type:text" json:"full_signature"`                              // 完整包签名
	Manifest        string          `gorm:"type:text" json:"manifest"`                                    // 更新清单 JSON
	Changelog       string          `gorm:"type:text" json:"changelog"`                                   // 更新日志
	ForceUpdate     bool            `gorm:"default:false" json:"force_update"`                            // 是否强制更新
	RestartRequired bool            `gorm:"default:false" json:"restart_required"`                        // 是否需要重启
	MinAppVersion   string          `gorm:"type:varchar(20)" json:"min_app_version"`                      // 最低支持版本
	RolloutPercent  int             `gorm:"column:rollout_percent;default:100" json:"rollout_percentage"` // 灰度比例
	Status          HotUpdateStatus `gorm:"type:varchar(20);default:draft" json:"status"`                 // 状态
	PublishedAt     *time.Time      `json:"published_at"`                                                 // 发布时间
	DownloadCount   int64           `gorm:"default:0" json:"download_count"`                              // 下载次数
	SuccessCount    int64           `gorm:"default:0" json:"success_count"`                               // 成功次数
	FailCount       int64           `gorm:"default:0" json:"fail_count"`                                  // 失败次数
	// 关联
	Application *Application `gorm:"foreignKey:AppID" json:"application,omitempty"`
}

type HotUpdateType string

const (
	HotUpdateTypeFull  HotUpdateType = "full"  // 全量更新
	HotUpdateTypePatch HotUpdateType = "patch" // 增量更新
)

type HotUpdateStatus string

const (
	HotUpdateStatusDraft      HotUpdateStatus = "draft"      // 草稿
	HotUpdateStatusPublished  HotUpdateStatus = "published"  // 已发布
	HotUpdateStatusDeprecated HotUpdateStatus = "deprecated" // 已废弃
	HotUpdateStatusRollback   HotUpdateStatus = "rollback"   // 已回滚
)

func (HotUpdate) TableName() string {
	return "hot_updates"
}

// HotUpdateLog 热更新日志
type HotUpdateLog struct {
	BaseModel
	HotUpdateID  string             `gorm:"type:varchar(36);not null;index" json:"hot_update_id"`
	DeviceID     string             `gorm:"type:varchar(36);index" json:"device_id"`
	MachineID    string             `gorm:"type:varchar(64);index" json:"machine_id"`
	FromVersion  string             `gorm:"type:varchar(20)" json:"from_version"`
	ToVersion    string             `gorm:"type:varchar(20)" json:"to_version"`
	Status       HotUpdateLogStatus `gorm:"type:varchar(20)" json:"status"`
	ErrorMessage string             `gorm:"type:text" json:"error_message"`
	IPAddress    string             `gorm:"type:varchar(45)" json:"ip_address"`
	StartedAt    *time.Time         `json:"started_at"`
	CompletedAt  *time.Time         `json:"completed_at"`
	// 关联
	HotUpdate *HotUpdate `gorm:"foreignKey:HotUpdateID" json:"hot_update,omitempty"`
}

type HotUpdateLogStatus string

const (
	HotUpdateLogStatusPending     HotUpdateLogStatus = "pending"     // 待更新
	HotUpdateLogStatusDownloading HotUpdateLogStatus = "downloading" // 下载中
	HotUpdateLogStatusInstalling  HotUpdateLogStatus = "installing"  // 安装中
	HotUpdateLogStatusSuccess     HotUpdateLogStatus = "success"     // 成功
	HotUpdateLogStatusFailed      HotUpdateLogStatus = "failed"      // 失败
	HotUpdateLogStatusRollback    HotUpdateLogStatus = "rollback"    // 已回滚
)

func (HotUpdateLog) TableName() string {
	return "hot_update_logs"
}

type ScriptStatus string

const (
	ScriptStatusActive     ScriptStatus = "active"
	ScriptStatusDeprecated ScriptStatus = "deprecated"
)

func (Script) TableName() string {
	return "scripts"
}
