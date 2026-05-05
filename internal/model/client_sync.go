package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ClientSyncData 客户端数据同步表
type ClientSyncData struct {
	ID           string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	TenantID     string         `gorm:"type:varchar(36);index" json:"tenant_id"`
	AppID        string         `gorm:"type:varchar(36);index" json:"app_id"`
	CustomerID   string         `gorm:"type:varchar(36);index" json:"customer_id"`    // 关联客户
	ClientUserID string         `gorm:"type:varchar(36);index" json:"client_user_id"` // 历史字段，当前存 customer_id
	DataType     string         `gorm:"type:varchar(50);index" json:"data_type"`      // scripts/danmaku_groups/ai_config
	DataJSON     string         `gorm:"type:longtext" json:"data_json"`               // JSON数据
	Version      int            `gorm:"default:1" json:"version"`                     // 版本号
	DeviceName   string         `gorm:"type:varchar(100)" json:"device_name"`         // 设备名称
	MachineID    string         `gorm:"type:varchar(100)" json:"machine_id"`          // 设备ID
	IsCurrent    bool           `gorm:"default:true" json:"is_current"`               // 是否为当前版本
	DataSize     int64          `gorm:"default:0" json:"data_size"`                   // 数据大小(字节)
	ItemCount    int            `gorm:"default:0" json:"item_count"`                  // 条目数量
	Checksum     string         `gorm:"type:varchar(64)" json:"checksum"`             // 数据校验和
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联
	Application *Application `gorm:"foreignKey:AppID" json:"application,omitempty"`
	Customer    *Customer    `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
}

// TableName 表名
func (ClientSyncData) TableName() string {
	return "client_sync_data"
}

// BeforeCreate 创建前生成UUID
func (c *ClientSyncData) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// DataTypeScripts 话术管理
const DataTypeScripts = "scripts"

// DataTypeDanmakuGroups 互动规则
const DataTypeDanmakuGroups = "danmaku_groups"

// DataTypeAIConfig AI配置
const DataTypeAIConfig = "ai_config"

// DataTypeRandomWordAIConfig 随机词AI配置
const DataTypeRandomWordAIConfig = "random_word_ai_config"

// MaxSyncVersions 每种数据最大保留版本数
const MaxSyncVersions = 10
