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
	"strings"

	"license-server/internal/model"
)

// GrokVideoAdapter 适配 xAI Grok Imagine Video 异步生成协议。
//
// REST 形态：
//   - POST {base}/v1/videos/generations -> {"request_id":"..."}
//   - GET  {base}/v1/videos/{request_id} -> {"status":"pending|done|expired|failed","video":{"url":"..."}}
//
// 单张图片走 image-to-video；多张图片走 reference-to-video，最多 7 张。
type GrokVideoAdapter struct{}

func (GrokVideoAdapter) Provider() model.ProviderKind { return model.ProviderGrok }

func (a GrokVideoAdapter) Create(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error) {
	if isGrokThirdPartyMode(cred) {
		return a.createOpenAICompatible(ctx, cred, plainKey, body)
	}

	body = injectDefaultModel(body, cred.DefaultModel)
	var err error
	body, err = buildGrokGenerateBody(body, parseServerUploads(body))
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(cred.UpstreamBase, "/") + "/v1/videos/generations"
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
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamHTTPError("create", resp.StatusCode, respBody, 200)
	}

	var parsed struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("create 响应解析失败: %w", err)
	}
	if parsed.RequestID == "" {
		return nil, errors.New("create 响应缺少 request_id 字段")
	}

	return &CreateResult{
		UpstreamTaskID:   parsed.RequestID,
		NextPollAfterSec: 5,
		RawSnippet:       truncateAdapter(string(respBody), 256),
	}, nil
}

func (a GrokVideoAdapter) Poll(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error) {
	if isGrokThirdPartyMode(cred) {
		return a.pollOpenAICompatible(ctx, cred, plainKey, upstreamTaskID)
	}

	url := strings.TrimRight(cred.UpstreamBase, "/") + "/v1/videos/" + upstreamTaskID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(plainKey))
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
		Status string `json:"status"`
		Video  struct {
			URL         string  `json:"url"`
			VideoURL    string  `json:"video_url"`
			DownloadURL string  `json:"download_url"`
			OutputURL   string  `json:"output_url"`
			Duration    float64 `json:"duration"`
			MimeType    string  `json:"mime_type"`
			Width       int     `json:"width"`
			Height      int     `json:"height"`
		} `json:"video"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("poll 响应解析失败: %w", err)
	}

	out := &PollResult{
		RawSnippet: truncateAdapter(string(respBody), 256),
	}
	switch strings.ToLower(parsed.Status) {
	case "done", "completed", "succeeded", "success":
		videoURL := firstNonEmptyAdapter(parsed.Video.URL, parsed.Video.VideoURL, parsed.Video.DownloadURL, parsed.Video.OutputURL)
		if videoURL == "" {
			out.Status = AsyncStatusFailed
			out.Error = parsed.Error.Message
			if out.Error == "" {
				out.Error = parsed.Message
			}
			if out.Error == "" {
				out.Error = "Grok 成功响应缺少 video.url"
			}
			return out, nil
		}

		out.Status = AsyncStatusSucceeded
		out.Progress = 1.0
		mime := parsed.Video.MimeType
		if mime == "" {
			mime = "video/mp4"
		}
		out.Media = append(out.Media, MediaDescriptor{
			Kind:        model.FileKindVideo,
			DownloadURL: videoURL,
			MimeType:    mime,
			DurationMs:  int(parsed.Video.Duration * 1000),
			Width:       parsed.Video.Width,
			Height:      parsed.Video.Height,
		})
	case "expired", "failed", "error":
		out.Status = AsyncStatusFailed
		out.Error = parsed.Error.Message
		if out.Error == "" {
			out.Error = parsed.Message
		}
		if out.Error == "" {
			out.Error = "Grok 视频生成失败或已过期"
		}
	default:
		out.Status = AsyncStatusRunning
		out.Progress = 0.5
	}
	return out, nil
}

func (a GrokVideoAdapter) BuildDownloadRequest(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, m MediaDescriptor) (*http.Request, error) {
	if isGrokThirdPartyMode(cred) {
		req, err := defaultDownload(ctx, m)
		if err != nil {
			return nil, err
		}
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+string(plainKey))
		}
		return req, nil
	}
	return defaultDownload(ctx, m)
}

func grokCredentialMode(cred *model.ProviderCredential) string {
	mode := "official"
	if cred != nil && strings.TrimSpace(cred.Mode) != "" {
		mode = strings.ToLower(strings.TrimSpace(cred.Mode))
	}
	return mode
}

func isGrokThirdPartyMode(cred *model.ProviderCredential) bool {
	switch grokCredentialMode(cred) {
	case "duoyuan", "suchuang":
		return true
	default:
		return false
	}
}

func (a GrokVideoAdapter) createOpenAICompatible(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error) {
	body = injectDefaultModel(body, cred.DefaultModel)
	var err error
	body, err = buildGrokThirdPartyGenerateBody(body, parseServerUploads(body))
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(cred.UpstreamBase, "/") + "/v1/videos/generations"
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
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamHTTPError("create", resp.StatusCode, respBody, 200)
	}

	var parsed struct {
		ID        string `json:"id"`
		RequestID string `json:"request_id"`
		TaskID    string `json:"task_id"`
		Data      struct {
			ID        string `json:"id"`
			RequestID string `json:"request_id"`
			TaskID    string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("create 响应解析失败: %w", err)
	}
	taskID := firstNonEmptyAdapter(parsed.ID, parsed.RequestID, parsed.TaskID, parsed.Data.ID, parsed.Data.RequestID, parsed.Data.TaskID)
	if taskID == "" {
		return nil, errors.New("create 响应缺少 id/request_id/task_id 字段")
	}
	return &CreateResult{
		UpstreamTaskID:   taskID,
		NextPollAfterSec: 5,
		RawSnippet:       truncateAdapter(string(respBody), 256),
	}, nil
}

func (a GrokVideoAdapter) pollOpenAICompatible(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error) {
	url := strings.TrimRight(cred.UpstreamBase, "/") + "/v1/videos/generations/" + upstreamTaskID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(plainKey))
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
		Status      string                      `json:"status"`
		Progress    float64                     `json:"progress"`
		URL         string                      `json:"url"`
		VideoURL    string                      `json:"video_url"`
		DownloadURL string                      `json:"download_url"`
		OutputURL   string                      `json:"output_url"`
		Videos      []grokOpenAICompatibleVideo `json:"videos"`
		Video       grokOpenAICompatibleVideo   `json:"video"`
		Error       struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
		Data    struct {
			Status      string                      `json:"status"`
			Progress    float64                     `json:"progress"`
			URL         string                      `json:"url"`
			VideoURL    string                      `json:"video_url"`
			DownloadURL string                      `json:"download_url"`
			OutputURL   string                      `json:"output_url"`
			Videos      []grokOpenAICompatibleVideo `json:"videos"`
			Video       grokOpenAICompatibleVideo   `json:"video"`
			Error       struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("poll 响应解析失败: %w", err)
	}

	status := firstNonEmptyAdapter(parsed.Status, parsed.Data.Status)
	progress := parsed.Progress
	if progress == 0 {
		progress = parsed.Data.Progress
	}
	out := &PollResult{
		Progress:   float32(progress),
		RawSnippet: truncateAdapter(string(respBody), 256),
	}
	switch strings.ToLower(status) {
	case "done", "completed", "succeeded", "success":
		out.Status = AsyncStatusSucceeded
		out.Progress = 1.0
		appendGrokOpenAICompatibleURL(out, firstNonEmptyAdapter(parsed.URL, parsed.VideoURL, parsed.DownloadURL, parsed.OutputURL), 0, "", 0, 0)
		appendGrokOpenAICompatibleVideos(out, parsed.Videos)
		appendGrokOpenAICompatibleVideo(out, parsed.Video)
		appendGrokOpenAICompatibleURL(out, firstNonEmptyAdapter(parsed.Data.URL, parsed.Data.VideoURL, parsed.Data.DownloadURL, parsed.Data.OutputURL), 0, "", 0, 0)
		appendGrokOpenAICompatibleVideos(out, parsed.Data.Videos)
		appendGrokOpenAICompatibleVideo(out, parsed.Data.Video)
		if len(out.Media) == 0 {
			out.Status = AsyncStatusFailed
			out.Error = firstNonEmptyAdapter(parsed.Error.Message, parsed.Message, parsed.Data.Error.Message, parsed.Data.Message, "Grok 成功响应缺少视频 URL")
		}
	case "failed", "error", "expired":
		out.Status = AsyncStatusFailed
		out.Error = firstNonEmptyAdapter(parsed.Error.Message, parsed.Message, parsed.Data.Error.Message, parsed.Data.Message, "Grok 视频生成失败或已过期")
	default:
		out.Status = AsyncStatusRunning
		if out.Progress == 0 {
			out.Progress = 0.5
		}
	}
	return out, nil
}

type grokOpenAICompatibleVideo struct {
	URL         string  `json:"url"`
	VideoURL    string  `json:"video_url"`
	DownloadURL string  `json:"download_url"`
	OutputURL   string  `json:"output_url"`
	Duration    float64 `json:"duration"`
	MimeType    string  `json:"mime_type"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
}

func appendGrokOpenAICompatibleVideos(out *PollResult, videos []grokOpenAICompatibleVideo) {
	for _, video := range videos {
		appendGrokOpenAICompatibleVideo(out, video)
	}
}

func appendGrokOpenAICompatibleVideo(out *PollResult, video grokOpenAICompatibleVideo) {
	appendGrokOpenAICompatibleURL(
		out,
		firstNonEmptyAdapter(video.URL, video.VideoURL, video.DownloadURL, video.OutputURL),
		video.Duration,
		video.MimeType,
		video.Width,
		video.Height,
	)
}

func appendGrokOpenAICompatibleURL(out *PollResult, url string, duration float64, mimeType string, width, height int) {
	if strings.TrimSpace(url) == "" {
		return
	}
	mime := mimeType
	if mime == "" {
		mime = "video/mp4"
	}
	out.Media = append(out.Media, MediaDescriptor{
		Kind:        model.FileKindVideo,
		DownloadURL: strings.TrimSpace(url),
		MimeType:    mime,
		DurationMs:  int(duration * 1000),
		Width:       width,
		Height:      height,
	})
}

func firstNonEmptyAdapter(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildGrokGenerateBody(body []byte, uploads []serverUpload) ([]byte, error) {
	cleanBody := stripServerOnlyFields(body)
	var m map[string]any
	if err := json.Unmarshal(cleanBody, &m); err != nil {
		return nil, fmt.Errorf("解析 Grok 请求失败: %w", err)
	}

	normalizeGrokDuration(m)
	delete(m, "duration_seconds")

	if len(uploads) == 0 {
		return json.Marshal(m)
	}
	if len(uploads) > 7 {
		return nil, errors.New("Grok 最多支持 7 张参考图")
	}

	images, err := buildGrokImageRefs(uploads)
	if err != nil {
		return nil, err
	}
	if len(images) == 1 {
		m["image"] = images[0]
		delete(m, "reference_images")
	} else {
		duration := numericField(m, "duration")
		if duration > 10 {
			return nil, errors.New("Grok 参考图模式最长支持 10 秒")
		}
		m["reference_images"] = images
		delete(m, "image")
	}

	return json.Marshal(m)
}

func buildGrokThirdPartyGenerateBody(body []byte, uploads []serverUpload) ([]byte, error) {
	cleanBody := stripServerOnlyFields(body)
	var m map[string]any
	if err := json.Unmarshal(cleanBody, &m); err != nil {
		return nil, fmt.Errorf("解析 Grok 请求失败: %w", err)
	}

	if len(uploads) == 0 {
		return json.Marshal(m)
	}
	if len(uploads) > 7 {
		return nil, errors.New("Grok 最多支持 7 张参考图")
	}

	images, err := buildGrokImageRefs(uploads)
	if err != nil {
		return nil, err
	}
	if len(images) == 1 {
		m["image"] = images[0]
		delete(m, "reference_images")
	} else {
		m["reference_images"] = images
		delete(m, "image")
	}

	return json.Marshal(m)
}

func normalizeGrokDuration(m map[string]any) {
	if _, ok := m["duration"]; ok {
		return
	}
	if v, ok := m["duration_seconds"]; ok {
		m["duration"] = v
	}
}

func buildGrokImageRefs(uploads []serverUpload) ([]map[string]any, error) {
	refs := make([]map[string]any, 0, len(uploads))
	for _, upload := range uploads {
		bytes, err := readUploadBytes(upload)
		if err != nil {
			return nil, fmt.Errorf("读取参考图失败: %w", err)
		}
		mimeType := strings.TrimSpace(upload.MimeType)
		if mimeType == "" {
			mimeType = "image/png"
		}
		refs = append(refs, map[string]any{
			"url": fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(bytes)),
		})
	}
	return refs, nil
}

func numericField(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		value, _ := v.Float64()
		return value
	default:
		return 0
	}
}
