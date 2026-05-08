package handler

import (
	"encoding/json"
	"testing"
	"time"

	"license-server/internal/model"
	"license-server/internal/service"
)

func TestBuildProxyCapabilitiesExposesEnabledOfficialGrok(t *testing.T) {
	got := buildProxyCapabilities([]model.ProviderCredential{
		{
			BaseModel: model.BaseModel{
				ID:        "grok-official",
				CreatedAt: time.Unix(2, 0),
			},
			Provider:     model.ProviderGrok,
			Mode:         "official",
			ChannelName:  "Grok 官方",
			UpstreamBase: "https://example.test",
			DefaultModel: "grok-imagine-video",
			Enabled:      true,
			Priority:     10,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel: model.BaseModel{
				ID:        "grok-down",
				CreatedAt: time.Unix(1, 0),
			},
			Provider:     model.ProviderGrok,
			Mode:         "official",
			ChannelName:  "Grok Down",
			UpstreamBase: "https://down.example.test",
			Enabled:      true,
			HealthStatus: model.CredentialHealthDown,
		},
	})

	if len(got.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(got.Providers))
	}
	provider := got.Providers[0]
	if provider.Provider != "grok" {
		t.Fatalf("provider = %q, want grok", provider.Provider)
	}
	if len(provider.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(provider.Channels))
	}

	channel := provider.Channels[0]
	if channel.ChannelID != "client-model:grok:grok-imagine:video" {
		t.Fatalf("channel_id = %q", channel.ChannelID)
	}
	if channel.Mode != "" {
		t.Fatalf("mode = %q, want empty client route mode", channel.Mode)
	}
	if !channel.IsDefault {
		t.Fatal("first available channel should be default")
	}
	if !containsString(channel.SupportedModes, "image_to_video") {
		t.Fatalf("supported_modes = %#v, want image_to_video", channel.SupportedModes)
	}
	if !containsString(channel.SupportedDurations, "15") {
		t.Fatalf("supported_durations = %#v, want 15", channel.SupportedDurations)
	}
}

func TestBuildProxyCapabilitiesExposesGrokThirdPartyModes(t *testing.T) {
	got := buildProxyCapabilities([]model.ProviderCredential{
		{
			BaseModel: model.BaseModel{
				ID:        "grok-duoyuan",
				CreatedAt: time.Unix(1, 0),
			},
			Provider:     model.ProviderGrok,
			Mode:         "duoyuan",
			ChannelName:  "Grok 多元",
			UpstreamBase: "https://duoyuan.example.test",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel: model.BaseModel{
				ID:        "grok-suchuang",
				CreatedAt: time.Unix(2, 0),
			},
			Provider:     model.ProviderGrok,
			Mode:         "suchuang",
			ChannelName:  "Grok 速创",
			UpstreamBase: "https://suchuang.example.test",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
	})

	provider := findCapabilityProvider(got, "grok")
	if provider == nil {
		t.Fatal("grok provider should be exposed")
	}
	if len(provider.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(provider.Channels))
	}
	duoyuan := provider.Channels[0]
	if duoyuan.Mode != "" {
		t.Fatalf("client route mode = %q, want empty", duoyuan.Mode)
	}
	if duoyuan.DefaultModel != "grok-imagine" {
		t.Fatalf("client default_model = %q, want grok-imagine", duoyuan.DefaultModel)
	}
	if !containsString(duoyuan.SupportedModes, "image_to_video") {
		t.Fatalf("duoyuan supported_modes = %#v, want image_to_video", duoyuan.SupportedModes)
	}
	if !containsString(duoyuan.SupportedDurations, "8") {
		t.Fatalf("duoyuan supported_durations = %#v, want 8", duoyuan.SupportedDurations)
	}
	if !containsString(duoyuan.SupportedDurations, "30") {
		t.Fatalf("merged supported_durations = %#v, want 30", duoyuan.SupportedDurations)
	}
}

func TestBuildProxyCapabilitiesDoesNotExposeUnsupportedGenerationModes(t *testing.T) {
	got := buildProxyCapabilities([]model.ProviderCredential{
		{
			BaseModel: model.BaseModel{
				ID:        "sora-chat",
				CreatedAt: time.Unix(1, 0),
			},
			Provider:     model.ProviderSora,
			Mode:         "chat",
			ChannelName:  "Sora Chat",
			UpstreamBase: "https://sora.example.test",
			DefaultModel: "sora-2",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel: model.BaseModel{
				ID:        "veo-duoyuan",
				CreatedAt: time.Unix(2, 0),
			},
			Provider:     model.ProviderVeo,
			Mode:         "duoyuan",
			ChannelName:  "Veo 多元",
			UpstreamBase: "https://veo.example.test",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel: model.BaseModel{
				ID:        "gpt-text",
				CreatedAt: time.Unix(3, 0),
			},
			Provider:     model.ProviderGpt,
			Mode:         "official",
			ChannelName:  "GPT 文本",
			UpstreamBase: "https://gpt.example.test",
			DefaultModel: "gpt-4o-mini",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
	})

	if provider := findCapabilityProvider(got, "sora"); provider != nil {
		t.Fatalf("sora chat should not be exposed as generation capability: %#v", provider.Channels)
	}

	veo := findCapabilityProvider(got, "veo")
	if veo == nil || len(veo.Channels) != 1 {
		t.Fatalf("veo provider/channels = %#v", veo)
	}
	if !containsString(veo.Channels[0].SupportedModes, "text_to_video") {
		t.Fatalf("veo modes = %#v, want text_to_video", veo.Channels[0].SupportedModes)
	}
	if !containsString(veo.Channels[0].SupportedModes, "image_to_video") {
		t.Fatalf("veo duoyuan should expose image_to_video: %#v", veo.Channels[0].SupportedModes)
	}
	if veo.Channels[0].DefaultModel != "veo-3.1" {
		t.Fatalf("veo client default_model = %q, want veo-3.1", veo.Channels[0].DefaultModel)
	}

	gpt := findCapabilityProvider(got, "gpt")
	if gpt == nil || len(gpt.Channels) != 1 {
		t.Fatalf("gpt provider/channels = %#v", gpt)
	}
	if len(gpt.Channels[0].SupportedModes) != 0 {
		t.Fatalf("gpt text model should not expose image generation modes: %#v", gpt.Channels[0].SupportedModes)
	}
	if !containsString(gpt.Channels[0].SupportedScopes, "text") {
		t.Fatalf("gpt text scopes = %#v, want text", gpt.Channels[0].SupportedScopes)
	}
}

func TestBuildProxyCapabilitiesUsesEmptyArraysForOptionalLists(t *testing.T) {
	got := buildProxyCapabilities([]model.ProviderCredential{
		{
			BaseModel: model.BaseModel{
				ID:        "gpt-text",
				CreatedAt: time.Unix(1, 0),
			},
			Provider:     model.ProviderGpt,
			Mode:         "official",
			ChannelName:  "GPT Text",
			UpstreamBase: "https://gpt.example.test",
			DefaultModel: "gpt-4o-mini",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
	})

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if containsString([]string{string(raw)}, "null") {
		t.Fatalf("capabilities should expose empty arrays instead of null: %s", raw)
	}

	gpt := findCapabilityProvider(got, "gpt")
	if gpt == nil || len(gpt.Channels) != 1 {
		t.Fatalf("gpt provider/channels = %#v", gpt)
	}
	channel := gpt.Channels[0]
	if channel.SupportedModes == nil {
		t.Fatal("supported_modes should be an empty slice, not nil")
	}
	if channel.SupportedAspectRatios == nil {
		t.Fatal("supported_aspect_ratios should be an empty slice, not nil")
	}
	if channel.SupportedDurations == nil {
		t.Fatal("supported_durations should be an empty slice, not nil")
	}
	if len(channel.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(channel.Models))
	}
	if channel.Models[0].SupportedModes == nil {
		t.Fatal("model supported_modes should be an empty slice, not nil")
	}
}

func TestBuildProxyCapabilitiesDefaultsFirstVisibleChannel(t *testing.T) {
	got := buildProxyCapabilities([]model.ProviderCredential{
		{
			BaseModel: model.BaseModel{
				ID:        "sora-chat",
				CreatedAt: time.Unix(1, 0),
			},
			Provider:     model.ProviderSora,
			Mode:         "chat",
			ChannelName:  "Sora Chat",
			UpstreamBase: "https://sora-chat.example.test",
			DefaultModel: "sora-2",
			Enabled:      true,
			IsDefault:    true,
			Priority:     100,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel: model.BaseModel{
				ID:        "sora-async",
				CreatedAt: time.Unix(2, 0),
			},
			Provider:     model.ProviderSora,
			Mode:         "async",
			ChannelName:  "Sora Async",
			UpstreamBase: "https://sora-async.example.test",
			DefaultModel: "sora-2",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
	})

	sora := findCapabilityProvider(got, "sora")
	if sora == nil || len(sora.Channels) != 1 {
		t.Fatalf("sora provider/channels = %#v", sora)
	}
	if sora.Channels[0].ChannelID != "client-model:sora:sora-2:video" {
		t.Fatalf("channel_id = %q, want client-model:sora:sora-2:video", sora.Channels[0].ChannelID)
	}
	if !sora.Channels[0].IsDefault {
		t.Fatal("first visible channel should be default")
	}
}

func TestBuildProxyCapabilitiesCollapsesThirdPartyRoutesToClientModels(t *testing.T) {
	got := buildProxyCapabilities([]model.ProviderCredential{
		{
			BaseModel:    model.BaseModel{ID: "veo-duoyuan", CreatedAt: time.Unix(1, 0)},
			Provider:     model.ProviderVeo,
			Mode:         "duoyuan",
			ChannelName:  "Veo 多元",
			UpstreamBase: "https://duoyuan.example.test",
			DefaultModel: "veo3",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel:    model.BaseModel{ID: "grok-duoyuan", CreatedAt: time.Unix(2, 0)},
			Provider:     model.ProviderGrok,
			Mode:         "duoyuan",
			ChannelName:  "Grok 多元",
			UpstreamBase: "https://duoyuan.example.test",
			DefaultModel: "grok-video-3",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel:    model.BaseModel{ID: "grok-suchuang", CreatedAt: time.Unix(3, 0)},
			Provider:     model.ProviderGrok,
			Mode:         "suchuang",
			ChannelName:  "Grok 速创",
			UpstreamBase: "https://suchuang.example.test",
			DefaultModel: "grok-video",
			Enabled:      true,
			HealthStatus: model.CredentialHealthUnknown,
		},
	})

	veo := findCapabilityProvider(got, "veo")
	if veo == nil || len(veo.Channels) != 1 {
		t.Fatalf("veo provider/channels = %#v", veo)
	}
	if veo.Channels[0].ChannelName != "后台路由" || veo.Channels[0].DefaultModel != "veo-3.1" {
		t.Fatalf("veo client channel = %#v", veo.Channels[0])
	}
	if veo.Channels[0].Models[0].DisplayName != "Veo 3.1" {
		t.Fatalf("veo model display = %q", veo.Channels[0].Models[0].DisplayName)
	}

	grok := findCapabilityProvider(got, "grok")
	if grok == nil || len(grok.Channels) != 1 {
		t.Fatalf("grok provider/channels = %#v", grok)
	}
	if grok.Channels[0].DefaultModel != "grok-imagine" {
		t.Fatalf("grok client model = %q", grok.Channels[0].DefaultModel)
	}
	if grok.Channels[0].Models[0].DisplayName != "Grok Imagine" {
		t.Fatalf("grok model display = %q", grok.Channels[0].Models[0].DisplayName)
	}
	if !containsString(grok.Channels[0].SupportedDurations, "30") {
		t.Fatalf("grok durations should merge routes: %#v", grok.Channels[0].SupportedDurations)
	}
}

func TestBuildConfiguredProxyCapabilitiesUsesAdminClientModels(t *testing.T) {
	got := buildConfiguredProxyCapabilities([]service.ClientModelWithRoutes{
		{
			Model: model.ClientModel{
				BaseModel:       model.BaseModel{ID: "cm-1"},
				TenantID:        "tenant-1",
				ModelKey:        "my-public-video",
				DisplayName:     "我的视频模型",
				Provider:        model.ProviderGrok,
				Scope:           model.PricingScopeVideo,
				Enabled:         true,
				SortOrder:       3,
				SupportedModes:  `["text_to_video"]`,
				SupportedScopes: `["video"]`,
				AspectRatios:    `["4:3"]`,
				Durations:       `["30"]`,
			},
			Routes: []model.ClientModelRoute{
				{
					BaseModel:     model.BaseModel{ID: "route-1"},
					TenantID:      "tenant-1",
					ClientModelID: "cm-1",
					CredentialID:  "cred-1",
					UpstreamModel: "grok-video-3",
					Enabled:       true,
					Credential: &model.ProviderCredential{
						BaseModel:    model.BaseModel{ID: "cred-1"},
						Provider:     model.ProviderGrok,
						Mode:         "duoyuan",
						ChannelName:  "多元",
						Enabled:      true,
						HealthStatus: model.CredentialHealthUnknown,
						DefaultModel: "grok-video-3",
						UpstreamBase: "https://example.test",
						CustomHeader: "{}",
						APIKeyEnc:    []byte("x"),
						DEKEnc:       []byte("x"),
						EncAlg:       "AES-256-GCM",
						KeyID:        "test",
					},
				},
			},
		},
	})

	grok := findCapabilityProvider(got, "grok")
	if grok == nil || len(grok.Channels) != 1 {
		t.Fatalf("grok provider/channels = %#v", grok)
	}
	channel := grok.Channels[0]
	if channel.ChannelID != "client-model:grok:my-public-video:video" {
		t.Fatalf("channel_id = %q", channel.ChannelID)
	}
	if channel.DefaultModel != "my-public-video" {
		t.Fatalf("default_model = %q", channel.DefaultModel)
	}
	if channel.Models[0].DisplayName != "我的视频模型" {
		t.Fatalf("display_name = %q", channel.Models[0].DisplayName)
	}
	if !containsString(channel.SupportedModes, "text_to_video") || containsString(channel.SupportedModes, "image_to_video") {
		t.Fatalf("supported_modes = %#v", channel.SupportedModes)
	}
	if containsString(channel.SupportedAspectRatios, "4:3") || containsString(channel.SupportedDurations, "30") {
		t.Fatalf("model-level aspect/duration should not leak into client capability: %#v %#v", channel.SupportedAspectRatios, channel.SupportedDurations)
	}
	if !containsString(channel.SupportedAspectRatios, "16:9") || !containsString(channel.SupportedDurations, "8") {
		t.Fatalf("route capability should be exposed: %#v %#v", channel.SupportedAspectRatios, channel.SupportedDurations)
	}
}

func TestBuildConfiguredProxyCapabilitiesUnionsRouteCapabilities(t *testing.T) {
	got := buildConfiguredProxyCapabilities([]service.ClientModelWithRoutes{
		{
			Model: model.ClientModel{
				BaseModel:       model.BaseModel{ID: "cm-1"},
				TenantID:        "tenant-1",
				ModelKey:        "veo-fast",
				DisplayName:     "Veo Fast",
				Provider:        model.ProviderVeo,
				Scope:           model.PricingScopeVideo,
				Enabled:         true,
				SupportedModes:  `["text_to_video"]`,
				SupportedScopes: `["video"]`,
			},
			Routes: []model.ClientModelRoute{
				{
					BaseModel:     model.BaseModel{ID: "route-1"},
					TenantID:      "tenant-1",
					ClientModelID: "cm-1",
					CredentialID:  "cred-1",
					UpstreamModel: "veo_3_1-fast",
					Enabled:       true,
					AspectRatios:  `["16:9"]`,
					Durations:     `["8"]`,
					Credential: &model.ProviderCredential{
						BaseModel:    model.BaseModel{ID: "cred-1"},
						Provider:     model.ProviderVeo,
						Mode:         "duoyuan",
						Enabled:      true,
						HealthStatus: model.CredentialHealthUnknown,
					},
				},
				{
					BaseModel:     model.BaseModel{ID: "route-2"},
					TenantID:      "tenant-1",
					ClientModelID: "cm-1",
					CredentialID:  "cred-2",
					UpstreamModel: "veo_3_1-fast",
					Enabled:       true,
					AspectRatios:  `["9:16"]`,
					Durations:     `["8"]`,
					Credential: &model.ProviderCredential{
						BaseModel:    model.BaseModel{ID: "cred-2"},
						Provider:     model.ProviderVeo,
						Mode:         "duoyuan",
						Enabled:      true,
						HealthStatus: model.CredentialHealthUnknown,
					},
				},
			},
		},
	})

	veo := findCapabilityProvider(got, "veo")
	if veo == nil || len(veo.Channels) != 1 {
		t.Fatalf("veo provider/channels = %#v", veo)
	}
	channel := veo.Channels[0]
	if !containsString(channel.SupportedAspectRatios, "16:9") || !containsString(channel.SupportedAspectRatios, "9:16") {
		t.Fatalf("route aspect ratios should be unioned: %#v", channel.SupportedAspectRatios)
	}
}

func TestSelectClientModelRouteUsesConfiguredPriority(t *testing.T) {
	rows := []model.ProviderCredential{
		{
			BaseModel:    model.BaseModel{ID: "grok-low", CreatedAt: time.Unix(1, 0)},
			Provider:     model.ProviderGrok,
			Mode:         "duoyuan",
			ChannelName:  "Grok 多元低优先级",
			DefaultModel: "grok-video-3",
			Enabled:      true,
			Priority:     1,
			HealthStatus: model.CredentialHealthUnknown,
		},
		{
			BaseModel:    model.BaseModel{ID: "grok-high", CreatedAt: time.Unix(2, 0)},
			Provider:     model.ProviderGrok,
			Mode:         "suchuang",
			ChannelName:  "Grok 速创高优先级",
			DefaultModel: "grok-video",
			Enabled:      true,
			Priority:     9,
			HealthStatus: model.CredentialHealthUnknown,
		},
	}

	route, ok := selectClientModelRoute(rows, model.ProviderGrok, "grok-imagine", model.PricingScopeVideo)
	if !ok {
		t.Fatal("route should be selected")
	}
	if route.CredentialID != "grok-high" || route.Mode != "suchuang" || route.UpstreamModel != "grok-video" {
		t.Fatalf("route = %#v", route)
	}
}

func findCapabilityProvider(got proxyCapabilitiesResponse, provider string) *proxyCapabilityProvider {
	for i := range got.Providers {
		if got.Providers[i].Provider == provider {
			return &got.Providers[i]
		}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
