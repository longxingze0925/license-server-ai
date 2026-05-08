package handler

import (
	"testing"

	"license-server/internal/model"
)

func TestGeminiDuoYuanImageCapability(t *testing.T) {
	modes := supportedGenerationModes(model.ProviderGemini, "duoyuan", "gemini-3-pro-image-preview")
	if !containsString(modes, "text_to_image") || !containsString(modes, "image_to_image") {
		t.Fatalf("modes = %#v, want image generation modes", modes)
	}

	scopes := supportedProviderScopes(model.ProviderGemini, "duoyuan", "gemini-3-pro-image-preview")
	if len(scopes) != 1 || scopes[0] != "image" {
		t.Fatalf("scopes = %#v, want image only", scopes)
	}
}

func TestGeminiOfficialStillAnalysisOnly(t *testing.T) {
	if modes := supportedGenerationModes(model.ProviderGemini, "official", "gemini-2.5-flash"); len(modes) != 0 {
		t.Fatalf("official modes = %#v, want none", modes)
	}
	scopes := supportedProviderScopes(model.ProviderGemini, "official", "gemini-2.5-flash")
	if len(scopes) != 1 || scopes[0] != "analysis" {
		t.Fatalf("official scopes = %#v, want analysis", scopes)
	}
}
