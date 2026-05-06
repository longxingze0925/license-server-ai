package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"license-server/internal/model"
)

func TestBuildGrokGenerateBody_UsesReferenceImagesForMultipleUploads(t *testing.T) {
	tmp := t.TempDir()
	first := writeGrokTestUpload(t, tmp, "first.png")
	second := writeGrokTestUpload(t, tmp, "second.png")

	out, err := buildGrokGenerateBody([]byte(`{"model":"grok-imagine-video","prompt":"product shot","duration_seconds":8}`), []serverUpload{
		{FileName: "first.png", MimeType: "image/png", Path: first},
		{FileName: "second.png", MimeType: "image/png", Path: second},
	})
	if err != nil {
		t.Fatalf("buildGrokGenerateBody failed: %v", err)
	}

	var parsed struct {
		Duration        int              `json:"duration"`
		DurationSeconds int              `json:"duration_seconds"`
		Image           map[string]any   `json:"image"`
		ReferenceImages []map[string]any `json:"reference_images"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if parsed.Duration != 8 || parsed.DurationSeconds != 0 {
		t.Fatalf("expected duration only, got duration=%d duration_seconds=%d", parsed.Duration, parsed.DurationSeconds)
	}
	if parsed.Image != nil {
		t.Fatalf("multi-reference request should not keep image field")
	}
	if got := len(parsed.ReferenceImages); got != 2 {
		t.Fatalf("expected 2 reference images, got %d", got)
	}
	for _, ref := range parsed.ReferenceImages {
		url, _ := ref["url"].(string)
		if !strings.HasPrefix(url, "data:image/png;base64,") {
			t.Fatalf("unexpected reference url: %q", url)
		}
	}
}

func TestBuildGrokGenerateBody_UsesImageForSingleUpload(t *testing.T) {
	tmp := t.TempDir()
	path := writeGrokTestUpload(t, tmp, "first.png")

	out, err := buildGrokGenerateBody([]byte(`{"model":"grok-imagine-video","prompt":"start frame","duration":12}`), []serverUpload{
		{FileName: "first.png", MimeType: "image/png", Path: path},
	})
	if err != nil {
		t.Fatalf("buildGrokGenerateBody failed: %v", err)
	}

	var parsed struct {
		Image           map[string]any   `json:"image"`
		ReferenceImages []map[string]any `json:"reference_images"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if parsed.Image == nil {
		t.Fatal("expected image field")
	}
	if len(parsed.ReferenceImages) != 0 {
		t.Fatalf("single upload should not use reference_images")
	}
}

func TestBuildGrokGenerateBody_RejectsMoreThanSevenUploads(t *testing.T) {
	uploads := make([]serverUpload, 8)
	_, err := buildGrokGenerateBody([]byte(`{"duration":8}`), uploads)
	if err == nil || !strings.Contains(err.Error(), "7 张") {
		t.Fatalf("expected max reference image error, got %v", err)
	}
}

func TestGrokPoll_ReportsFailedWhenSucceededWithoutVideoURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"done","video":{}}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Poll(context.Background(), &model.ProviderCredential{
		UpstreamBase: server.URL,
	}, []byte("key"), "request-1")
	if err != nil {
		t.Fatalf("Poll failed: %v", err)
	}
	if res.Status != AsyncStatusFailed {
		t.Fatalf("status = %v, want failed", res.Status)
	}
	if !strings.Contains(res.Error, "video.url") {
		t.Fatalf("error = %q, want missing video.url", res.Error)
	}
	if len(res.Media) != 0 {
		t.Fatalf("media should be empty, got %d", len(res.Media))
	}
}

func TestGrokPoll_ParsesCompatibleVideoURLFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"done","video":{"download_url":"https://cdn.example.test/official.mp4","duration":8}}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Poll(context.Background(), &model.ProviderCredential{
		UpstreamBase: server.URL,
	}, []byte("key"), "request-1")
	if err != nil {
		t.Fatalf("Poll failed: %v", err)
	}
	if res.Status != AsyncStatusSucceeded {
		t.Fatalf("status = %v, want succeeded", res.Status)
	}
	if len(res.Media) != 1 || res.Media[0].DownloadURL != "https://cdn.example.test/official.mp4" {
		t.Fatalf("media = %#v", res.Media)
	}
}

func TestBuildGrokThirdPartyGenerateBody_AllowsLongReferenceDuration(t *testing.T) {
	tmp := t.TempDir()
	first := writeGrokTestUpload(t, tmp, "first.png")
	second := writeGrokTestUpload(t, tmp, "second.png")

	out, err := buildGrokThirdPartyGenerateBody([]byte(`{"model":"grok-video","prompt":"test","duration_seconds":30}`), []serverUpload{
		{FileName: "first.png", MimeType: "image/png", Path: first},
		{FileName: "second.png", MimeType: "image/png", Path: second},
	})
	if err != nil {
		t.Fatalf("buildGrokThirdPartyGenerateBody failed: %v", err)
	}

	var parsed struct {
		DurationSeconds int              `json:"duration_seconds"`
		Duration        int              `json:"duration"`
		ReferenceImages []map[string]any `json:"reference_images"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if parsed.DurationSeconds != 30 || parsed.Duration != 0 {
		t.Fatalf("expected duration_seconds only, got duration_seconds=%d duration=%d", parsed.DurationSeconds, parsed.Duration)
	}
	if got := len(parsed.ReferenceImages); got != 2 {
		t.Fatalf("expected 2 reference images, got %d", got)
	}
}

func TestBuildDuoYuanVideoBody_UsesImagesArrayForUploads(t *testing.T) {
	tmp := t.TempDir()
	first := writeGrokTestUpload(t, tmp, "first.png")
	second := writeGrokTestUpload(t, tmp, "second.png")

	out, err := buildDuoYuanVideoBody([]byte(`{"model":"grok-video-3","prompt":"test","duration_seconds":8,"image":"legacy","reference_images":["legacy"]}`), []serverUpload{
		{FileName: "first.png", MimeType: "image/png", Path: first},
		{FileName: "second.png", MimeType: "image/png", Path: second},
	})
	if err != nil {
		t.Fatalf("buildDuoYuanVideoBody failed: %v", err)
	}

	var parsed struct {
		Duration        int      `json:"duration"`
		DurationSeconds int      `json:"durationSeconds"`
		DurationSnake   int      `json:"duration_seconds"`
		Images          []string `json:"images"`
		Image           string   `json:"image"`
		ReferenceImages []string `json:"reference_images"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if parsed.Duration != 8 || parsed.DurationSeconds != 0 || parsed.DurationSnake != 0 {
		t.Fatalf("duration fields = duration:%d durationSeconds:%d duration_seconds:%d, want duration only", parsed.Duration, parsed.DurationSeconds, parsed.DurationSnake)
	}
	if len(parsed.Images) != 2 {
		t.Fatalf("images count = %d, want 2", len(parsed.Images))
	}
	for _, image := range parsed.Images {
		if !strings.HasPrefix(image, "data:image/png;base64,") {
			t.Fatalf("unexpected image ref: %q", image)
		}
	}
	if parsed.Image != "" || len(parsed.ReferenceImages) != 0 {
		t.Fatalf("legacy image fields should be removed, got image=%q reference_images=%#v", parsed.Image, parsed.ReferenceImages)
	}
}

func TestGrokCreate_DuoYuanModeUsesDuoYuanEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/video/create" {
			t.Fatalf("path = %q, want /v1/video/create", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Fatalf("authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
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
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":"task-1"}}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Create(context.Background(), &model.ProviderCredential{
		Mode:         "duoyuan",
		UpstreamBase: server.URL,
		DefaultModel: "grok-video",
	}, []byte("key"), []byte(`{"prompt":"test","duration_seconds":8}`))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if res.UpstreamTaskID != "task-1" {
		t.Fatalf("task id = %q, want task-1", res.UpstreamTaskID)
	}
}

func TestGrokCreate_DuoYuanModeDoesNotFallbackToUnsupportedEndpoint(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid URL"}}`))
	}))
	defer server.Close()

	_, err := (GrokVideoAdapter{}).Create(context.Background(), &model.ProviderCredential{
		Mode:         "duoyuan",
		UpstreamBase: server.URL,
		DefaultModel: "grok-video-3",
	}, []byte("key"), []byte(`{"prompt":"test","duration_seconds":8}`))
	if err == nil {
		t.Fatal("Create should return upstream 404")
	}
	want := []string{"/v1/video/create"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestGrokCreate_SuChuangModeKeepsCompatibleEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/generations" {
			t.Fatalf("path = %q, want /v1/videos/generations", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"task-1"}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Create(context.Background(), &model.ProviderCredential{
		Mode:         "suchuang",
		UpstreamBase: server.URL,
		DefaultModel: "grok-video",
	}, []byte("key"), []byte(`{"prompt":"test","duration_seconds":8}`))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if res.UpstreamTaskID != "task-1" {
		t.Fatalf("task id = %q, want task-1", res.UpstreamTaskID)
	}
}

func TestGrokPoll_DuoYuanModeParsesResultJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/video/query" {
			t.Fatalf("path = %q, want /v1/video/query", r.URL.Path)
		}
		if got := r.URL.Query().Get("id"); got != "request-1" {
			t.Fatalf("id = %q, want request-1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"state":"success","resultJson":"{\"resultUrls\":[\"https://cdn.example.test/duoyuan.mp4\"],\"videoDuration\":8,\"videoSize\":\"1920x1080\"}"}}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Poll(context.Background(), &model.ProviderCredential{
		Mode:         "duoyuan",
		UpstreamBase: server.URL,
	}, []byte("key"), "request-1")
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if res.Status != AsyncStatusSucceeded {
		t.Fatalf("status = %v, want succeeded", res.Status)
	}
	if len(res.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(res.Media))
	}
	if res.Media[0].DownloadURL != "https://cdn.example.test/duoyuan.mp4" {
		t.Fatalf("download url = %q", res.Media[0].DownloadURL)
	}
	if res.Media[0].DurationMs != 8000 || res.Media[0].Width != 1920 || res.Media[0].Height != 1080 {
		t.Fatalf("media metadata = %#v", res.Media[0])
	}
}

func TestGrokPoll_DuoYuanModeParsesDocumentVideoURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/video/query" {
			t.Fatalf("path = %q, want /v1/video/query", r.URL.Path)
		}
		if got := r.URL.Query().Get("id"); got != "grok:request-1" {
			t.Fatalf("id = %q, want grok:request-1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"grok:request-1","status":"completed","progress":100,"video_url":"https://cdn.example.test/document.mp4","detail":{"status":"completed","progress_pct":1}}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Poll(context.Background(), &model.ProviderCredential{
		Mode:         "duoyuan",
		UpstreamBase: server.URL,
	}, []byte("key"), "grok:request-1")
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if res.Status != AsyncStatusSucceeded {
		t.Fatalf("status = %v, want succeeded", res.Status)
	}
	if res.Progress != 1 {
		t.Fatalf("progress = %v, want 1", res.Progress)
	}
	if len(res.Media) != 1 || res.Media[0].DownloadURL != "https://cdn.example.test/document.mp4" {
		t.Fatalf("media = %#v", res.Media)
	}
}

func TestGrokPoll_ThirdPartyModeParsesDataWrappedVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/generations/request-1" {
			t.Fatalf("path = %q, want /v1/videos/generations/request-1", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"succeeded","videos":[{"url":"https://cdn.example.test/video.mp4","duration":6,"width":1280,"height":720}]}}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Poll(context.Background(), &model.ProviderCredential{
		Mode:         "suchuang",
		UpstreamBase: server.URL,
	}, []byte("key"), "request-1")
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if res.Status != AsyncStatusSucceeded {
		t.Fatalf("status = %v, want succeeded", res.Status)
	}
	if len(res.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(res.Media))
	}
	if res.Media[0].DownloadURL != "https://cdn.example.test/video.mp4" {
		t.Fatalf("download url = %q", res.Media[0].DownloadURL)
	}
}

func TestGrokPoll_ThirdPartyModeParsesCompatibleURLFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"succeeded","video_url":"https://cdn.example.test/top.mp4","video":{"output_url":"https://cdn.example.test/nested.mp4"}}}`))
	}))
	defer server.Close()

	res, err := (GrokVideoAdapter{}).Poll(context.Background(), &model.ProviderCredential{
		Mode:         "suchuang",
		UpstreamBase: server.URL,
	}, []byte("key"), "request-1")
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if res.Status != AsyncStatusSucceeded {
		t.Fatalf("status = %v, want succeeded", res.Status)
	}
	if len(res.Media) != 2 {
		t.Fatalf("media count = %d, want 2", len(res.Media))
	}
	if res.Media[0].DownloadURL != "https://cdn.example.test/top.mp4" {
		t.Fatalf("first download url = %q", res.Media[0].DownloadURL)
	}
	if res.Media[1].DownloadURL != "https://cdn.example.test/nested.mp4" {
		t.Fatalf("second download url = %q", res.Media[1].DownloadURL)
	}
}

func writeGrokTestUpload(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
