package model

import "time"

// ProviderKind AI Provider 类型
// 对应客户端 StudioContracts.cs 里的 ProviderKind 枚举
type ProviderKind string

const (
	ProviderGemini ProviderKind = "gemini"
	ProviderGpt    ProviderKind = "gpt"
	ProviderVeo    ProviderKind = "veo"
	ProviderSora   ProviderKind = "sora"
	ProviderGrok   ProviderKind = "grok"
	ProviderClaude ProviderKind = "claude"
)

// CredentialHealth 凭证健康度
type CredentialHealth string

const (
	CredentialHealthUnknown  CredentialHealth = "unknown"
	CredentialHealthHealthy  CredentialHealth = "healthy"
	CredentialHealthDegraded CredentialHealth = "degraded"
	CredentialHealthDown     CredentialHealth = "down"
)

// ProviderCredential 平台级 Provider 凭证（API Key）
//
// 设计要点：
//   - 按租户隔离；后台只能管理和使用当前租户自己的凭证。
//   - API Key 用信封加密：APIKeyEnc = AES-256-GCM(DEK, key)；DEKEnc = AES-256-GCM(MasterKey, DEK)
//   - 响应给前端时绝不返回 APIKeyEnc / DEKEnc / 明文 Key，只返回元信息。
//   - 同一租户 + provider 下未删除的 active_channel_name 唯一；软删除后可重建同名通道。
type ProviderCredential struct {
	BaseModel
	TenantID          string           `gorm:"type:varchar(36);not null;index:idx_provider_credentials_tenant;index:idx_prov_mode;index:idx_prov_chan_lookup;uniqueIndex:idx_prov_active_chan" json:"tenant_id"`
	Provider          ProviderKind     `gorm:"type:varchar(16);not null;index:idx_prov_mode;index:idx_prov_chan_lookup;uniqueIndex:idx_prov_active_chan" json:"provider"`
	Mode              string           `gorm:"type:varchar(32);not null;index:idx_prov_mode" json:"mode"`
	ChannelName       string           `gorm:"type:varchar(64);not null;index:idx_prov_chan_lookup" json:"channel_name"`
	ActiveChannelName *string          `gorm:"type:varchar(64);uniqueIndex:idx_prov_active_chan" json:"-"`
	UpstreamBase      string           `gorm:"type:varchar(256);not null" json:"upstream_base"`
	APIKeyEnc         []byte           `gorm:"type:varbinary(2048);not null" json:"-"`
	DEKEnc            []byte           `gorm:"type:varbinary(512);not null" json:"-"`
	EncAlg            string           `gorm:"type:varchar(16);not null;default:'AES-256-GCM'" json:"enc_alg"`
	KeyID             string           `gorm:"type:varchar(32);not null;default:'env-v1'" json:"key_id"` // 主密钥版本，便于轮换
	CustomHeader      string           `gorm:"type:json" json:"custom_headers"`                          // JSON 字符串
	DefaultModel      string           `gorm:"type:varchar(64)" json:"default_model"`
	Enabled           bool             `gorm:"default:true" json:"enabled"`
	IsDefault         bool             `gorm:"default:false" json:"is_default"`
	Priority          int              `gorm:"default:0" json:"priority"`
	HealthStatus      CredentialHealth `gorm:"type:varchar(16);default:unknown" json:"health_status"`
	LastUsedAt        *time.Time       `json:"last_used_at"`
	Note              string           `gorm:"type:varchar(256)" json:"note"`
}

func (ProviderCredential) TableName() string {
	return "provider_credentials"
}
