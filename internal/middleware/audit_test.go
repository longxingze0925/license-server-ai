package middleware

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskSensitiveDataRedactsNestedSecrets(t *testing.T) {
	got := maskSensitiveData(`{"password":"p1","oldPassword":"old-secret","profile":{"apiKey":"sk-test","name":"alice"},"items":[{"refreshToken":"rt-secret"}],"custom_headers":"{\"X-Token\":\"header-secret\"}"}`)
	if strings.Contains(got, "p1") || strings.Contains(got, "old-secret") || strings.Contains(got, "sk-test") || strings.Contains(got, "rt-secret") || strings.Contains(got, "header-secret") {
		t.Fatalf("masked audit body leaked a secret: %s", got)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("masked audit body should remain valid JSON: %v", err)
	}
	if parsed["password"] != "***" {
		t.Fatalf("password was not masked: %s", got)
	}
}

func TestMaskSensitiveDataRejectsInvalidJSON(t *testing.T) {
	got := maskSensitiveData(`{"password":`)
	if !strings.Contains(got, "invalid_json") || strings.Contains(got, "password") {
		t.Fatalf("invalid JSON should be omitted safely, got %q", got)
	}
}

func TestParseActionFromPathClassifiesProviderAndPricingResources(t *testing.T) {
	_, resource, resourceID := parseActionFromPath("PUT", "/api/admin/proxy/credentials/cred-1")
	if resource != "provider_credential" || resourceID != "cred-1" {
		t.Fatalf("provider credential audit resource = %q id = %q", resource, resourceID)
	}

	_, resource, resourceID = parseActionFromPath("DELETE", "/api/admin/pricing/rules/42")
	if resource != "pricing_rule" || resourceID != "42" {
		t.Fatalf("pricing audit resource = %q id = %q", resource, resourceID)
	}
}
