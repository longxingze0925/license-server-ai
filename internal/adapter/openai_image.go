package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	"license-server/internal/model"
)

// OpenAIImageAdapter 适配 OpenAI Images API（DALL-E 协议）。
//
// 特殊点：上游同步返回（POST 一次拿到 URL），但我们要套用 AsyncAdapter 形态。
// 做法：Create 阶段直接返回媒体描述，让 AsyncRunner 立即落盘完成任务。
//
// 也支持 base64 模式（response_format: b64_json）—— 用 data: URI 形式喂给 BuildDownloadRequest。
//
// 端点：POST {base}/v1/images/generations
//
//	Body: {model, prompt, n, size, quality, response_format}
//	Resp: {created, data: [{url|b64_json: "..."}]}
type OpenAIImageAdapter struct{}

func (OpenAIImageAdapter) Provider() model.ProviderKind { return model.ProviderGpt }

type imagePackage struct {
	URLs   []string `json:"u,omitempty"` // http URL
	Inline []string `json:"b,omitempty"` // data: URL（base64 inline）
	Mime   string   `json:"m,omitempty"` // mime
}

func (OpenAIImageAdapter) Create(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error) {
	body = injectDefaultModel(body, cred.DefaultModel)
	uploads := parseServerUploads(body)
	if len(uploads) > 0 {
		return createImageEdit(ctx, cred, plainKey, body, uploads)
	}
	body = stripServerOnlyFields(body)

	url := strings.TrimRight(cred.UpstreamBase, "/") + "/v1/images/generations"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(plainKey))
	applyCustomHeaders(req, cred.CustomHeader)

	resp, err := adapterHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamHTTPError("create", resp.StatusCode, respBody, 200)
	}

	return packageImageResponse(respBody)
}

func createImageEdit(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte, uploads []serverUpload) (*CreateResult, error) {
	if len(uploads) > 16 {
		return nil, errors.New("OpenAI 图片编辑最多支持 16 张参考图")
	}

	cleanBody := stripServerOnlyFields(body)
	var fields map[string]any
	if err := json.Unmarshal(cleanBody, &fields); err != nil {
		return nil, fmt.Errorf("解析图片编辑请求失败: %w", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		if !isSimpleMultipartField(value) {
			continue
		}
		if err := writer.WriteField(key, fmt.Sprint(value)); err != nil {
			return nil, err
		}
	}

	imageField := "image[]"
	if len(uploads) == 1 {
		imageField = "image"
	}
	for _, upload := range uploads {
		if err := writeUploadPart(writer, imageField, upload); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	url := strings.TrimRight(cred.UpstreamBase, "/") + "/v1/images/edits"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+string(plainKey))
	applyCustomHeaders(req, cred.CustomHeader)

	resp, err := adapterHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamHTTPError("edit", resp.StatusCode, respBody, 200)
	}
	return packageImageResponse(respBody)
}

func (OpenAIImageAdapter) Poll(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error) {
	pkg, err := decodeImagePackage(upstreamTaskID)
	if err != nil {
		return nil, err
	}
	out := &PollResult{
		Status:   AsyncStatusSucceeded,
		Progress: 1.0,
	}
	media, err := mediaFromImagePackage(pkg)
	if err != nil {
		return nil, err
	}
	out.Media = media
	return out, nil
}

func (OpenAIImageAdapter) BuildDownloadRequest(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, m MediaDescriptor) (*http.Request, error) {
	// data: URI 会被 AsyncRunner 直接解码；这里保留兜底，避免误走普通下载。
	if strings.HasPrefix(m.DownloadURL, "data:") {
		return newDataURIRequest(ctx, m.DownloadURL)
	}
	return defaultDownload(ctx, m)
}

// ====================== helpers ======================

func packageImageResponse(respBody []byte) (*CreateResult, error) {
	var parsed struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
		OutputFormat string `json:"output_format"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("解析图片响应失败: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, errors.New("上游图片响应为空")
	}

	pkg := imagePackage{Mime: mimeFromImageFormat(parsed.OutputFormat)}
	for _, d := range parsed.Data {
		if d.URL != "" {
			pkg.URLs = append(pkg.URLs, d.URL)
		} else if d.B64JSON != "" {
			pkg.Inline = append(pkg.Inline, d.B64JSON)
		}
	}
	media, err := mediaFromImagePackage(&pkg)
	if err != nil {
		return nil, err
	}

	return &CreateResult{
		Media:            media,
		NextPollAfterSec: 0,
		RawSnippet:       truncateAdapter(string(respBody), 256),
	}, nil
}

func mediaFromImagePackage(pkg *imagePackage) ([]MediaDescriptor, error) {
	if pkg == nil {
		return nil, errors.New("图片包为空")
	}
	mimeType := strings.TrimSpace(pkg.Mime)
	if mimeType == "" {
		mimeType = "image/png"
	}
	media := make([]MediaDescriptor, 0, len(pkg.URLs)+len(pkg.Inline))
	for _, u := range pkg.URLs {
		if strings.TrimSpace(u) == "" {
			continue
		}
		media = append(media, MediaDescriptor{
			Kind:        model.FileKindImage,
			DownloadURL: u,
			MimeType:    mimeType,
		})
	}
	for i, b := range pkg.Inline {
		if strings.TrimSpace(b) == "" {
			continue
		}
		media = append(media, MediaDescriptor{
			Kind:        model.FileKindImage,
			DownloadURL: fmt.Sprintf("data:%s;base64,%s", mimeType, b),
			MimeType:    mimeType,
			AuthHeaders: map[string]string{"X-Inline-Index": fmt.Sprintf("%d", i)},
		})
	}
	if len(media) == 0 {
		return nil, errors.New("上游图片响应未包含 url 或 b64_json")
	}
	return media, nil
}

func isSimpleMultipartField(value any) bool {
	switch value.(type) {
	case nil, string, bool, float64, int, int64, json.Number:
		return true
	default:
		return false
	}
}

func writeUploadPart(writer *multipart.Writer, fieldName string, upload serverUpload) error {
	content, err := readUploadBytes(upload)
	if err != nil {
		return err
	}
	fileName := strings.TrimSpace(upload.FileName)
	if fileName == "" {
		fileName = filepath.Base(upload.Path)
	}
	mimeType := strings.TrimSpace(upload.MimeType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeMultipart(fieldName), escapeMultipart(fileName)))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(content)
	return err
}

func escapeMultipart(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func mimeFromImageFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func decodeImagePackage(upstreamTaskID string) (*imagePackage, error) {
	if !strings.HasPrefix(upstreamTaskID, "img:") {
		return nil, errors.New("非图片包 upstream_task_id")
	}
	raw := strings.TrimPrefix(upstreamTaskID, "img:")
	var pkg imagePackage
	if err := json.Unmarshal([]byte(raw), &pkg); err != nil {
		return nil, fmt.Errorf("反序列化图片包失败: %w", err)
	}
	return &pkg, nil
}

func newDataURIRequest(ctx context.Context, dataURI string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, "about:blank#"+base64.StdEncoding.EncodeToString([]byte(dataURI)), nil)
}
