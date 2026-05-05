package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBackupSensitiveDataEncryptsAndDecryptsNestedSecrets(t *testing.T) {
	raw := `{
		"api_key":"sk-root-secret",
		"nested":{"access_token":"token-secret","visible":"ok"},
		"items":[{"password":"pass-secret"}]
	}`

	encrypted := encryptSensitiveData(raw, "app-secret")
	if strings.Contains(encrypted, "sk-root-secret") ||
		strings.Contains(encrypted, "token-secret") ||
		strings.Contains(encrypted, "pass-secret") {
		t.Fatalf("encrypted data leaked plaintext secret: %s", encrypted)
	}
	if !strings.Contains(encrypted, "ENC:") {
		t.Fatalf("encrypted data does not contain encrypted marker: %s", encrypted)
	}

	decrypted := decryptSensitiveData(encrypted, "app-secret")
	if !strings.Contains(decrypted, "sk-root-secret") ||
		!strings.Contains(decrypted, "token-secret") ||
		!strings.Contains(decrypted, "pass-secret") {
		t.Fatalf("decrypted data missing original secrets: %s", decrypted)
	}
	if !strings.Contains(decrypted, `"visible":"ok"`) {
		t.Fatalf("decrypted data changed non-sensitive field: %s", decrypted)
	}
}

func TestBackupSensitiveDataDecryptKeepsLegacyPlaintext(t *testing.T) {
	raw := `{"api_key":"sk-legacy","nested":{"secret":"plain-secret"}}`
	got := decryptSensitiveData(raw, "app-secret")
	if !strings.Contains(got, "sk-legacy") || !strings.Contains(got, "plain-secret") {
		t.Fatalf("legacy plaintext should stay readable, got %s", got)
	}
}

func TestMaskAPIKeyMasksNestedSensitiveFields(t *testing.T) {
	raw := `{
		"api_key":"sk-root-secret",
		"nested":{"access_token":"token-secret","visible":"ok"},
		"items":[{"private_key":"private-secret"}]
	}`

	masked := maskAPIKey(raw)
	if strings.Contains(masked, "sk-root-secret") ||
		strings.Contains(masked, "token-secret") ||
		strings.Contains(masked, "private-secret") {
		t.Fatalf("masked data leaked secret: %s", masked)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(masked), &parsed); err != nil {
		t.Fatalf("masked data should stay valid JSON: %v", err)
	}
	if parsed["api_key"] == "" {
		t.Fatalf("masked api_key should not be empty: %#v", parsed)
	}
}
