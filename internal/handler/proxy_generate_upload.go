package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"license-server/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	proxyPayloadField                = "payload"
	proxyFallbackBodyField           = "body"
	proxyServerUploadsField          = "__server_uploads"
	defaultProxyUploadBytes    int64 = 20 << 20
	proxyUploadMaxFiles              = 16
	proxyMultipartPayloadSlack       = 1 << 20
)

var allowedProxyImageMimeTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
	"image/gif":  {},
	"image/bmp":  {},
}

type proxyServerUpload struct {
	FieldName string `json:"field_name,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	Path      string `json:"path,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

func readGeneratePayload(c *gin.Context, userID string) (body []byte, persisted []byte, cleanup func(), err error) {
	cleanup = func() {}
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return readMultipartGeneratePayload(c, userID)
	}

	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, cleanup, err
	}
	body, err = stripProxyServerUploads(rawBody)
	if err != nil {
		return nil, nil, cleanup, err
	}
	return body, body, cleanup, nil
}

func readMultipartGeneratePayload(c *gin.Context, userID string) ([]byte, []byte, func(), error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, getMaxProxyMultipartBytes())
	if err := c.Request.ParseMultipartForm(getMultipartMemoryBytes()); err != nil {
		return nil, nil, func() {}, fmt.Errorf("解析 multipart 失败: %w", err)
	}

	payload := strings.TrimSpace(c.PostForm(proxyPayloadField))
	if payload == "" {
		payload = strings.TrimSpace(c.PostForm(proxyFallbackBodyField))
	}
	if payload == "" {
		return nil, nil, func() {}, errors.New("multipart generate 缺少 payload 字段")
	}

	persisted, err := stripProxyServerUploads([]byte(payload))
	if err != nil {
		return nil, nil, func() {}, err
	}
	var bodyMap map[string]any
	if err := json.Unmarshal(persisted, &bodyMap); err != nil {
		return nil, nil, func() {}, fmt.Errorf("解析 payload JSON 失败: %w", err)
	}

	uploads, cleanup, err := saveProxyUploads(c.Request.MultipartForm, userID)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	if len(uploads) > 0 {
		bodyMap[proxyServerUploadsField] = uploads
		bodyMap["reference_image_count"] = len(uploads)
		bodyMap["input_image_count"] = len(uploads)
	}

	persistedMap := make(map[string]any, len(bodyMap))
	for key, value := range bodyMap {
		if key == proxyServerUploadsField {
			continue
		}
		persistedMap[key] = value
	}
	persisted, err = json.Marshal(persistedMap)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	return body, persisted, cleanup, nil
}

func saveProxyUploads(form *multipart.Form, userID string) ([]proxyServerUpload, func(), error) {
	if form == nil || len(form.File) == 0 {
		return nil, func() {}, nil
	}
	tempRoot := filepath.Join(os.TempDir(), "license-server-proxy-uploads", userID)
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, func() {}, err
	}

	maxBytes := getMaxProxyUploadBytes()
	var uploads []proxyServerUpload
	var paths []string
	for fieldName, headers := range form.File {
		for _, header := range headers {
			if len(uploads) >= proxyUploadMaxFiles {
				for _, path := range paths {
					_ = os.Remove(path)
				}
				return nil, func() {}, fmt.Errorf("最多支持上传 %d 张图片", proxyUploadMaxFiles)
			}
			upload, err := saveOneProxyUpload(tempRoot, fieldName, header, maxBytes)
			if err != nil {
				for _, path := range paths {
					_ = os.Remove(path)
				}
				return nil, func() {}, err
			}
			uploads = append(uploads, upload)
			paths = append(paths, upload.Path)
		}
	}

	return uploads, func() {
		for _, path := range paths {
			_ = os.Remove(path)
		}
	}, nil
}

func saveOneProxyUpload(tempRoot, fieldName string, header *multipart.FileHeader, maxBytes int64) (proxyServerUpload, error) {
	src, err := header.Open()
	if err != nil {
		return proxyServerUpload{}, err
	}
	defer src.Close()

	fileName := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		ext = ".bin"
	}
	destPath := filepath.Join(tempRoot, uuid.NewString()+ext)
	limited := &io.LimitedReader{R: src, N: maxBytes + 1}
	size, _, err := saveUploadedFile(limited, destPath)
	if err != nil {
		_ = os.Remove(destPath)
		return proxyServerUpload{}, err
	}
	if size > maxBytes {
		_ = os.Remove(destPath)
		return proxyServerUpload{}, errUploadFileTooLarge
	}

	mimeType, err := resolveProxyUploadMime(header.Header.Get("Content-Type"), ext, destPath)
	if err != nil {
		_ = os.Remove(destPath)
		return proxyServerUpload{}, err
	}

	return proxyServerUpload{
		FieldName: fieldName,
		FileName:  fileName,
		MimeType:  mimeType,
		Path:      destPath,
		SizeBytes: size,
	}, nil
}

func stripProxyServerUploads(body []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	delete(m, proxyServerUploadsField)
	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func getMaxProxyUploadBytes() int64 {
	cfg := config.Get()
	if cfg == nil {
		return defaultProxyUploadBytes
	}
	limit := resolveUploadLimitBytes(cfg.Security.MaxRequestBodyMB, defaultProxyUploadBytes)
	if limit <= 0 || limit > defaultProxyUploadBytes {
		return defaultProxyUploadBytes
	}
	return limit
}

func getMaxProxyMultipartBytes() int64 {
	return getMaxProxyUploadBytes()*proxyUploadMaxFiles + proxyMultipartPayloadSlack
}

func getMultipartMemoryBytes() int64 {
	cfg := config.Get()
	if cfg == nil || cfg.Security.MultipartMemoryMB <= 0 {
		return 8 << 20
	}
	return int64(cfg.Security.MultipartMemoryMB) << 20
}

func resolveProxyUploadMime(headerMime, ext, path string) (string, error) {
	for _, candidate := range []string{
		detectFileMime(path),
		headerMime,
		mime.TypeByExtension(ext),
	} {
		mimeType := normalizeMimeType(candidate)
		if mimeType == "" || mimeType == "application/octet-stream" {
			continue
		}
		if _, ok := allowedProxyImageMimeTypes[mimeType]; ok {
			return mimeType, nil
		}
		return "", fmt.Errorf("不支持的上传文件类型: %s", mimeType)
	}
	return "", errors.New("不支持的上传文件类型")
}

func detectFileMime(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	var buf [512]byte
	n, err := file.Read(buf[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	if n == 0 {
		return ""
	}
	return http.DetectContentType(buf[:n])
}

func normalizeMimeType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(value); err == nil {
		return strings.ToLower(mediaType)
	}
	return value
}
