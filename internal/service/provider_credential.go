package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"license-server/internal/model"
	"license-server/internal/pkg/crypto"

	"gorm.io/gorm"
)

// ProviderCredentialService 平台级 Provider 凭证管理（含信封加解密）。
//
// 单例模式：进程启动时调用 InitProviderCredentialService(masterKeyProvider) 注入主密钥来源；
// 启动自检 SelfCheck() 会用主密钥试解密任意一条已存在凭证，若失败立即 panic（防止用错 master 把库写废）。
type ProviderCredentialService struct {
	masterKey []byte
	keyID     string
}

var (
	credentialServiceOnce            sync.Once
	credentialService                *ProviderCredentialService
	credentialServiceErr             error
	ErrCredentialUnavailable         = errors.New("指定渠道不可用或不匹配")
	ErrChannelNameUnavailable        = errors.New("同一 Provider 下已存在未删除的同名通道")
	ErrUnsupportedProviderCredential = errors.New("Provider 暂未接入代理能力")
)

// InitProviderCredentialService 在 main.go 启动期调用一次。
func InitProviderCredentialService(provider crypto.MasterKeyProvider) (*ProviderCredentialService, error) {
	credentialServiceOnce.Do(func() {
		key, err := provider.GetMasterKey(context.Background())
		if err != nil {
			credentialServiceErr = fmt.Errorf("加载主密钥失败: %w", err)
			return
		}
		credentialService = &ProviderCredentialService{
			masterKey: key,
			keyID:     provider.KeyID(),
		}
	})
	return credentialService, credentialServiceErr
}

// GetProviderCredentialService 获取已初始化的服务（未初始化返回 nil）。
func GetProviderCredentialService() *ProviderCredentialService {
	return credentialService
}

// SelfCheck 启动自检：随便挑一条已存在凭证试解密，验证主密钥与库内数据匹配。
// 没有任何凭证时直接返回 nil（首次部署属于正常状态）。
func (s *ProviderCredentialService) SelfCheck() error {
	var row model.ProviderCredential
	err := model.DB.Order("created_at ASC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("自检读取凭证失败: %w", err)
	}
	if _, err := crypto.EnvelopeDecrypt(s.masterKey, row.APIKeyEnc, row.DEKEnc); err != nil {
		return fmt.Errorf("自检解密首条凭证失败（主密钥可能不正确，已拒绝启动以防写坏数据）: %w", err)
	}
	return nil
}

// CreateInput 创建凭证入参（明文 API Key 仅在内存停留，不入库）。
type CreateInput struct {
	TenantID     string
	Provider     model.ProviderKind
	Mode         string
	ChannelName  string
	UpstreamBase string
	APIKey       string // 明文，调用方传入后立即被加密
	DefaultModel string
	CustomHeader string // JSON 字符串
	Enabled      bool
	IsDefault    bool
	Priority     int
	Note         string
}

// UpdateInput 更新凭证入参；APIKey 留空表示不修改 Key。
type UpdateInput struct {
	Mode         *string
	ChannelName  *string
	UpstreamBase *string
	APIKey       *string // nil 表示不动；空字符串视为不动
	DefaultModel *string
	CustomHeader *string
	Enabled      *bool
	IsDefault    *bool
	Priority     *int
	Note         *string
}

// Create 新建凭证。
func (s *ProviderCredentialService) Create(in CreateInput) (*model.ProviderCredential, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	if in.TenantID == "" {
		return nil, errors.New("tenant_id 不能为空")
	}
	in.Provider = normalizeProviderKind(in.Provider)
	if err := ValidateProviderCredentialProvider(in.Provider); err != nil {
		return nil, err
	}
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.APIKey == "" {
		return nil, errors.New("API Key 不能为空")
	}
	apiKeyEnc, dekEnc, err := crypto.EnvelopeEncrypt(s.masterKey, []byte(in.APIKey))
	if err != nil {
		return nil, fmt.Errorf("加密 API Key 失败: %w", err)
	}

	row := &model.ProviderCredential{
		TenantID:          in.TenantID,
		Provider:          in.Provider,
		Mode:              NormalizeProviderCredentialMode(in.Provider, in.Mode),
		ChannelName:       strings.TrimSpace(in.ChannelName),
		ActiveChannelName: activeChannelName(strings.TrimSpace(in.ChannelName)),
		UpstreamBase:      normalizeUpstreamBase(in.UpstreamBase),
		APIKeyEnc:         apiKeyEnc,
		DEKEnc:            dekEnc,
		EncAlg:            "AES-256-GCM",
		KeyID:             s.keyID,
		CustomHeader:      normalizeJSON(in.CustomHeader),
		DefaultModel:      strings.TrimSpace(in.DefaultModel),
		Enabled:           in.Enabled,
		IsDefault:         in.IsDefault,
		Priority:          in.Priority,
		HealthStatus:      model.CredentialHealthUnknown,
		Note:              strings.TrimSpace(in.Note),
	}
	if err := s.ensureActiveChannelNameAvailable(row.TenantID, row.Provider, row.ChannelName, ""); err != nil {
		return nil, err
	}

	if err := model.DB.Create(row).Error; err != nil {
		return nil, err
	}

	// 若设为默认，把同 (provider, mode) 下其它行的 is_default 清零
	if in.IsDefault {
		if err := s.unsetOtherDefaults(row.ID, row.TenantID, row.Provider, row.Mode); err != nil {
			return nil, fmt.Errorf("更新默认标记失败: %w", err)
		}
	}
	return row, nil
}

// Update 更新凭证。
func (s *ProviderCredentialService) Update(id string, in UpdateInput) (*model.ProviderCredential, error) {
	return s.UpdateForTenant("", id, in)
}

// UpdateForTenant 更新当前租户凭证。tenantID 为空时保留旧内部调用语义。
func (s *ProviderCredentialService) UpdateForTenant(tenantID, id string, in UpdateInput) (*model.ProviderCredential, error) {
	var row model.ProviderCredential
	q := model.DB.Where("id = ?", id)
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if err := q.First(&row).Error; err != nil {
		return nil, err
	}
	row.Provider = normalizeProviderKind(row.Provider)
	if err := ValidateProviderCredentialProvider(row.Provider); err != nil {
		return nil, err
	}

	if in.Mode != nil {
		row.Mode = NormalizeProviderCredentialMode(row.Provider, *in.Mode)
	}
	if in.ChannelName != nil {
		row.ChannelName = strings.TrimSpace(*in.ChannelName)
	}
	if in.UpstreamBase != nil {
		row.UpstreamBase = normalizeUpstreamBase(*in.UpstreamBase)
	}
	if in.DefaultModel != nil {
		row.DefaultModel = strings.TrimSpace(*in.DefaultModel)
	}
	if in.CustomHeader != nil {
		row.CustomHeader = normalizeJSON(*in.CustomHeader)
	}
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	if in.IsDefault != nil {
		row.IsDefault = *in.IsDefault
	}
	if in.Priority != nil {
		row.Priority = *in.Priority
	}
	if in.Note != nil {
		row.Note = strings.TrimSpace(*in.Note)
	}
	if in.APIKey != nil && *in.APIKey != "" {
		apiKey := strings.TrimSpace(*in.APIKey)
		if apiKey == "" {
			return nil, errors.New("API Key 不能为空")
		}
		apiKeyEnc, dekEnc, err := crypto.EnvelopeEncrypt(s.masterKey, []byte(apiKey))
		if err != nil {
			return nil, fmt.Errorf("加密 API Key 失败: %w", err)
		}
		row.APIKeyEnc = apiKeyEnc
		row.DEKEnc = dekEnc
		row.KeyID = s.keyID
		row.HealthStatus = model.CredentialHealthUnknown
	}
	row.ActiveChannelName = activeChannelName(row.ChannelName)
	if err := s.ensureActiveChannelNameAvailable(row.TenantID, row.Provider, row.ChannelName, row.ID); err != nil {
		return nil, err
	}

	if err := model.DB.Save(&row).Error; err != nil {
		return nil, err
	}

	if in.IsDefault != nil && *in.IsDefault {
		if err := s.unsetOtherDefaults(row.ID, row.TenantID, row.Provider, row.Mode); err != nil {
			return nil, fmt.Errorf("更新默认标记失败: %w", err)
		}
	}
	return &row, nil
}

// Delete 删除凭证（软删除）。
func (s *ProviderCredentialService) Delete(id string) error {
	return s.DeleteForTenant("", id)
}

// DeleteForTenant 删除当前租户凭证。tenantID 为空时保留旧内部调用语义。
func (s *ProviderCredentialService) DeleteForTenant(tenantID, id string) error {
	tenantID = strings.TrimSpace(tenantID)
	return model.DB.Transaction(func(tx *gorm.DB) error {
		q := tx.Where("id = ?", id)
		if tenantID != "" {
			q = q.Where("tenant_id = ?", tenantID)
		}
		var row model.ProviderCredential
		if err := q.Select("id").First(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProviderCredential{}).
			Where("id = ?", row.ID).
			Update("active_channel_name", nil).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.ProviderCredential{}, "id = ?", row.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// Get 取单条（不含 Key 明文）。
func (s *ProviderCredentialService) Get(id string) (*model.ProviderCredential, error) {
	return s.GetForTenant("", id)
}

// GetForTenant 取当前租户单条凭证。tenantID 为空时保留旧内部调用语义。
func (s *ProviderCredentialService) GetForTenant(tenantID, id string) (*model.ProviderCredential, error) {
	var row model.ProviderCredential
	q := model.DB.Where("id = ?", id)
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if err := q.First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListFilter 列表过滤条件。
type ListFilter struct {
	TenantID string
	Provider string
	Mode     string
	Enabled  *bool
	Page     int
	PageSize int
}

// List 分页列出凭证（不含 Key 明文）。
func (s *ProviderCredentialService) List(f ListFilter) ([]model.ProviderCredential, int64, error) {
	q := model.DB.Model(&model.ProviderCredential{})
	if tenantID := strings.TrimSpace(f.TenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	provider := model.ProviderKind(strings.ToLower(strings.TrimSpace(f.Provider)))
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	if f.Mode != "" {
		mode := strings.ToLower(strings.TrimSpace(f.Mode))
		if provider != "" {
			mode = NormalizeProviderCredentialMode(provider, mode)
		}
		q = q.Where("mode = ?", mode)
	}
	if f.Enabled != nil {
		q = q.Where("enabled = ?", *f.Enabled)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 50
	}

	var rows []model.ProviderCredential
	if err := q.Order("priority DESC, created_at DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// Decrypt 取明文 Key —— 仅供 proxy 转发模块在内部内存中使用。
// 调用方使用完应自行 zero-out。绝不能把返回值写日志、写响应、写数据库。
func (s *ProviderCredentialService) Decrypt(row *model.ProviderCredential) ([]byte, error) {
	return crypto.EnvelopeDecrypt(s.masterKey, row.APIKeyEnc, row.DEKEnc)
}

// SelectFor 选一条可用凭证用于转发：按 (priority desc, last_used_at asc) 取头一条
// enabled=true 且 health!=down 的记录；命中即更新 last_used_at。
func (s *ProviderCredentialService) SelectFor(provider model.ProviderKind, mode string) (*model.ProviderCredential, error) {
	return s.SelectForTenantExcluding("", provider, mode, nil)
}

// SelectForExcluding 同 SelectFor，但跳过 excludeIDs 列表里的凭证（容灾重试时使用）。
func (s *ProviderCredentialService) SelectForExcluding(provider model.ProviderKind, mode string, excludeIDs []string) (*model.ProviderCredential, error) {
	return s.SelectForTenantExcluding("", provider, mode, excludeIDs)
}

// SelectForTenantExcluding 选当前租户可用凭证；tenantID 为空时保留旧内部调用语义。
func (s *ProviderCredentialService) SelectForTenantExcluding(tenantID string, provider model.ProviderKind, mode string, excludeIDs []string) (*model.ProviderCredential, error) {
	var row model.ProviderCredential
	q := model.DB.Where("provider = ? AND enabled = ? AND health_status <> ?",
		provider, true, model.CredentialHealthDown)
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if mode != "" {
		q = q.Where("mode = ?", NormalizeProviderCredentialMode(provider, mode))
	}
	if len(excludeIDs) > 0 {
		q = q.Where("id NOT IN ?", excludeIDs)
	}
	if err := q.Order("priority DESC, last_used_at ASC").First(&row).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	model.DB.Model(&model.ProviderCredential{}).
		Where("id = ?", row.ID).
		Update("last_used_at", &now)
	row.LastUsedAt = &now
	return &row, nil
}

// SelectByIDForUse 精确选择一条可用凭证。用于客户端明确选择后台渠道时，
// 必须校验 provider/mode/enabled/health，避免拿错渠道或偷偷降级到其它 key。
func (s *ProviderCredentialService) SelectByIDForUse(id string, provider model.ProviderKind, mode string) (*model.ProviderCredential, error) {
	return s.SelectByIDForTenantUse("", id, provider, mode)
}

// SelectByIDForTenantUse 精确选择当前租户的一条可用凭证。
func (s *ProviderCredentialService) SelectByIDForTenantUse(tenantID, id string, provider model.ProviderKind, mode string) (*model.ProviderCredential, error) {
	var row model.ProviderCredential
	q := model.DB.Where("id = ? AND provider = ? AND enabled = ? AND health_status <> ?",
		id, provider, true, model.CredentialHealthDown)
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrCredentialUnavailable, id)
		}
		return nil, err
	}
	if mode != "" && NormalizeProviderCredentialMode(provider, row.Mode) != NormalizeProviderCredentialMode(provider, mode) {
		return nil, fmt.Errorf("%w: mode=%s", ErrCredentialUnavailable, mode)
	}
	now := time.Now()
	model.DB.Model(&model.ProviderCredential{}).
		Where("id = ?", row.ID).
		Update("last_used_at", &now)
	row.LastUsedAt = &now
	return &row, nil
}

func NormalizeProviderCredentialMode(provider model.ProviderKind, mode string) string {
	provider = normalizeProviderKind(provider)
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch provider {
	case model.ProviderGemini:
		if mode == "duoyuan" {
			return "duoyuan"
		}
		return "official"
	case model.ProviderGpt:
		if mode == "gzxsy" {
			return "gzxsy"
		}
		return "official"
	case model.ProviderVeo:
		switch mode {
		case "adapter", "duoyuan":
			return mode
		default:
			return "google"
		}
	case model.ProviderSora:
		if mode == "chat" {
			return "chat"
		}
		return "async"
	case model.ProviderGrok:
		switch mode {
		case "duoyuan", "suchuang":
			return mode
		default:
			return "official"
		}
	default:
		if mode == "" {
			return "official"
		}
		return mode
	}
}

func normalizeProviderKind(provider model.ProviderKind) model.ProviderKind {
	return model.ProviderKind(strings.ToLower(strings.TrimSpace(string(provider))))
}

func ValidateProviderCredentialProvider(provider model.ProviderKind) error {
	switch normalizeProviderKind(provider) {
	case model.ProviderGemini, model.ProviderGpt, model.ProviderVeo, model.ProviderSora, model.ProviderGrok:
		return nil
	case model.ProviderClaude:
		return fmt.Errorf("%w: Claude 当前只保留类型定义，代理 adapter 未接入，不能配置为可用凭证", ErrUnsupportedProviderCredential)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedProviderCredential, provider)
	}
}

// MarkHealth 写凭证健康度。
func (s *ProviderCredentialService) MarkHealth(id string, h model.CredentialHealth) error {
	return model.DB.Model(&model.ProviderCredential{}).
		Where("id = ?", id).
		Update("health_status", h).Error
}

func (s *ProviderCredentialService) unsetOtherDefaults(currentID, tenantID string, provider model.ProviderKind, mode string) error {
	return model.DB.Model(&model.ProviderCredential{}).
		Where("tenant_id = ? AND provider = ? AND mode = ? AND id <> ?", tenantID, provider, mode, currentID).
		Update("is_default", false).Error
}

func (s *ProviderCredentialService) ensureActiveChannelNameAvailable(tenantID string, provider model.ProviderKind, channelName, excludeID string) error {
	tenantID = strings.TrimSpace(tenantID)
	channelName = strings.TrimSpace(channelName)
	if tenantID == "" || provider == "" || channelName == "" {
		return nil
	}
	q := model.DB.Model(&model.ProviderCredential{}).
		Where(
			"tenant_id = ? AND provider = ? AND (active_channel_name = ? OR (active_channel_name IS NULL AND channel_name = ?))",
			tenantID,
			provider,
			channelName,
			channelName,
		)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: %s", ErrChannelNameUnavailable, channelName)
	}
	return nil
}

func activeChannelName(channelName string) *string {
	channelName = strings.TrimSpace(channelName)
	if channelName == "" {
		return nil
	}
	return &channelName
}

func normalizeUpstreamBase(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

// normalizeJSON 把 CustomHeader 标准化成数据库 JSON 列接受的值。
// MariaDB / MySQL 对 JSON 列有 json_valid() 约束，空字符串会触发约束失败，
// 所以未填写时统一存 "{}" 而不是 ""。
func normalizeJSON(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return "{}"
	}
	return t
}
