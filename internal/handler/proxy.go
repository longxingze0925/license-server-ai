package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"license-server/internal/adapter"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"license-server/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ProxyHandler AI Provider 转发与凭证管理的 HTTP 入口。
type ProxyHandler struct {
	credService        *service.ProviderCredentialService
	clientModelService *service.ClientModelService
	proxy              *service.ProviderProxyService
	async              *service.AsyncRunnerService
}

func NewProxyHandler(asyncRunner *service.AsyncRunnerService) *ProxyHandler {
	credSvc := service.GetProviderCredentialService()
	pricing := service.NewPricingService()
	credit := service.NewCreditService()
	if asyncRunner == nil {
		asyncRunner = service.NewAsyncRunnerService(
			pricing, credit, credSvc, service.NewGenerationFileService(), adapter.NewAsyncRegistry(),
		)
	}
	return &ProxyHandler{
		credService:        credSvc,
		clientModelService: service.NewClientModelService(),
		proxy: service.NewProviderProxyService(
			pricing, credit, credSvc, adapter.NewRegistry(),
		),
		async: asyncRunner,
	}
}

// AsyncRunner 暴露给 main 起 worker 用。
func (h *ProxyHandler) AsyncRunner() *service.AsyncRunnerService { return h.async }

// ===================== 客户端能力发现 =====================

type proxyCapabilitiesResponse struct {
	Providers []proxyCapabilityProvider `json:"providers"`
}

type proxyCapabilityProvider struct {
	Provider       string                   `json:"provider"`
	DisplayName    string                   `json:"display_name"`
	Description    string                   `json:"description"`
	DefaultBaseURL string                   `json:"default_base_url"`
	DefaultModel   string                   `json:"default_model"`
	Channels       []proxyCapabilityChannel `json:"channels"`
}

type proxyCapabilityChannel struct {
	ChannelID             string                 `json:"channel_id"`
	ChannelName           string                 `json:"channel_name"`
	Mode                  string                 `json:"mode"`
	BaseURL               string                 `json:"base_url"`
	DefaultModel          string                 `json:"default_model"`
	Priority              int                    `json:"priority"`
	SortOrder             int                    `json:"sort_order"`
	IsDefault             bool                   `json:"is_default"`
	Enabled               bool                   `json:"enabled"`
	HealthStatus          string                 `json:"health_status"`
	SupportedModes        []string               `json:"supported_modes"`
	SupportedScopes       []string               `json:"supported_scopes"`
	SupportedAspectRatios []string               `json:"supported_aspect_ratios"`
	SupportedDurations    []string               `json:"supported_durations"`
	Models                []proxyCapabilityModel `json:"models"`
}

type proxyCapabilityModel struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	SupportedModes  []string `json:"supported_modes"`
	SupportedScopes []string `json:"supported_scopes"`
}

type clientModelRoute struct {
	Provider              model.ProviderKind
	ClientModel           string
	DisplayName           string
	Scope                 model.PricingScope
	Mode                  string
	CredentialID          string
	UpstreamModel         string
	SupportedModes        []string
	SupportedScopes       []string
	SupportedAspectRatios []string
	SupportedDurations    []string
	IsDefault             bool
	Priority              int
	SortOrder             int
}

// Capabilities GET /api/proxy/capabilities
//
// 客户端用它把后台真实启用的 provider_credentials 变成渠道/mode/model 选择；
// 不返回 API Key、密文 Key、自定义 Header。
func (h *ProxyHandler) Capabilities(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	if h.credService == nil {
		response.ServerError(c, "Provider 凭证服务未初始化")
		return
	}

	tenantID := middleware.GetTenantID(c)
	rows, err := h.listEnabledCredentials(tenantID)
	if err != nil {
		response.ServerError(c, "查询能力失败: "+err.Error())
		return
	}

	if h.clientModelService != nil {
		clientModels, err := h.clientModelService.List(tenantID, false)
		if err != nil {
			response.ServerError(c, "查询客户端模型失败: "+err.Error())
			return
		}
		if configured := buildConfiguredProxyCapabilities(clientModels); len(configured.Providers) > 0 {
			response.Success(c, configured)
			return
		}
	}

	response.Success(c, buildProxyCapabilities(rows))
}

func (h *ProxyHandler) listEnabledCredentials(tenantID string) ([]model.ProviderCredential, error) {
	enabled := true
	const pageSize = 200
	var all []model.ProviderCredential
	for page := 1; ; page++ {
		rows, total, err := h.credService.List(service.ListFilter{
			TenantID: tenantID,
			Enabled:  &enabled,
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(all) >= int(total) || len(rows) == 0 {
			return all, nil
		}
	}
}

func buildProxyCapabilities(rows []model.ProviderCredential) proxyCapabilitiesResponse {
	grouped := map[model.ProviderKind][]model.ProviderCredential{}
	for _, row := range rows {
		if row.HealthStatus == model.CredentialHealthDown {
			continue
		}
		grouped[row.Provider] = append(grouped[row.Provider], row)
	}

	providers := make([]proxyCapabilityProvider, 0, len(grouped))
	for provider, credentials := range grouped {
		sort.SliceStable(credentials, func(i, j int) bool {
			if credentials[i].IsDefault != credentials[j].IsDefault {
				return credentials[i].IsDefault
			}
			if credentials[i].Priority != credentials[j].Priority {
				return credentials[i].Priority > credentials[j].Priority
			}
			return credentials[i].CreatedAt.Before(credentials[j].CreatedAt)
		})

		channels := make([]proxyCapabilityChannel, 0, len(credentials))
		for index, row := range credentials {
			channel := credentialToCapabilityChannel(row, index)
			if len(channel.SupportedModes) == 0 && len(channel.SupportedScopes) == 0 {
				continue
			}
			channels = append(channels, channel)
		}
		if len(channels) == 0 {
			continue
		}
		ensureCapabilityDefaultChannel(channels)
		channels = collapseCapabilityChannelsForClient(provider, channels)

		defaultChannel := channels[0]
		providers = append(providers, proxyCapabilityProvider{
			Provider:       string(provider),
			DisplayName:    providerDisplayName(provider),
			Description:    providerDescription(provider),
			DefaultBaseURL: firstNonEmpty(defaultChannel.BaseURL, providerDefaultBaseURL(provider)),
			DefaultModel:   firstNonEmpty(defaultChannel.DefaultModel, providerDefaultModel(provider, "")),
			Channels:       channels,
		})
	}

	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].Provider < providers[j].Provider
	})
	return proxyCapabilitiesResponse{Providers: providers}
}

func buildConfiguredProxyCapabilities(clientModels []service.ClientModelWithRoutes) proxyCapabilitiesResponse {
	grouped := map[model.ProviderKind][]proxyCapabilityChannel{}
	for _, item := range clientModels {
		cm := item.Model
		if !cm.Enabled {
			continue
		}
		hasEnabledRoute := false
		for _, route := range item.Routes {
			if route.Enabled && route.Credential != nil && route.Credential.Enabled && route.Credential.HealthStatus != model.CredentialHealthDown {
				hasEnabledRoute = true
				break
			}
		}
		if !hasEnabledRoute {
			continue
		}

		modes := nonNilStrings(service.ParseClientModelJSONStrings(cm.SupportedModes))
		scopes := nonNilStrings(service.ParseClientModelJSONStrings(cm.SupportedScopes))
		if len(scopes) == 0 {
			scopes = []string{string(cm.Scope)}
		}
		if len(modes) == 0 {
			modes = defaultClientModelSupportedModes(cm.Scope)
		}
		aspectRatios := nonNilStrings(service.ParseClientModelJSONStrings(cm.AspectRatios))
		durations := nonNilStrings(service.ParseClientModelJSONStrings(cm.Durations))
		channel := proxyCapabilityChannel{
			ChannelID:             "client-model:" + string(cm.Provider) + ":" + cm.ModelKey + ":" + string(cm.Scope),
			ChannelName:           "后台路由",
			Mode:                  "",
			BaseURL:               "",
			DefaultModel:          cm.ModelKey,
			Priority:              0,
			SortOrder:             cm.SortOrder,
			IsDefault:             false,
			Enabled:               true,
			HealthStatus:          string(model.CredentialHealthHealthy),
			SupportedModes:        modes,
			SupportedScopes:       scopes,
			SupportedAspectRatios: aspectRatios,
			SupportedDurations:    durations,
			Models: []proxyCapabilityModel{{
				ID:              cm.ModelKey,
				DisplayName:     firstNonEmpty(cm.DisplayName, cm.ModelKey),
				SupportedModes:  modes,
				SupportedScopes: scopes,
			}},
		}
		grouped[cm.Provider] = append(grouped[cm.Provider], channel)
	}

	providers := make([]proxyCapabilityProvider, 0, len(grouped))
	for provider, channels := range grouped {
		sort.SliceStable(channels, func(i, j int) bool {
			if channels[i].SortOrder != channels[j].SortOrder {
				return channels[i].SortOrder < channels[j].SortOrder
			}
			return channels[i].DefaultModel < channels[j].DefaultModel
		})
		ensureCapabilityDefaultChannel(channels)
		defaultChannel := channels[0]
		providers = append(providers, proxyCapabilityProvider{
			Provider:       string(provider),
			DisplayName:    providerDisplayName(provider),
			Description:    providerDescription(provider),
			DefaultBaseURL: providerDefaultBaseURL(provider),
			DefaultModel:   defaultChannel.DefaultModel,
			Channels:       channels,
		})
	}

	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].Provider < providers[j].Provider
	})
	return proxyCapabilitiesResponse{Providers: providers}
}

func defaultClientModelSupportedModes(scope model.PricingScope) []string {
	switch scope {
	case model.PricingScopeVideo:
		return []string{"text_to_video", "image_to_video"}
	case model.PricingScopeImage:
		return []string{"text_to_image", "image_to_image"}
	default:
		return []string{}
	}
}

func collapseCapabilityChannelsForClient(provider model.ProviderKind, channels []proxyCapabilityChannel) []proxyCapabilityChannel {
	routes := make([]clientModelRoute, 0, len(channels))
	for _, channel := range channels {
		models := channel.Models
		if len(models) == 0 && strings.TrimSpace(channel.DefaultModel) != "" {
			models = []proxyCapabilityModel{{
				ID:              channel.DefaultModel,
				DisplayName:     channel.DefaultModel,
				SupportedModes:  nonNilStrings(channel.SupportedModes),
				SupportedScopes: nonNilStrings(channel.SupportedScopes),
			}}
		}
		for _, m := range models {
			upstreamModel := firstNonEmpty(m.ID, channel.DefaultModel)
			clientModel := publicClientModelID(provider, upstreamModel)
			if clientModel == "" {
				clientModel = upstreamModel
			}
			if strings.TrimSpace(clientModel) == "" {
				continue
			}
			scopes := nonNilStrings(m.SupportedScopes)
			if len(scopes) == 0 {
				scopes = nonNilStrings(channel.SupportedScopes)
			}
			scope := routePricingScope(scopes)
			routes = append(routes, clientModelRoute{
				Provider:              provider,
				ClientModel:           clientModel,
				DisplayName:           publicClientModelDisplayName(provider, clientModel, firstNonEmpty(m.DisplayName, upstreamModel)),
				Scope:                 scope,
				Mode:                  channel.Mode,
				CredentialID:          channel.ChannelID,
				UpstreamModel:         upstreamModel,
				SupportedModes:        nonNilStrings(firstNonEmptyStringSlice(m.SupportedModes, channel.SupportedModes)),
				SupportedScopes:       scopes,
				SupportedAspectRatios: nonNilStrings(channel.SupportedAspectRatios),
				SupportedDurations:    nonNilStrings(channel.SupportedDurations),
				IsDefault:             channel.IsDefault,
				Priority:              channel.Priority,
				SortOrder:             channel.SortOrder,
			})
		}
	}

	if len(routes) == 0 {
		return channels
	}
	sort.SliceStable(routes, lessClientModelRoute(routes))
	grouped := make(map[string][]clientModelRoute)
	var keys []string
	for _, route := range routes {
		key := clientModelRouteKey(route.Provider, route.ClientModel, route.Scope)
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], route)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		a := grouped[keys[i]][0]
		b := grouped[keys[j]][0]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.SortOrder != b.SortOrder {
			return a.SortOrder < b.SortOrder
		}
		return a.DisplayName < b.DisplayName
	})

	out := make([]proxyCapabilityChannel, 0, len(keys))
	for index, key := range keys {
		group := grouped[key]
		sort.SliceStable(group, lessClientModelRoute(group))
		first := group[0]
		modes := mergeRouteStrings(group, func(route clientModelRoute) []string { return route.SupportedModes })
		scopes := mergeRouteStrings(group, func(route clientModelRoute) []string { return route.SupportedScopes })
		aspectRatios := mergeRouteStrings(group, func(route clientModelRoute) []string { return route.SupportedAspectRatios })
		durations := mergeRouteStrings(group, func(route clientModelRoute) []string { return route.SupportedDurations })
		out = append(out, proxyCapabilityChannel{
			ChannelID:             "client-model:" + string(first.Provider) + ":" + first.ClientModel + ":" + string(first.Scope),
			ChannelName:           "后台路由",
			Mode:                  "",
			BaseURL:               "",
			DefaultModel:          first.ClientModel,
			Priority:              first.Priority,
			SortOrder:             index,
			IsDefault:             index == 0,
			Enabled:               true,
			HealthStatus:          string(model.CredentialHealthHealthy),
			SupportedModes:        modes,
			SupportedScopes:       scopes,
			SupportedAspectRatios: aspectRatios,
			SupportedDurations:    durations,
			Models: []proxyCapabilityModel{{
				ID:              first.ClientModel,
				DisplayName:     first.DisplayName,
				SupportedModes:  modes,
				SupportedScopes: scopes,
			}},
		})
	}
	return out
}

func buildClientModelRoutes(rows []model.ProviderCredential) []clientModelRoute {
	routes := make([]clientModelRoute, 0, len(rows))
	for index, row := range rows {
		if !row.Enabled || row.HealthStatus == model.CredentialHealthDown {
			continue
		}
		channel := credentialToCapabilityChannel(row, index)
		if len(channel.SupportedModes) == 0 && len(channel.SupportedScopes) == 0 {
			continue
		}
		models := channel.Models
		if len(models) == 0 && strings.TrimSpace(channel.DefaultModel) != "" {
			models = []proxyCapabilityModel{{
				ID:              channel.DefaultModel,
				DisplayName:     channel.DefaultModel,
				SupportedModes:  nonNilStrings(channel.SupportedModes),
				SupportedScopes: nonNilStrings(channel.SupportedScopes),
			}}
		}
		for _, m := range models {
			upstreamModel := firstNonEmpty(m.ID, channel.DefaultModel)
			clientModel := publicClientModelID(row.Provider, upstreamModel)
			if clientModel == "" {
				clientModel = upstreamModel
			}
			if strings.TrimSpace(clientModel) == "" {
				continue
			}
			scopes := nonNilStrings(m.SupportedScopes)
			if len(scopes) == 0 {
				scopes = nonNilStrings(channel.SupportedScopes)
			}
			routes = append(routes, clientModelRoute{
				Provider:              row.Provider,
				ClientModel:           clientModel,
				DisplayName:           publicClientModelDisplayName(row.Provider, clientModel, firstNonEmpty(m.DisplayName, upstreamModel)),
				Scope:                 routePricingScope(scopes),
				Mode:                  channel.Mode,
				CredentialID:          row.ID,
				UpstreamModel:         upstreamModel,
				SupportedModes:        nonNilStrings(firstNonEmptyStringSlice(m.SupportedModes, channel.SupportedModes)),
				SupportedScopes:       scopes,
				SupportedAspectRatios: nonNilStrings(channel.SupportedAspectRatios),
				SupportedDurations:    nonNilStrings(channel.SupportedDurations),
				IsDefault:             channel.IsDefault,
				Priority:              channel.Priority,
				SortOrder:             channel.SortOrder,
			})
		}
	}
	sort.SliceStable(routes, lessClientModelRoute(routes))
	return routes
}

func selectClientModelRoute(rows []model.ProviderCredential, provider model.ProviderKind, clientModel string, scope model.PricingScope) (*clientModelRoute, bool) {
	clientModel = normalizePublicClientModelID(provider, clientModel)
	if clientModel == "" {
		return nil, false
	}
	routes := buildClientModelRoutes(rows)
	for i := range routes {
		route := &routes[i]
		if route.Provider != provider || route.ClientModel != clientModel {
			continue
		}
		if scope != "" && route.Scope != "" && route.Scope != scope {
			continue
		}
		return route, true
	}
	return nil, false
}

func lessClientModelRoute(routes []clientModelRoute) func(i, j int) bool {
	return func(i, j int) bool {
		if routes[i].IsDefault != routes[j].IsDefault {
			return routes[i].IsDefault
		}
		if routes[i].Priority != routes[j].Priority {
			return routes[i].Priority > routes[j].Priority
		}
		if routes[i].SortOrder != routes[j].SortOrder {
			return routes[i].SortOrder < routes[j].SortOrder
		}
		return routes[i].CredentialID < routes[j].CredentialID
	}
}

func firstNonEmptyStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return []string{}
}

func mergeRouteStrings(routes []clientModelRoute, selectValues func(clientModelRoute) []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, route := range routes {
		for _, value := range selectValues(route) {
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
	}
	return out
}

func routePricingScope(scopes []string) model.PricingScope {
	for _, scope := range scopes {
		switch strings.ToLower(strings.TrimSpace(scope)) {
		case string(model.PricingScopeImage):
			return model.PricingScopeImage
		case string(model.PricingScopeVideo):
			return model.PricingScopeVideo
		case string(model.PricingScopeAnalysis):
			return model.PricingScopeAnalysis
		case string(model.PricingScopeChat), "text":
			return model.PricingScopeChat
		}
	}
	return model.PricingScopeVideo
}

func clientModelRouteKey(provider model.ProviderKind, clientModel string, scope model.PricingScope) string {
	return string(provider) + ":" + strings.ToLower(strings.TrimSpace(clientModel)) + ":" + string(scope)
}

func ensureCapabilityDefaultChannel(channels []proxyCapabilityChannel) {
	for _, channel := range channels {
		if channel.IsDefault {
			return
		}
	}
	channels[0].IsDefault = true
}

func credentialToCapabilityChannel(row model.ProviderCredential, index int) proxyCapabilityChannel {
	mode := normalizeCredentialMode(row.Provider, row.Mode)
	modelID := firstNonEmpty(row.DefaultModel, providerDefaultModel(row.Provider, mode))
	modes := nonNilStrings(supportedGenerationModes(row.Provider, mode, modelID))
	scopes := nonNilStrings(supportedProviderScopes(row.Provider, mode, modelID))
	models := capabilityModels(modelID, modes, scopes)
	return proxyCapabilityChannel{
		ChannelID:             row.ID,
		ChannelName:           row.ChannelName,
		Mode:                  mode,
		BaseURL:               row.UpstreamBase,
		DefaultModel:          modelID,
		Priority:              row.Priority,
		SortOrder:             index,
		IsDefault:             row.IsDefault,
		Enabled:               row.Enabled,
		HealthStatus:          string(row.HealthStatus),
		SupportedModes:        modes,
		SupportedScopes:       scopes,
		SupportedAspectRatios: nonNilStrings(supportedAspectRatios(row.Provider, mode)),
		SupportedDurations:    nonNilStrings(supportedDurations(row.Provider, mode)),
		Models:                models,
	}
}

func capabilityModels(modelID string, modes, scopes []string) []proxyCapabilityModel {
	if strings.TrimSpace(modelID) == "" {
		return []proxyCapabilityModel{}
	}
	return []proxyCapabilityModel{{
		ID:              modelID,
		DisplayName:     modelID,
		SupportedModes:  nonNilStrings(modes),
		SupportedScopes: nonNilStrings(scopes),
	}}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func normalizeCredentialMode(provider model.ProviderKind, mode string) string {
	return service.NormalizeProviderCredentialMode(provider, mode)
}

func supportedGenerationModes(provider model.ProviderKind, mode, modelID string) []string {
	switch provider {
	case model.ProviderGpt:
		if isGptImageModel(modelID) {
			return []string{"text_to_image", "image_to_image"}
		}
		return nil
	case model.ProviderVeo:
		if mode == "google" {
			return []string{"text_to_video", "image_to_video"}
		}
		if mode == "duoyuan" {
			return []string{"text_to_video", "image_to_video"}
		}
		return []string{"text_to_video"}
	case model.ProviderGrok:
		return []string{"text_to_video", "image_to_video"}
	case model.ProviderSora:
		if mode == "async" {
			return []string{"text_to_video"}
		}
		return nil
	default:
		return nil
	}
}

func supportedProviderScopes(provider model.ProviderKind, mode, modelID string) []string {
	switch provider {
	case model.ProviderGemini:
		return []string{"analysis"}
	case model.ProviderGpt:
		if isGptImageModel(modelID) {
			return []string{"image"}
		}
		return []string{"text"}
	case model.ProviderVeo, model.ProviderSora, model.ProviderGrok:
		if provider == model.ProviderSora && mode != "async" {
			return nil
		}
		return []string{"video"}
	case model.ProviderClaude:
		return nil
	default:
		return nil
	}
}

func supportedAspectRatios(provider model.ProviderKind, mode string) []string {
	switch provider {
	case model.ProviderGemini:
		if mode == "duoyuan" {
			return []string{"21:9", "16:9", "9:16", "5:4", "4:5", "4:3", "3:4", "3:2", "2:3", "1:1"}
		}
		return []string{"21:9", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "1:1"}
	case model.ProviderGpt:
		return []string{"16:9", "1:1", "9:16"}
	case model.ProviderVeo, model.ProviderSora:
		return []string{"16:9", "9:16", "1:1"}
	case model.ProviderGrok:
		if mode == "duoyuan" || mode == "suchuang" {
			return []string{"16:9", "9:16", "1:1", "3:2", "2:3"}
		}
		return []string{"16:9", "4:3", "3:2", "1:1", "2:3", "3:4", "9:16"}
	default:
		return nil
	}
}

func supportedDurations(provider model.ProviderKind, mode string) []string {
	switch provider {
	case model.ProviderGrok:
		switch mode {
		case "official":
			return []string{"5", "8", "10", "12", "15"}
		case "duoyuan":
			return []string{"8"}
		case "suchuang":
			return []string{"6", "10", "15", "20", "30"}
		default:
			return nil
		}
	case model.ProviderVeo, model.ProviderSora:
		return []string{"5", "8", "12"}
	default:
		return nil
	}
}

func providerDefaultModel(provider model.ProviderKind, mode string) string {
	switch provider {
	case model.ProviderGemini:
		return "gemini-2.5-flash"
	case model.ProviderGpt:
		if mode == "gzxsy" {
			return "gpt-image-2"
		}
		return ""
	case model.ProviderVeo:
		if mode == "duoyuan" {
			return "veo3"
		}
		return "veo-3.1-generate-preview"
	case model.ProviderSora:
		if mode == "async" {
			return "sora-2"
		}
		return ""
	case model.ProviderGrok:
		if mode == "duoyuan" || mode == "suchuang" {
			if mode == "duoyuan" {
				return "grok-video-3"
			}
			return "grok-video"
		}
		return "grok-imagine-video"
	case model.ProviderClaude:
		return "claude-sonnet-4"
	default:
		return ""
	}
}

func publicClientModelID(provider model.ProviderKind, upstreamModel string) string {
	return normalizePublicClientModelID(provider, upstreamModel)
}

func normalizePublicClientModelID(provider model.ProviderKind, modelID string) string {
	value := strings.ToLower(strings.TrimSpace(modelID))
	value = strings.ReplaceAll(value, "_", "-")
	switch provider {
	case model.ProviderVeo:
		switch value {
		case "veo3", "veo-3", "veo-3.1", "veo-3.1-generate-preview":
			return "veo-3.1"
		case "veo3-fast", "veo-3-fast", "veo-3.1-fast", "veo-3.1-fast-generate-preview":
			return "veo-3.1-fast"
		}
	case model.ProviderGrok:
		switch value {
		case "grok-video", "grok-video-3", "grok-imagine-video":
			return "grok-imagine"
		}
	}
	return value
}

func publicClientModelDisplayName(provider model.ProviderKind, clientModel, fallback string) string {
	switch provider {
	case model.ProviderVeo:
		switch normalizePublicClientModelID(provider, clientModel) {
		case "veo-3.1":
			return "Veo 3.1"
		case "veo-3.1-fast":
			return "Veo 3.1 Fast"
		}
	case model.ProviderGrok:
		if normalizePublicClientModelID(provider, clientModel) == "grok-imagine" {
			return "Grok Imagine"
		}
	}
	return firstNonEmpty(fallback, clientModel)
}

func isGptImageModel(modelID string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if modelID == "" {
		return false
	}
	return strings.HasPrefix(modelID, "gpt-image") ||
		strings.HasPrefix(modelID, "dall-e") ||
		strings.Contains(modelID, "image")
}

func providerDefaultBaseURL(provider model.ProviderKind) string {
	switch provider {
	case model.ProviderGemini, model.ProviderVeo:
		return "https://generativelanguage.googleapis.com"
	case model.ProviderGpt, model.ProviderSora:
		return "https://api.openai.com"
	case model.ProviderGrok:
		return "https://api.x.ai"
	case model.ProviderClaude:
		return "https://api.anthropic.com"
	default:
		return ""
	}
}

func providerDisplayName(provider model.ProviderKind) string {
	switch provider {
	case model.ProviderGemini:
		return "Gemini"
	case model.ProviderGpt:
		return "GPT"
	case model.ProviderVeo:
		return "Veo"
	case model.ProviderSora:
		return "Sora"
	case model.ProviderGrok:
		return "Grok"
	case model.ProviderClaude:
		return "Claude"
	default:
		return string(provider)
	}
}

func providerDescription(provider model.ProviderKind) string {
	switch provider {
	case model.ProviderGemini:
		return "Gemini 多模态理解和分析能力。"
	case model.ProviderGpt:
		return "GPT 图片生成和参考图编辑能力。"
	case model.ProviderVeo:
		return "Google Veo 视频生成能力。"
	case model.ProviderSora:
		return "OpenAI Sora 视频生成能力。"
	case model.ProviderGrok:
		return "xAI Grok Imagine 视频生成能力。"
	case model.ProviderClaude:
		return "Claude 文字能力。"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ===================== 后台凭证管理 =====================

type credentialRequest struct {
	Provider     string `json:"provider" binding:"required"`
	Mode         string `json:"mode" binding:"required"`
	ChannelName  string `json:"channel_name" binding:"required"`
	UpstreamBase string `json:"upstream_base" binding:"required"`
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model"`
	CustomHeader string `json:"custom_headers"`
	Enabled      *bool  `json:"enabled"`
	IsDefault    *bool  `json:"is_default"`
	Priority     *int   `json:"priority"`
	Note         string `json:"note"`
}

// AdminListCredentials GET /admin/proxy/credentials
func (h *ProxyHandler) AdminListCredentials(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	var enabled *bool
	if v := c.Query("enabled"); v != "" {
		b := v == "true" || v == "1"
		enabled = &b
	}

	rows, total, err := h.credService.List(service.ListFilter{
		TenantID: middleware.GetTenantID(c),
		Provider: c.Query("provider"),
		Mode:     c.Query("mode"),
		Enabled:  enabled,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ServerError(c, "查询失败: "+err.Error())
		return
	}

	// 列表脱敏：不返回密文 Key 字段（json:"-" 已排除，但显式声明 safer）
	list := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		list = append(list, credentialToView(&r))
	}
	response.SuccessPage(c, list, total, page, pageSize)
}

// AdminGetCredential GET /admin/proxy/credentials/:id
func (h *ProxyHandler) AdminGetCredential(c *gin.Context) {
	row, err := h.credService.GetForTenant(middleware.GetTenantID(c), c.Param("id"))
	if err != nil {
		response.NotFound(c, "凭证不存在")
		return
	}
	response.Success(c, credentialToView(row))
}

// AdminCreateCredential POST /admin/proxy/credentials
func (h *ProxyHandler) AdminCreateCredential(c *gin.Context) {
	var req credentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validateCustomHeaders(req.CustomHeader); err != nil {
		response.BadRequest(c, "自定义请求头 JSON 错误: "+err.Error())
		return
	}
	if strings.TrimSpace(req.APIKey) == "" {
		response.BadRequest(c, "API Key 不能为空")
		return
	}

	in := service.CreateInput{
		TenantID:     middleware.GetTenantID(c),
		Provider:     model.ProviderKind(strings.ToLower(strings.TrimSpace(req.Provider))),
		Mode:         req.Mode,
		ChannelName:  strings.TrimSpace(req.ChannelName),
		UpstreamBase: normalizeBaseURL(req.UpstreamBase),
		APIKey:       strings.TrimSpace(req.APIKey),
		DefaultModel: strings.TrimSpace(req.DefaultModel),
		CustomHeader: req.CustomHeader,
		Note:         strings.TrimSpace(req.Note),
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	} else {
		in.Enabled = true
	}
	if req.IsDefault != nil {
		in.IsDefault = *req.IsDefault
	}
	if req.Priority != nil {
		in.Priority = *req.Priority
	}

	row, err := h.credService.Create(in)
	if err != nil {
		if errors.Is(err, service.ErrChannelNameUnavailable) || errors.Is(err, service.ErrUnsupportedProviderCredential) {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "创建失败: "+err.Error())
		return
	}
	response.Success(c, credentialToView(row))
}

// AdminUpdateCredential PUT /admin/proxy/credentials/:id
func (h *ProxyHandler) AdminUpdateCredential(c *gin.Context) {
	var req credentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validateCustomHeaders(req.CustomHeader); err != nil {
		response.BadRequest(c, "自定义请求头 JSON 错误: "+err.Error())
		return
	}

	in := credentialUpdateInputFromRequest(req)
	if strings.TrimSpace(req.APIKey) != "" {
		k := strings.TrimSpace(req.APIKey)
		in.APIKey = &k
	}

	row, err := h.credService.UpdateForTenant(middleware.GetTenantID(c), c.Param("id"), in)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "凭证不存在")
			return
		}
		if errors.Is(err, service.ErrChannelNameUnavailable) || errors.Is(err, service.ErrUnsupportedProviderCredential) {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "更新失败: "+err.Error())
		return
	}
	response.Success(c, credentialToView(row))
}

// AdminDeleteCredential DELETE /admin/proxy/credentials/:id
func (h *ProxyHandler) AdminDeleteCredential(c *gin.Context) {
	if err := h.credService.DeleteForTenant(middleware.GetTenantID(c), c.Param("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "凭证不存在")
			return
		}
		response.ServerError(c, "删除失败: "+err.Error())
		return
	}
	response.Success(c, gin.H{"id": c.Param("id")})
}

// AdminTestCredential POST /admin/proxy/credentials/:id/test
//
// 给凭证发一个最小请求，验证 Key 是否可用 + 上游可达。
// 按 provider/mode 选择无扣费的模型列表接口，避免把 Google 原生接口误测成 OpenAI 路径。
func (h *ProxyHandler) AdminTestCredential(c *gin.Context) {
	row, err := h.credService.GetForTenant(middleware.GetTenantID(c), c.Param("id"))
	if err != nil {
		response.NotFound(c, "凭证不存在")
		return
	}
	if err := service.ValidateProviderCredentialProvider(row.Provider); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	keyBytes, err := h.credService.Decrypt(row)
	if err != nil {
		response.ServerError(c, "解密 Key 失败: "+err.Error())
		return
	}
	defer zeroBytes(keyBytes)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	req, err := buildCredentialProbeRequest(ctx, row, keyBytes)
	if err != nil {
		response.ServerError(c, "构造探测请求失败: "+err.Error())
		return
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		_ = h.credService.MarkHealth(row.ID, model.CredentialHealthDown)
		response.Success(c, gin.H{
			"ok":           false,
			"reason":       "网络错误：" + err.Error(),
			"latency_ms":   latency,
			"probe_url":    safeProbeURL(req),
			"probe_method": req.Method,
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300

	health := model.CredentialHealthHealthy
	if !ok {
		health = model.CredentialHealthDegraded
	}
	_ = h.credService.MarkHealth(row.ID, health)

	response.Success(c, gin.H{
		"ok":              ok,
		"http_status":     resp.StatusCode,
		"latency_ms":      latency,
		"upstream_sample": truncate(string(body), 256),
		"probe_url":       safeProbeURL(req),
		"probe_method":    req.Method,
		"reason":          credentialProbeReason(resp.StatusCode, body),
	})
}

func buildCredentialProbeRequest(ctx context.Context, row *model.ProviderCredential, keyBytes []byte) (*http.Request, error) {
	if row == nil {
		return nil, errors.New("凭证为空")
	}
	baseURL := strings.TrimRight(row.UpstreamBase, "/")
	if baseURL == "" {
		return nil, errors.New("上游 Base URL 为空")
	}
	mode := normalizeCredentialMode(row.Provider, row.Mode)

	var req *http.Request
	var err error
	switch row.Provider {
	case model.ProviderGemini:
		req, err = newGoogleModelsProbeRequest(ctx, baseURL, keyBytes)
	case model.ProviderVeo:
		if mode == "google" {
			req, err = newGoogleModelsProbeRequest(ctx, baseURL, keyBytes)
		} else {
			req, err = newBearerModelsProbeRequest(ctx, baseURL, keyBytes)
		}
	case model.ProviderClaude:
		req, err = newClaudeModelsProbeRequest(ctx, baseURL, keyBytes)
	default:
		req, err = newBearerModelsProbeRequest(ctx, baseURL, keyBytes)
	}
	if err != nil {
		return nil, err
	}
	applyProbeCustomHeaders(req, row.CustomHeader)
	return req, nil
}

func newGoogleModelsProbeRequest(ctx context.Context, baseURL string, keyBytes []byte) (*http.Request, error) {
	probeURL, err := url.Parse(baseURL + "/v1beta/models")
	if err != nil {
		return nil, err
	}
	q := probeURL.Query()
	q.Set("key", string(keyBytes))
	probeURL.RawQuery = q.Encode()
	return http.NewRequestWithContext(ctx, http.MethodGet, probeURL.String(), nil)
}

func newBearerModelsProbeRequest(ctx context.Context, baseURL string, keyBytes []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(keyBytes))
	return req, nil
}

func newClaudeModelsProbeRequest(ctx context.Context, baseURL string, keyBytes []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", string(keyBytes))
	req.Header.Set("anthropic-version", "2023-06-01")
	return req, nil
}

func applyProbeCustomHeaders(req *http.Request, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return
	}
	headers := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return
	}
	for key, value := range headers {
		if isReservedCustomHeader(key) {
			continue
		}
		req.Header.Set(key, fmt.Sprint(value))
	}
}

func safeProbeURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	copied := *req.URL
	q := copied.Query()
	if q.Has("key") {
		q.Set("key", "***")
		copied.RawQuery = q.Encode()
	}
	return copied.String()
}

func credentialProbeReason(status int, body []byte) string {
	if status >= 200 && status < 300 {
		return ""
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "上游拒绝访问，通常是 API Key 或权限不正确"
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return "上游不支持当前探测接口，凭证未必不可用；可用真实生成请求进一步验证"
	default:
		if len(body) > 0 {
			return "上游返回非 2xx: " + truncate(string(body), 200)
		}
		return "上游返回非 2xx"
	}
}

// ===================== 客户端转发：Chat =====================

// chatRequest 客户端发起 chat 时的轻量包装。Body 是 OpenAI 风格的原始请求体。
// 客户端可以在 body 里加 "mode" 字段指定接入方式，也可以放在 query 里 ?mode=xxx。
type chatRequest = json.RawMessage

// Chat POST /api/proxy/:provider/chat
func (h *ProxyHandler) Chat(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}

	provider := strings.ToLower(c.Param("provider"))
	providerKind := model.ProviderKind(provider)

	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.BadRequest(c, "读取请求体失败: "+err.Error())
		return
	}

	mode, scope := extractChatModeAndScope(rawBody, c)
	mode = resolveProxyRequestMode(providerKind, mode)
	appID, err := resolveProxyAppID(c, rawBody)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	out, err := h.proxy.Chat(c.Request.Context(), service.ChatInput{
		UserID:       userID,
		TenantID:     middleware.GetTenantID(c),
		AppID:        appID,
		Provider:     providerKind,
		Mode:         mode,
		Scope:        scope,
		CredentialID: extractCredentialID(rawBody, c),
		Body:         rawBody,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInsufficientBalance):
			c.JSON(402, gin.H{"code": 402, "message": "余额不足"})
			return
		case errors.Is(err, service.ErrConcurrentLimit):
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "message": "超过并发上限"})
			return
		case errors.Is(err, service.ErrNoPricingRule):
			response.BadRequest(c, "未配置计价规则: "+err.Error())
			return
		case errors.Is(err, service.ErrAdapterNotFound):
			response.BadRequest(c, "Provider 暂不支持: "+err.Error())
			return
		case errors.Is(err, service.ErrCredentialUnavailable):
			response.BadRequest(c, "渠道不可用或不匹配: "+err.Error())
			return
		default:
			response.ServerError(c, err.Error())
			return
		}
	}

	// 透传上游 Content-Type；其它头不透传，避免 Set-Cookie 之类穿透。
	if ct := out.UpstreamHdrs.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Header("X-Task-Id", out.TaskID)
	c.Header("X-Cost", strconv.Itoa(out.Cost))
	c.Data(out.HTTPStatus, "application/json", out.UpstreamBody)
}

// Generate POST /api/proxy/:provider/generate
//
// 异步路径：客户端立即拿到 task_id；后台 poller 推进；客户端通过 GET /api/proxy/tasks/:id
// 或 WebSocket 收到完成事件后再下载文件。
//
// 客户端 body 形态（透传到上游 + 抓取计价字段）：
//
//	{
//	  "mode": "official",                  // 接入方式
//	  "scope": "video",                    // image|video，默认 video
//	  "model": "veo-3.0-generate-preview",
//	  "prompt": "a cat dancing",
//	  "duration_seconds": 5,
//	  "aspect_ratio": "16:9",
//	  ... 其它上游字段
//	}
func (h *ProxyHandler) Generate(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	provider := strings.ToLower(c.Param("provider"))
	providerKind := model.ProviderKind(provider)

	rawBody, persistedBody, cleanup, err := readGeneratePayload(c, userID)
	if err != nil {
		response.BadRequest(c, "读取请求体失败: "+err.Error())
		return
	}
	mode, scope := extractModeAndScope(rawBody, c)
	mode = resolveProxyRequestMode(providerKind, mode)
	credentialID := extractCredentialID(rawBody, c)
	clientModel := extractClientModel(rawBody, c)
	if clientModel != "" {
		if h.credService == nil {
			cleanup()
			response.ServerError(c, "Provider 凭证服务未初始化")
			return
		}
		tenantID := middleware.GetTenantID(c)
		var routeMode string
		var routeCredentialID string
		var routeUpstreamModel string
		var routeClientModel string
		ok := false
		if h.clientModelService != nil {
			cm, configuredRoute, err := h.clientModelService.SelectRoute(tenantID, providerKind, clientModel, scope)
			if err == nil && cm != nil && configuredRoute != nil && configuredRoute.Credential != nil {
				routeMode = service.NormalizeProviderCredentialMode(configuredRoute.Credential.Provider, configuredRoute.Credential.Mode)
				routeCredentialID = configuredRoute.CredentialID
				routeUpstreamModel = configuredRoute.UpstreamModel
				routeClientModel = cm.ModelKey
				ok = true
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				cleanup()
				response.ServerError(c, "查询客户端模型路由失败: "+err.Error())
				return
			}
		}
		if !ok {
			rows, err := h.listEnabledCredentials(tenantID)
			if err != nil {
				cleanup()
				response.ServerError(c, "查询模型路由失败: "+err.Error())
				return
			}
			route, found := selectClientModelRoute(rows, providerKind, clientModel, scope)
			if found {
				routeMode = route.Mode
				routeCredentialID = route.CredentialID
				routeUpstreamModel = route.UpstreamModel
				routeClientModel = route.ClientModel
				ok = true
			}
		}
		if !ok {
			cleanup()
			response.BadRequest(c, "客户端模型未配置可用渠道: "+clientModel)
			return
		}
		mode = routeMode
		credentialID = routeCredentialID
		rawBody, persistedBody, err = rewriteGenerateModel(rawBody, persistedBody, routeUpstreamModel, routeClientModel)
		if err != nil {
			cleanup()
			response.BadRequest(c, "处理客户端模型失败: "+err.Error())
			return
		}
	}
	appID, err := resolveProxyAppID(c, rawBody)
	if err != nil {
		cleanup()
		response.BadRequest(c, err.Error())
		return
	}

	out, err := h.async.StartTask(c.Request.Context(), service.StartInput{
		UserID:        userID,
		TenantID:      middleware.GetTenantID(c),
		AppID:         appID,
		Provider:      providerKind,
		Mode:          mode,
		Scope:         scope,
		CredentialID:  credentialID,
		Body:          rawBody,
		PersistedBody: persistedBody,
	})
	cleanup()
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInsufficientBalance):
			c.JSON(402, gin.H{"code": 402, "message": "余额不足"})
			return
		case errors.Is(err, service.ErrConcurrentLimit):
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "message": "超过并发上限"})
			return
		case errors.Is(err, service.ErrNoPricingRule):
			response.BadRequest(c, "未配置计价规则: "+err.Error())
			return
		case errors.Is(err, service.ErrAdapterNotFound):
			response.BadRequest(c, "Provider 暂不支持异步生成: "+err.Error())
			return
		case errors.Is(err, service.ErrCredentialUnavailable):
			response.BadRequest(c, "渠道不可用或不匹配: "+err.Error())
			return
		default:
			response.ServerError(c, err.Error())
			return
		}
	}

	response.Success(c, gin.H{
		"task_id": out.TaskID,
		"cost":    out.Cost,
		"status":  "running",
	})
}

func extractModeAndScope(body []byte, c *gin.Context) (string, model.PricingScope) {
	return extractModeAndScopeWithDefault(body, c, model.PricingScopeVideo)
}

func extractChatModeAndScope(body []byte, c *gin.Context) (string, model.PricingScope) {
	return extractModeAndScopeWithDefault(body, c, model.PricingScopeChat)
}

func extractModeAndScopeWithDefault(body []byte, c *gin.Context, defaultScope model.PricingScope) (string, model.PricingScope) {
	mode := c.Query("mode")
	scope := model.PricingScope(c.Query("scope"))
	var probe map[string]any
	if json.Unmarshal(body, &probe) == nil {
		if mode == "" {
			if v, ok := probe["mode"].(string); ok {
				mode = v
			}
		}
		if scope == "" {
			if v, ok := probe["scope"].(string); ok {
				scope = model.PricingScope(v)
			}
		}
	}
	if scope == "" {
		scope = defaultScope
	}
	return mode, scope
}

func resolveProxyRequestMode(provider model.ProviderKind, mode string) string {
	return service.NormalizeProviderCredentialMode(provider, mode)
}

func extractCredentialID(body []byte, c *gin.Context) string {
	id := firstNonEmpty(c.Query("channel_id"), c.Query("credential_id"))
	if id == "" {
		var probe map[string]any
		if json.Unmarshal(body, &probe) == nil {
			if v, ok := probe["channel_id"].(string); ok {
				id = v
			} else if v, ok := probe["credential_id"].(string); ok {
				id = v
			}
		}
	}
	id = strings.TrimSpace(id)
	if strings.HasPrefix(strings.ToLower(id), "backend-proxy-") {
		return ""
	}
	return id
}

func extractClientModel(body []byte, c *gin.Context) string {
	modelID := firstNonEmpty(c.Query("client_model"), c.Query("public_model"))
	if modelID == "" {
		var probe map[string]any
		if json.Unmarshal(body, &probe) == nil {
			if v, ok := probe["client_model"].(string); ok {
				modelID = v
			} else if v, ok := probe["public_model"].(string); ok {
				modelID = v
			}
		}
	}
	return strings.TrimSpace(modelID)
}

func rewriteGenerateModel(body []byte, persistedBody []byte, upstreamModel string, clientModel string) ([]byte, []byte, error) {
	rewrittenBody, err := rewriteJSONModel(body, upstreamModel, clientModel)
	if err != nil {
		return nil, nil, err
	}
	if len(persistedBody) == 0 {
		return rewrittenBody, rewrittenBody, nil
	}
	rewrittenPersisted, err := rewriteJSONModel(persistedBody, upstreamModel, clientModel)
	if err != nil {
		return nil, nil, err
	}
	return rewrittenBody, rewrittenPersisted, nil
}

func rewriteJSONModel(body []byte, upstreamModel string, clientModel string) ([]byte, error) {
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, err
	}
	if strings.TrimSpace(upstreamModel) != "" {
		probe["model"] = strings.TrimSpace(upstreamModel)
	}
	if strings.TrimSpace(clientModel) != "" {
		probe["client_model"] = strings.TrimSpace(clientModel)
	}
	return json.Marshal(probe)
}

func resolveProxyAppID(c *gin.Context, body []byte) (string, error) {
	sessionAppID := middleware.GetClientAppID(c)
	if sessionAppID != "" {
		return sessionAppID, nil
	}

	appID := firstNonEmpty(c.Query("app_id"))
	if appID == "" {
		var probe map[string]any
		if json.Unmarshal(body, &probe) == nil {
			if v, ok := probe["app_id"].(string); ok {
				appID = strings.TrimSpace(v)
			}
		}
	}
	if appID == "" {
		var app model.Application
		if err := model.DB.
			Where("tenant_id = ? AND status = ?", middleware.GetTenantID(c), model.AppStatusActive).
			Order("created_at ASC").
			First(&app).Error; err == nil {
			return app.ID, nil
		}
		return middleware.GetTenantID(c), nil
	}

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, middleware.GetTenantID(c)).Error; err != nil {
		return "", errors.New("无效的应用")
	}
	return app.ID, nil
}

// ===================== 内部辅助 =====================

func credentialToView(r *model.ProviderCredential) gin.H {
	view := gin.H{
		"id":             r.ID,
		"tenant_id":      r.TenantID,
		"provider":       r.Provider,
		"mode":           r.Mode,
		"channel_name":   r.ChannelName,
		"upstream_base":  r.UpstreamBase,
		"default_model":  r.DefaultModel,
		"custom_headers": maskCustomHeaders(r.CustomHeader),
		"enabled":        r.Enabled,
		"is_default":     r.IsDefault,
		"priority":       r.Priority,
		"health_status":  r.HealthStatus,
		"last_used_at":   r.LastUsedAt,
		"key_id":         r.KeyID,
		"enc_alg":        r.EncAlg,
		"note":           r.Note,
		"created_at":     r.CreatedAt,
		"updated_at":     r.UpdatedAt,
	}
	// 仅返回 Key 长度与首尾 4 字符提示，不返回明文与密文
	if l := len(r.APIKeyEnc); l > 0 {
		view["api_key_set"] = true
		view["api_key_cipher_size"] = l
	} else {
		view["api_key_set"] = false
	}
	return view
}

func maskCustomHeaders(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return "{}"
	}
	headers := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return "{}"
	}
	for key, value := range headers {
		if isSensitiveCustomHeader(key) {
			headers[key] = "***"
			continue
		}
		if s, ok := value.(string); ok && looksSensitiveHeaderValue(s) {
			headers[key] = maskSecretPreview(s)
		}
	}
	data, err := json.Marshal(headers)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func isSensitiveCustomHeader(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "key") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "cookie")
}

func looksSensitiveHeaderValue(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) >= 24 {
		return true
	}
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "bearer ") ||
		strings.HasPrefix(lower, "sk-") ||
		strings.HasPrefix(lower, "xai-")
}

func maskSecretPreview(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "***" + value[len(value)-4:]
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optionalStringTrim(s string) *string {
	t := normalizeBaseURL(s)
	if t == "" {
		return nil
	}
	return &t
}

func optionalStringTrimSpace(s string) *string {
	t := strings.TrimSpace(s)
	return &t
}

func normalizeBaseURL(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

func credentialUpdateInputFromRequest(req credentialRequest) service.UpdateInput {
	return service.UpdateInput{
		Mode:         optionalStringTrimSpace(req.Mode),
		ChannelName:  optionalStringTrimSpace(req.ChannelName),
		UpstreamBase: optionalStringTrim(req.UpstreamBase),
		DefaultModel: optionalStringTrimSpace(req.DefaultModel),
		CustomHeader: optionalStringTrimSpace(req.CustomHeader),
		Enabled:      req.Enabled,
		IsDefault:    req.IsDefault,
		Priority:     req.Priority,
		Note:         optionalStringTrimSpace(req.Note),
	}
}

func validateCustomHeaders(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	headers := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return err
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			return errors.New("请求头名称不能为空")
		}
		if isReservedCustomHeader(key) {
			return fmt.Errorf("请求头 %q 由系统管理，不能在自定义请求头里覆盖", key)
		}
		switch value.(type) {
		case string, float64, bool, nil:
		default:
			return fmt.Errorf("请求头 %q 的值必须是字符串、数字或布尔值", key)
		}
	}
	return nil
}

func isReservedCustomHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "content-type", "content-length", "host":
		return true
	default:
		return false
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
