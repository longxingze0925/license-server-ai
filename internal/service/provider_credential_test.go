package service

import (
	"errors"
	"testing"

	"license-server/internal/model"
)

func TestNormalizeProviderCredentialMode(t *testing.T) {
	tests := []struct {
		name     string
		provider model.ProviderKind
		mode     string
		want     string
	}{
		{name: "veo default aliases to google", provider: model.ProviderVeo, mode: "official", want: "google"},
		{name: "sora empty aliases to async", provider: model.ProviderSora, mode: "", want: "async"},
		{name: "grok suchuang preserved", provider: model.ProviderGrok, mode: " SuChuang ", want: "suchuang"},
		{name: "gpt unknown aliases to official", provider: model.ProviderGpt, mode: "other", want: "official"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeProviderCredentialMode(tt.provider, tt.mode)
			if got != tt.want {
				t.Fatalf("NormalizeProviderCredentialMode(%q, %q) = %q, want %q", tt.provider, tt.mode, got, tt.want)
			}
		})
	}
}

func TestNormalizeUpstreamBaseTrimsSpacesAndTrailingSlashes(t *testing.T) {
	got := normalizeUpstreamBase(" https://api.example.test/// ")
	if got != "https://api.example.test" {
		t.Fatalf("normalizeUpstreamBase = %q, want https://api.example.test", got)
	}
}

func TestValidateProviderCredentialProviderRejectsClaude(t *testing.T) {
	err := ValidateProviderCredentialProvider(model.ProviderClaude)
	if !errors.Is(err, ErrUnsupportedProviderCredential) {
		t.Fatalf("validateProviderCredentialProvider(claude) error = %v, want ErrUnsupportedProviderCredential", err)
	}
}
