package model

import "time"

// PricingScope 计价作用范围
type PricingScope string

const (
	PricingScopeImage    PricingScope = "image"
	PricingScopeVideo    PricingScope = "video"
	PricingScopeAnalysis PricingScope = "analysis"
	PricingScopeChat     PricingScope = "chat"
)

// PricingRule 计价规则（后台可配）
//
// 匹配逻辑：按 priority desc 找第一条 (provider, scope) 命中且 MatchJSON 全部 key 都
// 在请求参数里命中的规则。命中后若 Formula 非空则按公式算（如 "duration_seconds * 2"），
// 否则使用 Credits 字段。规则不命中 → 拒绝（"未配置计价"）。
type PricingRule struct {
	ID        int64        `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  string       `gorm:"type:varchar(36);not null;index:idx_pricing_rules_tenant;index:idx_match" json:"tenant_id"`
	Provider  string       `gorm:"type:varchar(16);not null;index:idx_match" json:"provider"` // 通配可填 '*'
	Scope     PricingScope `gorm:"type:varchar(16);not null;index:idx_match" json:"scope"`
	MatchJSON string       `gorm:"type:json" json:"match_json"` // 可选：{"duration_seconds":5,"model":"veo-3"}
	Credits   int          `gorm:"not null" json:"credits"`
	Formula   string       `gorm:"type:varchar(128)" json:"formula"` // 可选：'duration_seconds * 2'
	Priority  int          `gorm:"not null;default:0;index" json:"priority"`
	Enabled   bool         `gorm:"not null;default:true" json:"enabled"`
	Note      string       `gorm:"type:varchar(256)" json:"note"`
	CreatedAt time.Time    `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time    `gorm:"autoUpdateTime" json:"updated_at"`
}

func (PricingRule) TableName() string {
	return "pricing_rules"
}
