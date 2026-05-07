package service

import (
	"strings"

	"license-server/internal/model"
)

type UpstreamModelCapability struct {
	Provider     model.ProviderKind `json:"provider"`
	Mode         string             `json:"mode"`
	Model        string             `json:"model"`
	DisplayName  string             `json:"display_name"`
	AspectRatios []string           `json:"aspect_ratios"`
	Durations    []string           `json:"durations"`
	Resolutions  []string           `json:"resolutions"`
	MaxImages    int                `json:"max_images"`
	Note         string             `json:"note"`
}

func ListUpstreamModelCapabilities(provider model.ProviderKind, mode string) []UpstreamModelCapability {
	provider = model.ProviderKind(strings.ToLower(strings.TrimSpace(string(provider))))
	mode = NormalizeProviderCredentialMode(provider, mode)
	out := []UpstreamModelCapability{}
	for _, item := range builtinUpstreamModelCapabilities {
		if item.Provider != provider {
			continue
		}
		if mode != "" && item.Mode != mode {
			continue
		}
		out = append(out, item)
	}
	return out
}

func FindUpstreamModelCapability(provider model.ProviderKind, mode, upstreamModel string) (UpstreamModelCapability, bool) {
	upstreamModel = normalizeCapabilityModelKey(upstreamModel)
	for _, item := range ListUpstreamModelCapabilities(provider, mode) {
		if normalizeCapabilityModelKey(item.Model) == upstreamModel {
			return item, true
		}
	}
	return UpstreamModelCapability{}, false
}

func ResolveRouteAspectRatios(route model.ClientModelRoute) []string {
	if values := ParseClientModelJSONStrings(route.AspectRatios); len(values) > 0 {
		return values
	}
	if route.Credential != nil {
		if cap, ok := FindUpstreamModelCapability(route.Credential.Provider, route.Credential.Mode, route.UpstreamModel); ok {
			return cap.AspectRatios
		}
	}
	return []string{}
}

func ResolveRouteDurations(route model.ClientModelRoute) []string {
	if values := ParseClientModelJSONStrings(route.Durations); len(values) > 0 {
		return values
	}
	if route.Credential != nil {
		if cap, ok := FindUpstreamModelCapability(route.Credential.Provider, route.Credential.Mode, route.UpstreamModel); ok {
			return cap.Durations
		}
	}
	return []string{}
}

func ResolveRouteResolutions(route model.ClientModelRoute) []string {
	if values := ParseClientModelJSONStrings(route.Resolutions); len(values) > 0 {
		return values
	}
	if route.Credential != nil {
		if cap, ok := FindUpstreamModelCapability(route.Credential.Provider, route.Credential.Mode, route.UpstreamModel); ok {
			return cap.Resolutions
		}
	}
	return []string{}
}

func ResolveRouteMaxImages(route model.ClientModelRoute) int {
	if route.MaxImages > 0 {
		return route.MaxImages
	}
	if route.Credential != nil {
		if cap, ok := FindUpstreamModelCapability(route.Credential.Provider, route.Credential.Mode, route.UpstreamModel); ok {
			return cap.MaxImages
		}
	}
	return 0
}

func normalizeCapabilityModelKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

var builtinUpstreamModelCapabilities = []UpstreamModelCapability{
	{
		Provider:     model.ProviderVeo,
		Mode:         "duoyuan",
		Model:        "veo_3_1",
		DisplayName:  "Veo 3.1",
		AspectRatios: []string{"16:9", "9:16"},
		Durations:    []string{"8"},
		Resolutions:  []string{"1080p"},
		MaxImages:    3,
		Note:         "多元 Veo 3.1，支持文生视频和参考图生成。",
	},
	{
		Provider:     model.ProviderVeo,
		Mode:         "duoyuan",
		Model:        "veo_3_1-fast",
		DisplayName:  "Veo 3.1 Fast",
		AspectRatios: []string{"16:9", "9:16"},
		Durations:    []string{"8"},
		Resolutions:  []string{"1080p"},
		MaxImages:    3,
		Note:         "多元 Veo 3.1 Fast。",
	},
	{
		Provider:     model.ProviderVeo,
		Mode:         "duoyuan",
		Model:        "veo_3_1-4K",
		DisplayName:  "Veo 3.1 4K",
		AspectRatios: []string{"16:9", "9:16"},
		Durations:    []string{"8"},
		Resolutions:  []string{"4K"},
		MaxImages:    3,
		Note:         "多元 Veo 3.1 4K。",
	},
	{
		Provider:     model.ProviderVeo,
		Mode:         "duoyuan",
		Model:        "veo_3_1-components",
		DisplayName:  "Veo 3.1 Components",
		AspectRatios: []string{"16:9", "9:16"},
		Durations:    []string{"8"},
		Resolutions:  []string{"1080p"},
		MaxImages:    3,
		Note:         "多元 Veo 组件模式。",
	},
	{
		Provider:     model.ProviderGrok,
		Mode:         "duoyuan",
		Model:        "grok-video-3",
		DisplayName:  "Grok Video 3",
		AspectRatios: []string{"16:9", "9:16", "1:1", "3:2", "2:3"},
		Durations:    []string{"8"},
		Resolutions:  []string{"720p", "1080p"},
		MaxImages:    7,
		Note:         "多元 Grok 视频模型。",
	},
}
