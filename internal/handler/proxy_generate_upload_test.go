package handler

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSaveOneProxyUploadRejectsNonImage(t *testing.T) {
	header := buildProxyUploadHeader(t, "bad.txt", "text/plain", []byte("not an image"))

	_, err := saveOneProxyUpload(t.TempDir(), "images", header, defaultProxyUploadBytes)
	if err == nil || !strings.Contains(err.Error(), "不支持的上传文件类型") {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestSaveOneProxyUploadAcceptsPNG(t *testing.T) {
	header := buildProxyUploadHeader(t, "good.png", "image/png", []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0x00, 0x00, 0x00, 0x0d,
	})

	upload, err := saveOneProxyUpload(t.TempDir(), "images", header, defaultProxyUploadBytes)
	if err != nil {
		t.Fatalf("saveOneProxyUpload failed: %v", err)
	}
	if upload.MimeType != "image/png" {
		t.Fatalf("mime = %q, want image/png", upload.MimeType)
	}
}

func buildProxyUploadHeader(t *testing.T, fileName, contentType string, data []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("images", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	header := req.MultipartForm.File["images"][0]
	header.Header.Set("Content-Type", contentType)
	return header
}
