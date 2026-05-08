package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"license-server/internal/model"
)

func TestGeminiImageCreate_DuoYuanUsesImagesEndpoint(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotModel, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"b64_json": base64.StdEncoding.EncodeToString([]byte("image"))},
			},
			"output_format": "png",
		})
	}))
	defer server.Close()

	res, err := (GeminiImageAdapter{}).Create(context.Background(), &model.ProviderCredential{
		Provider:     model.ProviderGemini,
		Mode:         "duoyuan",
		UpstreamBase: server.URL,
	}, []byte("secret"), []byte(`{"model":"gemini-3-pro-image-preview","prompt":"logo","scope":"image","mode":"duoyuan"}`))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if gotPath != "/v1/images/generations" {
		t.Fatalf("path = %q, want /v1/images/generations", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotModel != "gemini-3-pro-image-preview" {
		t.Fatalf("model = %q", gotModel)
	}
	if len(res.Media) != 1 || !strings.HasPrefix(res.Media[0].DownloadURL, "data:image/png;base64,") {
		t.Fatalf("media = %#v", res.Media)
	}
}

func TestGeminiImageCreate_RejectsNonDuoYuanMode(t *testing.T) {
	_, err := (GeminiImageAdapter{}).Create(context.Background(), &model.ProviderCredential{
		Provider: model.ProviderGemini,
		Mode:     "official",
	}, []byte("secret"), []byte(`{"model":"gemini-2.5-flash-image"}`))
	if err == nil || !strings.Contains(err.Error(), "duoyuan") {
		t.Fatalf("expected duoyuan mode error, got %v", err)
	}
}

func TestAsyncRegistryIncludesGeminiImageAdapter(t *testing.T) {
	a, ok := NewAsyncRegistry().Get(model.ProviderGemini)
	if !ok {
		t.Fatal("Gemini async adapter missing")
	}
	if _, ok := a.(*GeminiImageAdapter); !ok {
		t.Fatalf("adapter = %T, want *GeminiImageAdapter", a)
	}
}
