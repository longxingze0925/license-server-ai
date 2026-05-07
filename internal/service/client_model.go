package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"license-server/internal/model"

	"gorm.io/gorm"
)

type ClientModelService struct{}

func NewClientModelService() *ClientModelService { return &ClientModelService{} }

type ClientModelInput struct {
	ModelKey        string
	DisplayName     string
	Provider        model.ProviderKind
	Scope           model.PricingScope
	Enabled         bool
	SortOrder       int
	SupportedModes  []string
	SupportedScopes []string
	AspectRatios    []string
	Durations       []string
	Note            string
}

type ClientModelRouteInput struct {
	CredentialID  string
	UpstreamModel string
	Enabled       bool
	IsDefault     bool
	Priority      int
	SortOrder     int
	AspectRatios  []string
	Durations     []string
	Resolutions   []string
	MaxImages     int
	Note          string
}

type ClientModelWithRoutes struct {
	Model  model.ClientModel
	Routes []model.ClientModelRoute
}

func (s *ClientModelService) List(tenantID string, includeDisabled bool) ([]ClientModelWithRoutes, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id 不能为空")
	}
	q := model.DB.Where("tenant_id = ?", tenantID)
	if !includeDisabled {
		q = q.Where("enabled = ?", true)
	}
	var rows []model.ClientModel
	if err := q.Order("sort_order ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ClientModelWithRoutes, 0, len(rows))
	for i := range rows {
		routes, err := s.ListRoutes(tenantID, rows[i].ID, includeDisabled)
		if err != nil {
			return nil, err
		}
		out = append(out, ClientModelWithRoutes{Model: rows[i], Routes: routes})
	}
	return out, nil
}

func (s *ClientModelService) Get(tenantID, id string) (*model.ClientModel, []model.ClientModelRoute, error) {
	var row model.ClientModel
	if err := model.DB.Where("tenant_id = ? AND id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(id)).First(&row).Error; err != nil {
		return nil, nil, err
	}
	routes, err := s.ListRoutes(tenantID, row.ID, true)
	if err != nil {
		return nil, nil, err
	}
	return &row, routes, nil
}

func (s *ClientModelService) Create(tenantID string, in ClientModelInput) (*model.ClientModel, error) {
	row, err := buildClientModelRow(tenantID, in)
	if err != nil {
		return nil, err
	}
	if err := s.ensureModelKeyAvailable(row.TenantID, row.Provider, row.Scope, row.ModelKey, ""); err != nil {
		return nil, err
	}
	if err := model.DB.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *ClientModelService) Update(tenantID, id string, in ClientModelInput) (*model.ClientModel, error) {
	row, err := s.getForUpdate(tenantID, id)
	if err != nil {
		return nil, err
	}
	updated, err := buildClientModelRow(tenantID, in)
	if err != nil {
		return nil, err
	}
	if err := s.ensureModelKeyAvailable(row.TenantID, updated.Provider, updated.Scope, updated.ModelKey, row.ID); err != nil {
		return nil, err
	}
	row.ModelKey = updated.ModelKey
	row.DisplayName = updated.DisplayName
	row.Provider = updated.Provider
	row.Scope = updated.Scope
	row.Enabled = updated.Enabled
	row.SortOrder = updated.SortOrder
	row.SupportedModes = updated.SupportedModes
	row.SupportedScopes = updated.SupportedScopes
	row.AspectRatios = updated.AspectRatios
	row.Durations = updated.Durations
	row.Note = updated.Note
	if err := model.DB.Save(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *ClientModelService) Delete(tenantID, id string) error {
	tenantID = strings.TrimSpace(tenantID)
	id = strings.TrimSpace(id)
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.ClientModelRoute{}, "tenant_id = ? AND client_model_id = ?", tenantID, id).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.ClientModel{}, "tenant_id = ? AND id = ?", tenantID, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *ClientModelService) ListRoutes(tenantID, clientModelID string, includeDisabled bool) ([]model.ClientModelRoute, error) {
	q := model.DB.Where("tenant_id = ? AND client_model_id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(clientModelID))
	if !includeDisabled {
		q = q.Where("enabled = ?", true)
	}
	var rows []model.ClientModelRoute
	if err := q.Order("is_default DESC, priority DESC, sort_order ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		var cred model.ProviderCredential
		if err := model.DB.First(&cred, "tenant_id = ? AND id = ?", tenantID, rows[i].CredentialID).Error; err == nil {
			rows[i].Credential = &cred
		}
	}
	return rows, nil
}

func (s *ClientModelService) CreateRoute(tenantID, clientModelID string, in ClientModelRouteInput) (*model.ClientModelRoute, error) {
	if _, err := s.getForUpdate(tenantID, clientModelID); err != nil {
		return nil, err
	}
	row, err := buildClientModelRouteRow(tenantID, clientModelID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateRouteCredential(row); err != nil {
		return nil, err
	}
	if row.IsDefault {
		if err := s.unsetOtherRouteDefaults(tenantID, clientModelID, ""); err != nil {
			return nil, err
		}
	}
	if err := model.DB.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *ClientModelService) UpdateRoute(tenantID, routeID string, in ClientModelRouteInput) (*model.ClientModelRoute, error) {
	var row model.ClientModelRoute
	if err := model.DB.Where("tenant_id = ? AND id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(routeID)).First(&row).Error; err != nil {
		return nil, err
	}
	updated, err := buildClientModelRouteRow(tenantID, row.ClientModelID, in)
	if err != nil {
		return nil, err
	}
	row.CredentialID = updated.CredentialID
	row.UpstreamModel = updated.UpstreamModel
	row.Enabled = updated.Enabled
	row.IsDefault = updated.IsDefault
	row.Priority = updated.Priority
	row.SortOrder = updated.SortOrder
	row.AspectRatios = updated.AspectRatios
	row.Durations = updated.Durations
	row.Resolutions = updated.Resolutions
	row.MaxImages = updated.MaxImages
	row.Note = updated.Note
	if err := s.validateRouteCredential(&row); err != nil {
		return nil, err
	}
	if row.IsDefault {
		if err := s.unsetOtherRouteDefaults(tenantID, row.ClientModelID, row.ID); err != nil {
			return nil, err
		}
	}
	if err := model.DB.Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *ClientModelService) DeleteRoute(tenantID, routeID string) error {
	result := model.DB.Delete(&model.ClientModelRoute{}, "tenant_id = ? AND id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(routeID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *ClientModelService) SelectRoute(tenantID string, provider model.ProviderKind, clientModel string, scope model.PricingScope) (*model.ClientModel, *model.ClientModelRoute, error) {
	clientModel = strings.TrimSpace(clientModel)
	if clientModel == "" {
		return nil, nil, gorm.ErrRecordNotFound
	}
	var cm model.ClientModel
	q := model.DB.Where("tenant_id = ? AND provider = ? AND model_key = ? AND enabled = ?", strings.TrimSpace(tenantID), provider, clientModel, true)
	if scope != "" {
		q = q.Where("scope = ?", scope)
	}
	if err := q.First(&cm).Error; err != nil {
		return nil, nil, err
	}
	routes, err := s.ListRoutes(tenantID, cm.ID, false)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].IsDefault != routes[j].IsDefault {
			return routes[i].IsDefault
		}
		if routes[i].Priority != routes[j].Priority {
			return routes[i].Priority > routes[j].Priority
		}
		return routes[i].SortOrder < routes[j].SortOrder
	})
	for i := range routes {
		route := &routes[i]
		if route.Credential == nil || !route.Credential.Enabled || route.Credential.HealthStatus == model.CredentialHealthDown {
			continue
		}
		if route.Credential.Provider != provider {
			continue
		}
		return &cm, route, nil
	}
	return nil, nil, gorm.ErrRecordNotFound
}

func (s *ClientModelService) SelectRouteForParams(tenantID string, provider model.ProviderKind, clientModel string, scope model.PricingScope, params map[string]any) (*model.ClientModel, *model.ClientModelRoute, error) {
	clientModel = strings.TrimSpace(clientModel)
	if clientModel == "" {
		return nil, nil, gorm.ErrRecordNotFound
	}
	var cm model.ClientModel
	q := model.DB.Where("tenant_id = ? AND provider = ? AND model_key = ? AND enabled = ?", strings.TrimSpace(tenantID), provider, clientModel, true)
	if scope != "" {
		q = q.Where("scope = ?", scope)
	}
	if err := q.First(&cm).Error; err != nil {
		return nil, nil, err
	}
	routes, err := s.ListRoutes(tenantID, cm.ID, false)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].IsDefault != routes[j].IsDefault {
			return routes[i].IsDefault
		}
		if routes[i].Priority != routes[j].Priority {
			return routes[i].Priority > routes[j].Priority
		}
		return routes[i].SortOrder < routes[j].SortOrder
	})
	for i := range routes {
		route := &routes[i]
		if route.Credential == nil || !route.Credential.Enabled || route.Credential.HealthStatus == model.CredentialHealthDown {
			continue
		}
		if route.Credential.Provider != provider {
			continue
		}
		if !routeMatchesRequestParamsWithParams(*route, params) {
			continue
		}
		return &cm, route, nil
	}
	return nil, nil, gorm.ErrRecordNotFound
}

func routeMatchesRequestParamsWithParams(route model.ClientModelRoute, params map[string]any) bool {
	params = NormalizePricingParams(params)
	if aspectRatio := paramString(params, "aspect_ratio"); aspectRatio != "" {
		if values := ResolveRouteAspectRatios(route); len(values) > 0 && !containsStringFold(values, aspectRatio) {
			return false
		}
	}
	if resolution := paramString(params, "resolution"); resolution != "" {
		if values := ResolveRouteResolutions(route); len(values) > 0 && !containsStringFold(values, resolution) {
			return false
		}
	}
	if duration := paramInt(params, "duration_seconds"); duration > 0 {
		if values := ResolveRouteDurations(route); len(values) > 0 && !containsStringFold(values, fmt.Sprint(duration)) {
			return false
		}
	}
	if maxImages := ResolveRouteMaxImages(route); maxImages > 0 {
		count := max(paramInt(params, "reference_image_count"), paramInt(params, "input_image_count"))
		if count > maxImages {
			return false
		}
	}
	return true
}

func paramString(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func paramInt(params map[string]any, key string) int {
	value, ok := params[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		var i int
		if _, err := fmt.Sscan(strings.TrimSpace(v), &i); err == nil {
			return i
		}
	}
	return 0
}

func containsStringFold(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func (s *ClientModelService) HasEnabledModels(tenantID string) (bool, error) {
	var count int64
	if err := model.DB.Model(&model.ClientModel{}).Where("tenant_id = ? AND enabled = ?", strings.TrimSpace(tenantID), true).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *ClientModelService) getForUpdate(tenantID, id string) (*model.ClientModel, error) {
	var row model.ClientModel
	if err := model.DB.Where("tenant_id = ? AND id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(id)).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *ClientModelService) validateRouteCredential(route *model.ClientModelRoute) error {
	var cm model.ClientModel
	if err := model.DB.First(&cm, "tenant_id = ? AND id = ?", route.TenantID, route.ClientModelID).Error; err != nil {
		return err
	}
	var cred model.ProviderCredential
	if err := model.DB.First(&cred, "tenant_id = ? AND id = ?", route.TenantID, route.CredentialID).Error; err != nil {
		return err
	}
	if cred.Provider != cm.Provider {
		return fmt.Errorf("路由渠道 Provider=%s 与客户端模型 Provider=%s 不一致", cred.Provider, cm.Provider)
	}
	return nil
}

func (s *ClientModelService) unsetOtherRouteDefaults(tenantID, clientModelID, excludeID string) error {
	q := model.DB.Model(&model.ClientModelRoute{}).Where("tenant_id = ? AND client_model_id = ?", strings.TrimSpace(tenantID), strings.TrimSpace(clientModelID))
	if strings.TrimSpace(excludeID) != "" {
		q = q.Where("id <> ?", strings.TrimSpace(excludeID))
	}
	return q.Update("is_default", false).Error
}

func (s *ClientModelService) ensureModelKeyAvailable(tenantID string, provider model.ProviderKind, scope model.PricingScope, modelKey, excludeID string) error {
	q := model.DB.Model(&model.ClientModel{}).
		Where("tenant_id = ? AND provider = ? AND scope = ? AND model_key = ?", strings.TrimSpace(tenantID), provider, scope, strings.TrimSpace(modelKey))
	if strings.TrimSpace(excludeID) != "" {
		q = q.Where("id <> ?", strings.TrimSpace(excludeID))
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("同一 Provider 和能力类型下已存在相同客户端模型标识")
	}
	return nil
}

func buildClientModelRow(tenantID string, in ClientModelInput) (*model.ClientModel, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id 不能为空")
	}
	key := normalizeClientModelKey(in.ModelKey)
	if key == "" {
		return nil, errors.New("模型标识不能为空")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return nil, errors.New("模型名称不能为空")
	}
	if in.Provider == "" {
		return nil, errors.New("Provider 不能为空")
	}
	if err := ValidateProviderCredentialProvider(in.Provider); err != nil {
		return nil, err
	}
	if in.Scope == "" {
		return nil, errors.New("能力类型不能为空")
	}
	if !isValidClientModelScope(in.Scope) {
		return nil, errors.New("能力类型不支持")
	}
	supportedScopes := normalizeStringList(in.SupportedScopes)
	if len(supportedScopes) == 0 {
		supportedScopes = []string{string(in.Scope)}
	}
	return &model.ClientModel{
		TenantID:        tenantID,
		ModelKey:        key,
		DisplayName:     strings.TrimSpace(in.DisplayName),
		Provider:        in.Provider,
		Scope:           in.Scope,
		Enabled:         in.Enabled,
		SortOrder:       in.SortOrder,
		SupportedModes:  mustJSONStrings(normalizeStringList(in.SupportedModes)),
		SupportedScopes: mustJSONStrings(supportedScopes),
		AspectRatios:    mustJSONStrings(normalizeStringList(in.AspectRatios)),
		Durations:       mustJSONStrings(normalizeStringList(in.Durations)),
		Note:            strings.TrimSpace(in.Note),
	}, nil
}

func buildClientModelRouteRow(tenantID, clientModelID string, in ClientModelRouteInput) (*model.ClientModelRoute, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(clientModelID) == "" {
		return nil, errors.New("tenant_id/client_model_id 不能为空")
	}
	if strings.TrimSpace(in.CredentialID) == "" {
		return nil, errors.New("请选择真实渠道")
	}
	if strings.TrimSpace(in.UpstreamModel) == "" {
		return nil, errors.New("真实上游模型不能为空")
	}
	return &model.ClientModelRoute{
		TenantID:      strings.TrimSpace(tenantID),
		ClientModelID: strings.TrimSpace(clientModelID),
		CredentialID:  strings.TrimSpace(in.CredentialID),
		UpstreamModel: strings.TrimSpace(in.UpstreamModel),
		Enabled:       in.Enabled,
		IsDefault:     in.IsDefault,
		Priority:      in.Priority,
		SortOrder:     in.SortOrder,
		AspectRatios:  mustJSONStrings(normalizeStringList(in.AspectRatios)),
		Durations:     mustJSONStrings(normalizeStringList(in.Durations)),
		Resolutions:   mustJSONStrings(normalizeStringList(in.Resolutions)),
		MaxImages:     max(in.MaxImages, 0),
		Note:          strings.TrimSpace(in.Note),
	}, nil
}

func isValidClientModelScope(scope model.PricingScope) bool {
	switch scope {
	case model.PricingScopeImage, model.PricingScopeVideo, model.PricingScopeAnalysis, model.PricingScopeChat:
		return true
	default:
		return false
	}
}

func normalizeClientModelKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func normalizeStringList(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mustJSONStrings(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

func ParseClientModelJSONStrings(value string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return []string{}
	}
	return normalizeStringList(out)
}
