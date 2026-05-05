package adapter

import (
	"strings"
	"testing"

	"license-server/internal/model"
)

func TestPackageImageResponseReturnsImmediateMedia(t *testing.T) {
	res, err := packageImageResponse([]byte(`{"data":[{"b64_json":"aGVsbG8="}],"output_format":"jpeg"}`))
	if err != nil {
		t.Fatalf("packageImageResponse failed: %v", err)
	}
	if res.UpstreamTaskID != "" {
		t.Fatalf("UpstreamTaskID should stay empty for immediate image media, got %q", res.UpstreamTaskID)
	}
	if len(res.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(res.Media))
	}
	if res.Media[0].Kind != model.FileKindImage {
		t.Fatalf("kind = %q", res.Media[0].Kind)
	}
	if res.Media[0].MimeType != "image/jpeg" {
		t.Fatalf("mime = %q", res.Media[0].MimeType)
	}
	if !strings.HasPrefix(res.Media[0].DownloadURL, "data:image/jpeg;base64,") {
		t.Fatalf("download url = %q", res.Media[0].DownloadURL)
	}
}

func TestPackageImageResponseRejectsDataWithoutMedia(t *testing.T) {
	_, err := packageImageResponse([]byte(`{"data":[{}]}`))
	if err == nil || !strings.Contains(err.Error(), "url 或 b64_json") {
		t.Fatalf("expected missing media error, got %v", err)
	}
}
