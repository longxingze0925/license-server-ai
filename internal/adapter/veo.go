package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"license-server/internal/model"
)

// VeoAdapter 适配 Google Veo 的"长任务（Long Running Operation）"协议。
//
// 路径形态（GoogleNative 模式）：
//
//	POST {base}/v1beta/models/{model}:predictLongRunning?key=<api_key>
//	  Body: {"instances":[{"prompt":"...","image":...}], "parameters":{"durationSeconds":5,...}}
//	  Resp: {"name":"projects/.../operations/abc"}
//
//	GET  {base}/v1beta/{operation_name}?key=<api_key>
//	  Resp 进行中: {"name":"...","metadata":{...},"done":false}
//	  Resp 完成 : {"name":"...","done":true,"response":{"videos":[{"video":{"uri":"http://..."}}]}}
//	  Resp 失败 : {"done":true,"error":{"code":3,"message":"..."}}
//
// 注：Veo 的实际响应字段名随版本变化，这里实现的是最常见形态；真实接入时调整 JSON 字段映射即可。
// 兼容性：当 cred.Mode == "adapter" 时改走 OpenAI 风格（同 Sora 路径），方便接 3rd-party 转发服务。
type VeoAdapter struct{}

func (VeoAdapter) Provider() model.ProviderKind { return model.ProviderVeo }

func (a VeoAdapter) Create(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error) {
	if cred.Mode == "adapter" || cred.Mode == "duoyuan" {
		return (SoraAdapter{}).Create(ctx, cred, plainKey, body)
	}
	// GoogleNative：默认
	body = injectDefaultModel(body, cred.DefaultModel)
	if uploads := parseServerUploads(body); len(uploads) > 0 {
		var err error
		body, err = injectVeoImageInputs(body, uploads)
		if err != nil {
			return nil, err
		}
	} else {
		body = stripServerOnlyFields(body)
	}

	model_id := extractFieldString(body, "model")
	if model_id == "" {
		model_id = cred.DefaultModel
	}
	if model_id == "" {
		return nil, errors.New("Veo 需要在 body 中提供 model（如 veo-3.0-generate-preview）")
	}
	upURL := fmt.Sprintf("%s/v1beta/models/%s:predictLongRunning?key=%s",
		strings.TrimRight(cred.UpstreamBase, "/"), url.PathEscape(model_id), url.QueryEscape(string(plainKey)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyCustomHeaders(req, cred.CustomHeader)

	resp, err := adapterHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamHTTPError("create", resp.StatusCode, respBody, 200)
	}

	var parsed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("create 响应解析失败: %w", err)
	}
	if parsed.Name == "" {
		return nil, errors.New("create 响应缺少 name 字段")
	}
	return &CreateResult{
		UpstreamTaskID:   parsed.Name,
		NextPollAfterSec: 8,
		RawSnippet:       truncateAdapter(string(respBody), 256),
	}, nil
}

func (a VeoAdapter) Poll(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error) {
	if cred.Mode == "adapter" || cred.Mode == "duoyuan" {
		return (SoraAdapter{}).Poll(ctx, cred, plainKey, upstreamTaskID)
	}
	upURL := fmt.Sprintf("%s/v1beta/%s?key=%s",
		strings.TrimRight(cred.UpstreamBase, "/"), upstreamTaskID, url.QueryEscape(string(plainKey)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upURL, nil)
	if err != nil {
		return nil, err
	}
	applyCustomHeaders(req, cred.CustomHeader)

	resp, err := adapterHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("poll 上游 HTTP %d: %s", resp.StatusCode, truncateAdapter(string(respBody), 200))
	}

	var parsed struct {
		Done     bool `json:"done"`
		Metadata struct {
			Progress float64 `json:"progressPercent"`
		} `json:"metadata"`
		Response struct {
			Videos []struct {
				Video struct {
					URI                string `json:"uri"`
					GcsURI             string `json:"gcsUri"`
					BytesBase64Encoded string `json:"bytesBase64Encoded"`
					MimeType           string `json:"mimeType"`
					Duration           int    `json:"durationMs"`
					Width              int    `json:"width"`
					Height             int    `json:"height"`
				} `json:"video"`
			} `json:"videos"`
			GenerateVideoResponse struct {
				GeneratedSamples []struct {
					Video struct {
						URI                string `json:"uri"`
						GcsURI             string `json:"gcsUri"`
						BytesBase64Encoded string `json:"bytesBase64Encoded"`
						MimeType           string `json:"mimeType"`
						Duration           int    `json:"durationMs"`
						Width              int    `json:"width"`
						Height             int    `json:"height"`
					} `json:"video"`
				} `json:"generatedSamples"`
			} `json:"generateVideoResponse"`
		} `json:"response"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("poll 响应解析失败: %w", err)
	}

	out := &PollResult{
		Progress:   float32(parsed.Metadata.Progress / 100.0),
		RawSnippet: truncateAdapter(string(respBody), 256),
	}
	if !parsed.Done {
		out.Status = AsyncStatusRunning
		return out, nil
	}
	if parsed.Error.Message != "" {
		out.Status = AsyncStatusFailed
		out.Error = parsed.Error.Message
		return out, nil
	}
	out.Status = AsyncStatusSucceeded
	out.Progress = 1.0
	for _, v := range parsed.Response.Videos {
		appendVeoVideo(&out.Media, v.Video.URI, v.Video.GcsURI, v.Video.BytesBase64Encoded, v.Video.MimeType, v.Video.Duration, v.Video.Width, v.Video.Height)
	}
	for _, sample := range parsed.Response.GenerateVideoResponse.GeneratedSamples {
		appendVeoVideo(&out.Media, sample.Video.URI, sample.Video.GcsURI, sample.Video.BytesBase64Encoded, sample.Video.MimeType, sample.Video.Duration, sample.Video.Width, sample.Video.Height)
	}
	return out, nil
}

func (a VeoAdapter) BuildDownloadRequest(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, m MediaDescriptor) (*http.Request, error) {
	req, err := defaultDownload(ctx, m)
	if err != nil {
		return nil, err
	}
	// Google Cloud Storage 签名 URL 不需要带 key；通用兼容性留头。
	return req, nil
}

// extractFieldString 从 JSON body 里读一个 string 字段，失败返回空。
func extractFieldString(body []byte, key string) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func injectVeoImageInputs(body []byte, uploads []serverUpload) ([]byte, error) {
	if len(uploads) > 3 {
		return nil, errors.New("Veo 最多支持 3 张参考图")
	}

	cleanBody := stripServerOnlyFields(body)
	var m map[string]any
	if err := json.Unmarshal(cleanBody, &m); err != nil {
		return nil, fmt.Errorf("解析 Veo 请求失败: %w", err)
	}

	instances, _ := m["instances"].([]any)
	if len(instances) == 0 {
		instances = []any{map[string]any{}}
	}
	first, ok := instances[0].(map[string]any)
	if !ok {
		first = map[string]any{}
		instances[0] = first
	}

	images, err := buildVeoReferenceImages(uploads)
	if err != nil {
		return nil, err
	}
	if len(images) == 1 {
		first["image"] = images[0]
	} else {
		delete(first, "image")
		first["referenceImages"] = wrapVeoReferenceImages(images)
	}
	m["instances"] = instances

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func buildVeoReferenceImages(uploads []serverUpload) ([]map[string]any, error) {
	images := make([]map[string]any, 0, len(uploads))
	for _, upload := range uploads {
		bytes, err := readUploadBytes(upload)
		if err != nil {
			return nil, fmt.Errorf("读取参考图失败: %w", err)
		}
		mimeType := strings.TrimSpace(upload.MimeType)
		if mimeType == "" {
			mimeType = "image/png"
		}
		images = append(images, map[string]any{
			"bytesBase64Encoded": base64.StdEncoding.EncodeToString(bytes),
			"mimeType":           mimeType,
		})
	}
	return images, nil
}

func wrapVeoReferenceImages(images []map[string]any) []map[string]any {
	refs := make([]map[string]any, 0, len(images))
	for index, image := range images {
		refs = append(refs, map[string]any{
			"referenceType": "asset",
			"referenceId":   fmt.Sprintf("asset_%d", index+1),
			"image":         image,
		})
	}
	return refs
}

func appendVeoVideo(target *[]MediaDescriptor, uri, gcsURI, b64, mime string, durationMs, width, height int) {
	if mime == "" {
		mime = "video/mp4"
	}
	downloadURL := uri
	if downloadURL == "" {
		downloadURL = gcsURI
	}
	if downloadURL == "" && b64 != "" {
		downloadURL = fmt.Sprintf("data:%s;base64,%s", mime, b64)
	}
	if downloadURL == "" {
		return
	}
	*target = append(*target, MediaDescriptor{
		Kind:        model.FileKindVideo,
		DownloadURL: downloadURL,
		MimeType:    mime,
		DurationMs:  durationMs,
		Width:       width,
		Height:      height,
	})
}
