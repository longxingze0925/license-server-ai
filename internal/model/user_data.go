package model

import (
	"time"
)

// ==================== 用户配置 ====================

// UserConfig 用户配置
type UserConfig struct {
	BaseModel
	UserID      string `gorm:"type:varchar(36);not null;uniqueIndex:idx_user_config_key"`
	AppID       string `gorm:"type:varchar(36);not null;uniqueIndex:idx_user_config_key"`
	ConfigKey   string `gorm:"type:varchar(100);not null;uniqueIndex:idx_user_config_key"`
	ConfigValue string `gorm:"type:text"`
	IsEncrypted bool   `gorm:"default:false"`
	Version     int64  `gorm:"default:1"`
	IsDeleted   bool   `gorm:"default:false;index"`
}

func (UserConfig) TableName() string {
	return "user_configs"
}

// ==================== 用户工作流 ====================

// UserWorkflow 用户工作流
type UserWorkflow struct {
	BaseModel
	UserID       string     `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_workflow_key"`
	AppID        string     `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_workflow_key"`
	WorkflowID   string     `gorm:"type:varchar(50);not null;uniqueIndex:idx_user_workflow_key"`
	WorkflowName string     `gorm:"type:varchar(100)"`
	Description  string     `gorm:"type:text"`
	Steps        string     `gorm:"type:json"`                        // 步骤配置 JSON
	Status       string     `gorm:"type:varchar(20);default:pending"` // pending/running/completed/failed
	CurrentStep  int        `gorm:"default:0"`
	CreateTime   time.Time  `json:"create_time"`
	StartTime    *time.Time `json:"start_time"`
	EndTime      *time.Time `json:"end_time"`
	Version      int64      `gorm:"default:1"`
	IsDeleted    bool       `gorm:"default:false;index"`
}

func (UserWorkflow) TableName() string {
	return "user_workflows"
}

// ==================== 用户批量任务 ====================

// UserBatchTask 用户批量任务
type UserBatchTask struct {
	BaseModel
	UserID          string     `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_batch_task_key"`
	AppID           string     `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_batch_task_key"`
	TaskID          string     `gorm:"type:varchar(50);not null;uniqueIndex:idx_user_batch_task_key"`
	TaskName        string     `gorm:"type:varchar(100)"`
	Description     string     `gorm:"type:text"`
	ScriptPath      string     `gorm:"type:varchar(255)"`
	ScriptType      string     `gorm:"type:varchar(50)"`
	Params          string     `gorm:"type:json"`        // 任务参数
	Environments    string     `gorm:"type:longtext"`    // 环境配置和执行结果 (可能很大)
	EnvConfig       string     `gorm:"type:json"`        // 环境特定配置 (如 envCommentConfig)
	Status          string     `gorm:"type:varchar(20)"` // pending/running/completed/failed
	Concurrency     int        `gorm:"default:1"`
	TotalCount      int        `gorm:"default:0"`
	CompletedCount  int        `gorm:"default:0"`
	FailedCount     int        `gorm:"default:0"`
	CurrentIndex    int        `gorm:"default:0"`
	CloseOnComplete bool       `gorm:"default:false"`
	CreateTime      time.Time  `json:"create_time"`
	StartTime       *time.Time `json:"start_time"`
	EndTime         *time.Time `json:"end_time"`
	Version         int64      `gorm:"default:1"`
	IsDeleted       bool       `gorm:"default:false;index"`
}

func (UserBatchTask) TableName() string {
	return "user_batch_tasks"
}

// ==================== 用户素材 ====================

// UserMaterial 用户素材
type UserMaterial struct {
	BaseModel
	UserID      string     `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_material_key"`
	AppID       string     `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_material_key"`
	MaterialID  int64      `gorm:"not null;uniqueIndex:idx_user_material_key"`
	FileName    string     `gorm:"type:varchar(255)"`
	FileType    string     `gorm:"type:varchar(20)"` // 图片/视频
	Caption     string     `gorm:"type:text"`
	GroupName   string     `gorm:"type:varchar(50);index"`
	Status      string     `gorm:"type:varchar(20);default:未使用"` // 未使用/处理中/已使用
	LocalPath   string     `gorm:"type:varchar(500)"`            // 本地路径
	CloudFileID string     `gorm:"type:varchar(36)"`             // 云端文件ID (可选)
	FileSize    int64      `gorm:"default:0"`
	FileHash    string     `gorm:"type:varchar(64)"`
	CreatedAt   time.Time  `json:"created_at"`
	UsedAt      *time.Time `json:"used_at"`
	Version     int64      `gorm:"default:1"`
	IsDeleted   bool       `gorm:"default:false;index"`
}

func (UserMaterial) TableName() string {
	return "user_materials"
}

// ==================== 用户帖子数据 ====================

// UserPost 用户采集的帖子数据
type UserPost struct {
	BaseModel
	UserID       string     `gorm:"type:varchar(36);not null;index"`
	AppID        string     `gorm:"type:varchar(36);not null;index"`
	PostType     string     `gorm:"type:varchar(20);not null;index"` // hashtag_posts/location_posts/user_posts
	GroupName    string     `gorm:"type:varchar(50);not null;index"`
	PostLink     string     `gorm:"type:varchar(500);not null"`
	PostID       string     `gorm:"type:varchar(100);index"`
	Shortcode    string     `gorm:"type:varchar(50);index"`
	Username     string     `gorm:"type:varchar(100);index"`
	FullName     string     `gorm:"type:varchar(200)"`
	Caption      string     `gorm:"type:text"`
	MediaType    string     `gorm:"type:varchar(20)"` // image/video/carousel
	LikeCount    int        `gorm:"default:0"`
	CommentCount int        `gorm:"default:0"`
	Timestamp    *time.Time `json:"timestamp"`
	Status       string     `gorm:"type:varchar(20);default:unused"` // unused/used
	CollectedAt  time.Time  `json:"collected_at"`
	UsedAt       *time.Time `json:"used_at"`
	Version      int64      `gorm:"default:1"`
	IsDeleted    bool       `gorm:"default:false;index"`
}

func (UserPost) TableName() string {
	return "user_posts"
}

// ==================== 用户评论数据 ====================

// UserComment 用户采集的评论数据
type UserComment struct {
	BaseModel
	UserID      string `gorm:"type:varchar(36);not null;index"`
	AppID       string `gorm:"type:varchar(36);not null;index"`
	GroupName   string `gorm:"type:varchar(50);not null;index"`
	PostLink    string `gorm:"type:varchar(500)"`
	PostID      string `gorm:"type:varchar(100);index"`
	CommentID   string `gorm:"type:varchar(100);index"`
	Username    string `gorm:"type:varchar(100);index"`
	FullName    string `gorm:"type:varchar(200)"`
	Content     string `gorm:"type:text"`
	LikeCount   int    `gorm:"default:0"`
	Timestamp   *time.Time
	CollectedAt time.Time `json:"collected_at"`
	Version     int64     `gorm:"default:1"`
	IsDeleted   bool      `gorm:"default:false;index"`
}

func (UserComment) TableName() string {
	return "user_comments"
}

// ==================== 用户评论话术 ====================

// UserCommentScript 用户评论话术
type UserCommentScript struct {
	BaseModel
	UserID    string `gorm:"type:varchar(36);not null;index"`
	AppID     string `gorm:"type:varchar(36);not null;index"`
	GroupName string `gorm:"type:varchar(50);not null;index"`
	Content   string `gorm:"type:text;not null"`
	UseCount  int    `gorm:"default:0"`
	Status    string `gorm:"type:varchar(20);default:active"` // active/disabled
	Version   int64  `gorm:"default:1"`
	IsDeleted bool   `gorm:"default:false;index"`
}

func (UserCommentScript) TableName() string {
	return "user_comment_scripts"
}

// ==================== 用户文件 ====================

// UserFile 用户文件存储 (素材等大文件)
type UserFile struct {
	BaseModel
	UserID    string `gorm:"type:varchar(36);not null;index"`
	AppID     string `gorm:"type:varchar(36);not null;index"`
	FileType  string `gorm:"type:varchar(50);not null"` // material/screenshot/log/other
	FileName  string `gorm:"type:varchar(255);not null"`
	FilePath  string `gorm:"type:varchar(500);not null"` // 服务端存储路径
	FileSize  int64  `json:"file_size"`
	FileHash  string `gorm:"type:varchar(64)"` // SHA256
	MimeType  string `gorm:"type:varchar(100)"`
	Metadata  string `gorm:"type:json"` // 额外元数据
	Status    string `gorm:"type:varchar(20);default:active"`
	IsDeleted bool   `gorm:"default:false;index"`
}

func (UserFile) TableName() string {
	return "user_files"
}

// ==================== 同步相关 ====================

// SyncCheckpoint 同步检查点
type SyncCheckpoint struct {
	BaseModel
	UserID      string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_sync_checkpoint"`
	DeviceID    string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_sync_checkpoint"`
	AppID       string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_sync_checkpoint"`
	DataType    string    `gorm:"type:varchar(50);not null;uniqueIndex:idx_sync_checkpoint"`
	LastSyncAt  time.Time `gorm:"not null"`
	LastVersion int64     `gorm:"default:0"`
}

func (SyncCheckpoint) TableName() string {
	return "sync_checkpoints"
}

// SyncConflict 同步冲突记录
type SyncConflict struct {
	BaseModel
	UserID        string     `gorm:"type:varchar(36);not null;index"`
	DeviceID      string     `gorm:"type:varchar(36);not null"`
	AppID         string     `gorm:"type:varchar(36);not null"`
	DataType      string     `gorm:"type:varchar(50);not null"`
	DataKey       string     `gorm:"type:varchar(100);not null"`
	LocalVersion  int64      `json:"local_version"`
	ServerVersion int64      `json:"server_version"`
	LocalData     string     `gorm:"type:longtext"`
	ServerData    string     `gorm:"type:longtext"`
	Resolution    string     `gorm:"type:varchar(20)"` // use_local/use_server/merge
	ResolvedData  string     `gorm:"type:longtext"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	Status        string     `gorm:"type:varchar(20);default:pending"` // pending/resolved
}

func (SyncConflict) TableName() string {
	return "sync_conflicts"
}

// SyncLog 同步日志
type SyncLog struct {
	BaseModel
	UserID     string `gorm:"type:varchar(36);not null;index"`
	DeviceID   string `gorm:"type:varchar(36);not null;index"`
	AppID      string `gorm:"type:varchar(36);not null;index"`
	Action     string `gorm:"type:varchar(20);not null"` // push/pull/conflict/resolve
	DataType   string `gorm:"type:varchar(50)"`
	DataKey    string `gorm:"type:varchar(100)"`
	ItemCount  int    `gorm:"default:0"`
	OldVersion int64  `json:"old_version"`
	NewVersion int64  `json:"new_version"`
	Status     string `gorm:"type:varchar(20)"` // success/failed/partial
	ErrorMsg   string `gorm:"type:text"`
	Duration   int64  `json:"duration"` // 毫秒
}

func (SyncLog) TableName() string {
	return "sync_logs"
}

// ==================== 用户声音配置 ====================

// UserVoiceConfig 用户声音配置（TTS配置）
type UserVoiceConfig struct {
	BaseModel
	UserID       string  `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_voice_config_key"`
	AppID        string  `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_user_voice_config_key"`
	VoiceID      int64   `gorm:"not null;uniqueIndex:idx_user_voice_config_key"` // 客户端的voice_configs.id
	Role         string  `gorm:"type:varchar(50)"`                               // 角色标识
	Name         string  `gorm:"type:varchar(100)"`                              // 配置名称
	GPTPath      string  `gorm:"type:varchar(500)"`                              // GPT模型路径
	SoVITSPath   string  `gorm:"type:varchar(500)"`                              // SoVITS模型路径
	RefAudioPath string  `gorm:"type:varchar(500)"`                              // 参考音频路径
	RefText      string  `gorm:"type:text"`                                      // 参考文本
	Language     string  `gorm:"type:varchar(20);default:zh"`
	SpeedFactor  float64 `gorm:"default:1.0"`
	TTSVersion   int     `gorm:"default:2"` // TTS版本: 1=v1, 2=v2, 3=v3, 4=v4, 5=v2Pro, 6=v2ProPlus
	Enabled      bool    `gorm:"default:true"`
	Version      int64   `gorm:"default:1"`
	IsDeleted    bool    `gorm:"default:false;index"`
}

func (UserVoiceConfig) TableName() string {
	return "user_voice_configs"
}

// ==================== 数据类型常量 ====================

const (
	DataTypeConfig        = "config"
	DataTypeWorkflow      = "workflow"
	DataTypeBatchTask     = "batch_task"
	DataTypeMaterial      = "material"
	DataTypePost          = "post"
	DataTypeComment       = "comment"
	DataTypeCommentScript = "comment_script"
	DataTypeVoiceConfig   = "voice_config" // TTS声音配置
	DataTypeTable         = "table"        // 通用表数据
)

// 同步动作常量
const (
	SyncActionPush     = "push"
	SyncActionPull     = "pull"
	SyncActionConflict = "conflict"
	SyncActionResolve  = "resolve"
)

// 冲突解决方式
const (
	ConflictResolutionUseLocal  = "use_local"
	ConflictResolutionUseServer = "use_server"
	ConflictResolutionMerge     = "merge"
)

// ==================== 通用表数据存储 ====================

// UserTableData 通用表数据存储（用于存储任意表结构的数据）
type UserTableData struct {
	BaseModel
	UserID      string `gorm:"type:varchar(36);not null;index:idx_user_table_data;uniqueIndex:idx_user_table_data_unique_record"`
	AppID       string `gorm:"type:varchar(36);not null;index:idx_user_table_data;uniqueIndex:idx_user_table_data_unique_record"`
	SourceTable string `gorm:"type:varchar(100);not null;index:idx_user_table_data;uniqueIndex:idx_user_table_data_unique_record;column:table_name"` // 表名，如 voice_configs, scripts
	RecordID    string `gorm:"type:varchar(100);not null;index:idx_user_table_data;uniqueIndex:idx_user_table_data_unique_record"`                   // 记录ID（原表的主键）
	Data        string `gorm:"type:longtext"`                                                                                                        // JSON 格式的完整记录数据
	Version     int64  `gorm:"default:1"`
	IsDeleted   bool   `gorm:"default:false;index"`
}

func (UserTableData) TableName() string {
	return "user_table_data"
}
