package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"license-server/internal/model"
)

func TestVeoCreate_DuoYuanModeUsesDuoYuanEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/video/create" {
			t.Fatalf("path = %q, want /v1/video/create", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["model"] != "veo3" {
			t.Fatalf("model = %#v, want veo3", body["model"])
		}
		if _, ok := body["duration"]; !ok {
			t.Fatalf("request body should include duration: %#v", body)
		}
		if _, ok := body["duration_seconds"]; ok {
			t.Fatalf("request body should not include duration_seconds: %#v", body)
		}
		if _, ok := body["durationSeconds"]; ok {
			t.Fatalf("request body should not include durationSeconds: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":"veo-task-1"}}`))
	}))
	defer server.Close()

	res, err := (VeoAdapter{}).Create(context.Background(), &model.ProviderCredential{
		Mode:         "duoyuan",
		UpstreamBase: server.URL,
		DefaultModel: "veo3",
	}, []byte("key"), []byte(`{"prompt":"test","duration_seconds":8}`))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if res.UpstreamTaskID != "veo-task-1" {
		t.Fatalf("task id = %q, want veo-task-1", res.UpstreamTaskID)
	}
}
