package handler

import (
	"encoding/json"
	"testing"
	"time"

	"license-server/internal/model"
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
	if channel.ChannelID != "grok-official" {
		t.Fatalf("channel_id = %q", channel.ChannelID)
	}
	if channel.Mode != "official" {
		t.Fatalf("mode = %q, want official", channel.Mode)
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
	if len(provider.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(provider.Channels))
	}
	duoyuan := provider.Channels[0]
	if duoyuan.Mode != "duoyuan" {
		t.Fatalf("first mode = %q, want duoyuan", duoyuan.Mode)
	}
	if duoyuan.DefaultModel != "grok-video-3" {
		t.Fatalf("duoyuan default_model = %q, want grok-video-3", duoyuan.DefaultModel)
	}
	if !containsString(duoyuan.SupportedModes, "image_to_video") {
		t.Fatalf("duoyuan supported_modes = %#v, want image_to_video", duoyuan.SupportedModes)
	}
	if !containsString(duoyuan.SupportedDurations, "8") {
		t.Fatalf("duoyuan supported_durations = %#v, want 8", duoyuan.SupportedDurations)
	}
	suchuang := provider.Channels[1]
	if suchuang.Mode != "suchuang" {
		t.Fatalf("second mode = %q, want suchuang", suchuang.Mode)
	}
	if suchuang.DefaultModel != "grok-video" {
		t.Fatalf("suchuang default_model = %q, want grok-video", suchuang.DefaultModel)
	}
	if !containsString(suchuang.SupportedDurations, "30") {
		t.Fatalf("suchuang supported_durations = %#v, want 30", suchuang.SupportedDurations)
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
	if veo.Channels[0].DefaultModel != "veo3" {
		t.Fatalf("veo duoyuan default_model = %q, want veo3", veo.Channels[0].DefaultModel)
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
	if sora.Channels[0].ChannelID != "sora-async" {
		t.Fatalf("channel_id = %q, want sora-async", sora.Channels[0].ChannelID)
	}
	if !sora.Channels[0].IsDefault {
		t.Fatal("first visible channel should be default")
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
