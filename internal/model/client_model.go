package model

// ClientModel 后台配置给客户端展示的模型。
type ClientModel struct {
	BaseModel
	TenantID        string       `gorm:"type:varchar(36);not null;index:idx_client_models_tenant;index:idx_client_model_key" json:"tenant_id"`
	ModelKey        string       `gorm:"type:varchar(80);not null;index:idx_client_model_key" json:"model_key"`
	DisplayName     string       `gorm:"type:varchar(120);not null" json:"display_name"`
	Provider        ProviderKind `gorm:"type:varchar(16);not null;index;index:idx_client_model_key" json:"provider"`
	Scope           PricingScope `gorm:"type:varchar(16);not null;index;index:idx_client_model_key" json:"scope"`
	Enabled         bool         `gorm:"not null;default:true;index" json:"enabled"`
	SortOrder       int          `gorm:"not null;default:0;index" json:"sort_order"`
	SupportedModes  string       `gorm:"type:json" json:"supported_modes"`
	SupportedScopes string       `gorm:"type:json" json:"supported_scopes"`
	AspectRatios    string       `gorm:"type:json" json:"aspect_ratios"`
	Durations       string       `gorm:"type:json" json:"durations"`
	Note            string       `gorm:"type:varchar(256)" json:"note"`
}

func (ClientModel) TableName() string {
	return "client_models"
}

// ClientModelRoute 一个客户端模型下的真实渠道路由。
type ClientModelRoute struct {
	BaseModel
	TenantID      string `gorm:"type:varchar(36);not null;index:idx_client_model_routes_tenant" json:"tenant_id"`
	ClientModelID string `gorm:"type:varchar(36);not null;index" json:"client_model_id"`
	CredentialID  string `gorm:"type:varchar(36);not null;index" json:"credential_id"`
	UpstreamModel string `gorm:"type:varchar(120);not null" json:"upstream_model"`
	Enabled       bool   `gorm:"not null;default:true;index" json:"enabled"`
	IsDefault     bool   `gorm:"not null;default:false;index" json:"is_default"`
	Priority      int    `gorm:"not null;default:0;index" json:"priority"`
	SortOrder     int    `gorm:"not null;default:0;index" json:"sort_order"`
	AspectRatios  string `gorm:"type:json" json:"aspect_ratios"`
	Durations     string `gorm:"type:json" json:"durations"`
	Resolutions   string `gorm:"type:json" json:"resolutions"`
	MaxImages     int    `gorm:"not null;default:0" json:"max_images"`
	Note          string `gorm:"type:varchar(256)" json:"note"`

	Credential *ProviderCredential `gorm:"-" json:"credential,omitempty"`
}

func (ClientModelRoute) TableName() string {
	return "client_model_routes"
}
