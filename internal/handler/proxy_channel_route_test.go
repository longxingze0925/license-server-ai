package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"license-server/internal/model"

	"github.com/gin-gonic/gin"
)

func TestExtractCredentialIDUsesRealChannelID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/proxy/grok/generate?channel_id=cred-123", nil)

	got := extractCredentialID(nil, c)
	if got != "cred-123" {
		t.Fatalf("credential id = %q, want cred-123", got)
	}
}

func TestExtractCredentialIDIgnoresBackendProxyFallbackID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/proxy/grok/generate?channel_id=backend-proxy-grok-official-1", nil)

	got := extractCredentialID([]byte(`{"channel_id":"body-cred"}`), c)
	if got != "" {
		t.Fatalf("credential id = %q, want empty fallback", got)
	}
}

func TestExtractChatModeAndScopeDefaultsToChat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/proxy/gpt/chat", nil)

	mode, scope := extractChatModeAndScope([]byte(`{"model":"gpt-5.2"}`), c)
	if mode != "" {
		t.Fatalf("mode = %q, want empty", mode)
	}
	if scope != model.PricingScopeChat {
		t.Fatalf("scope = %q, want chat", scope)
	}
}

func TestExtractChatModeAndScopeUsesAnalysisScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/proxy/gemini/chat?scope=analysis&mode=official", nil)

	mode, scope := extractChatModeAndScope([]byte(`{"scope":"chat","mode":"duoyuan"}`), c)
	if mode != "official" {
		t.Fatalf("mode = %q, want official", mode)
	}
	if scope != model.PricingScopeAnalysis {
		t.Fatalf("scope = %q, want analysis", scope)
	}
}

func TestResolveProxyRequestModeUsesProviderDefault(t *testing.T) {
	tests := []struct {
		name     string
		provider model.ProviderKind
		mode     string
		want     string
	}{
		{name: "veo default", provider: model.ProviderVeo, want: "google"},
		{name: "sora default", provider: model.ProviderSora, want: "async"},
		{name: "grok third party", provider: model.ProviderGrok, mode: "SuChuang", want: "suchuang"},
		{name: "gpt default", provider: model.ProviderGpt, want: "official"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveProxyRequestMode(tt.provider, tt.mode)
			if got != tt.want {
				t.Fatalf("mode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildCredentialProbeRequestUsesGoogleModelsEndpoint(t *testing.T) {
	req, err := buildCredentialProbeRequest(context.Background(), &model.ProviderCredential{
		Provider:     model.ProviderVeo,
		Mode:         "google",
		UpstreamBase: "https://generativelanguage.googleapis.com",
	}, []byte("secret-key"))
	if err != nil {
		t.Fatalf("build probe request: %v", err)
	}
	if req.URL.Path != "/v1beta/models" {
		t.Fatalf("path = %q, want /v1beta/models", req.URL.Path)
	}
	if req.URL.Query().Get("key") != "secret-key" {
		t.Fatalf("query key was not set")
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("google probe should not set bearer authorization")
	}
	if strings.Contains(safeProbeURL(req), "secret-key") {
		t.Fatalf("safe probe URL leaked key: %s", safeProbeURL(req))
	}
}

func TestBuildCredentialProbeRequestUsesProviderHeaders(t *testing.T) {
	req, err := buildCredentialProbeRequest(context.Background(), &model.ProviderCredential{
		Provider:     model.ProviderClaude,
		Mode:         "official",
		UpstreamBase: "https://api.anthropic.com",
		CustomHeader: `{"Authorization":"Bearer wrong","X-Test":"ok"}`,
	}, []byte("anthropic-key"))
	if err != nil {
		t.Fatalf("build probe request: %v", err)
	}
	if req.URL.Path != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", req.URL.Path)
	}
	if req.Header.Get("x-api-key") != "anthropic-key" {
		t.Fatalf("x-api-key was not set")
	}
	if req.Header.Get("anthropic-version") == "" {
		t.Fatalf("anthropic-version was not set")
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("reserved custom authorization should be ignored")
	}
	if req.Header.Get("X-Test") != "ok" {
		t.Fatalf("custom header was not applied")
	}
}

func TestMaskCustomHeadersMasksSensitiveValues(t *testing.T) {
	got := maskCustomHeaders(`{"X-Api-Key":"sk-test-secret-value","X-Trace":"trace-id"}`)

	var headers map[string]string
	if err := json.Unmarshal([]byte(got), &headers); err != nil {
		t.Fatalf("maskCustomHeaders returned invalid json: %v", err)
	}
	if headers["X-Api-Key"] != "***" {
		t.Fatalf("X-Api-Key = %q, want masked", headers["X-Api-Key"])
	}
	if headers["X-Trace"] != "trace-id" {
		t.Fatalf("X-Trace = %q, want trace-id", headers["X-Trace"])
	}
}
