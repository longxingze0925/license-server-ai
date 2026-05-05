package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectVeoImageInputs_UsesReferenceImagesForMultipleUploads(t *testing.T) {
	tmp := t.TempDir()
	first := writeTestUpload(t, tmp, "first.png")
	second := writeTestUpload(t, tmp, "second.png")

	body := []byte(`{"model":"veo-test","instances":[{"prompt":"show products"}],"parameters":{"durationSeconds":8}}`)
	out, err := injectVeoImageInputs(body, []serverUpload{
		{FileName: "first.png", MimeType: "image/png", Path: first},
		{FileName: "second.png", MimeType: "image/png", Path: second},
	})
	if err != nil {
		t.Fatalf("injectVeoImageInputs failed: %v", err)
	}

	var parsed struct {
		Instances []struct {
			Image           map[string]any `json:"image"`
			ReferenceImages []struct {
				ReferenceType string         `json:"referenceType"`
				ReferenceID   string         `json:"referenceId"`
				Image         map[string]any `json:"image"`
			} `json:"referenceImages"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(parsed.Instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(parsed.Instances))
	}
	if parsed.Instances[0].Image != nil {
		t.Fatalf("multi-reference request should not keep single image field")
	}
	if got := len(parsed.Instances[0].ReferenceImages); got != 2 {
		t.Fatalf("expected 2 reference images, got %d", got)
	}
	for _, ref := range parsed.Instances[0].ReferenceImages {
		if ref.ReferenceType != "asset" {
			t.Fatalf("expected asset reference type, got %q", ref.ReferenceType)
		}
		if ref.ReferenceID == "" {
			t.Fatal("expected reference id")
		}
		if ref.Image["bytesBase64Encoded"] == "" || ref.Image["mimeType"] != "image/png" {
			t.Fatalf("unexpected image payload: %#v", ref.Image)
		}
	}
}

func TestInjectVeoImageInputs_RejectsMoreThanThreeUploads(t *testing.T) {
	_, err := injectVeoImageInputs([]byte(`{"instances":[{}]}`), []serverUpload{{Path: "1"}, {Path: "2"}, {Path: "3"}, {Path: "4"}})
	if err == nil || !strings.Contains(err.Error(), "3 张") {
		t.Fatalf("expected max reference image error, got %v", err)
	}
}

func TestCreateImageEdit_RejectsMoreThanSixteenUploads(t *testing.T) {
	uploads := make([]serverUpload, 17)
	_, err := createImageEdit(context.Background(), nil, nil, []byte(`{}`), uploads)
	if err == nil || !strings.Contains(err.Error(), "16 张") {
		t.Fatalf("expected max reference image error, got %v", err)
	}
}

func TestStripServerOnlyFieldsRemovesRoutingFields(t *testing.T) {
	out := stripServerOnlyFields([]byte(`{"model":"gpt","mode":"official","scope":"analysis","channel_id":"cred-1","credential_id":"cred-2","messages":[]}`))

	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	for _, key := range []string{"mode", "scope", "channel_id", "credential_id"} {
		if _, ok := parsed[key]; ok {
			t.Fatalf("%s should be stripped: %#v", key, parsed)
		}
	}
	if parsed["model"] != "gpt" {
		t.Fatalf("model = %#v", parsed["model"])
	}
	if _, ok := parsed["messages"]; !ok {
		t.Fatalf("messages should be preserved: %#v", parsed)
	}
}

func writeTestUpload(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
