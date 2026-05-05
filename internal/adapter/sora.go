package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"license-server/internal/model"
)

// SoraAdapter 适配 OpenAI 风格的视频异步生成协议。
//
// 第一阶段按"OpenAI Sora（async 模式）"协议实现：
//
//	POST {base}/v1/videos/generations
//	  Body: {model, prompt, duration_seconds, ...}
//	  Resp: {"id":"task-xxx","status":"queued",...}
//
//	GET  {base}/v1/videos/generations/{id}
//	  Resp 进行中: {"id":"task-xxx","status":"queued|running","progress":0.3}
//	  Resp 完成 : {"id":"task-xxx","status":"completed","videos":[{"url":"http://...","duration":5,"width":1280,"height":720}]}
//	  Resp 失败 : {"id":"task-xxx","status":"failed","error":{"message":"..."}}
//
// 这个协议同时也兼容我们 mock 服务器的实现（详见 tools/mock_async/）。
// 真实 Sora API 上线后细节调整时只动这个文件。
type SoraAdapter struct{}

func (SoraAdapter) Provider() model.ProviderKind { return model.ProviderSora }

func (a SoraAdapter) Create(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error) {
	body = injectDefaultModel(body, cred.DefaultModel)
	if len(parseServerUploads(body)) > 0 {
		return nil, errors.New("Sora adapter 暂不支持参考图上传")
	}
	body = stripServerOnlyFields(body)

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
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("create 响应解析失败: %w", err)
	}
	if parsed.ID == "" {
		return nil, errors.New("create 响应缺少 id 字段")
	}
	return &CreateResult{
		UpstreamTaskID:   parsed.ID,
		NextPollAfterSec: 5,
		RawSnippet:       truncateAdapter(string(respBody), 256),
	}, nil
}

func (a SoraAdapter) Poll(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error) {
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
		ID       string  `json:"id"`
		Status   string  `json:"status"`
		Progress float64 `json:"progress"`
		Videos   []struct {
			URL      string `json:"url"`
			Duration int    `json:"duration"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			MimeType string `json:"mime_type"`
		} `json:"videos"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("poll 响应解析失败: %w", err)
	}

	out := &PollResult{
		Progress:   float32(parsed.Progress),
		RawSnippet: truncateAdapter(string(respBody), 256),
	}
	switch strings.ToLower(parsed.Status) {
	case "completed", "succeeded", "success":
		out.Status = AsyncStatusSucceeded
		out.Progress = 1.0
		for _, v := range parsed.Videos {
			mime := v.MimeType
			if mime == "" {
				mime = "video/mp4"
			}
			out.Media = append(out.Media, MediaDescriptor{
				Kind:        model.FileKindVideo,
				DownloadURL: v.URL,
				MimeType:    mime,
				DurationMs:  v.Duration * 1000,
				Width:       v.Width,
				Height:      v.Height,
			})
		}
	case "failed", "error":
		out.Status = AsyncStatusFailed
		out.Error = parsed.Error.Message
		if out.Error == "" {
			out.Error = "上游报告失败但未提供原因"
		}
	default:
		out.Status = AsyncStatusRunning
	}
	return out, nil
}

func (a SoraAdapter) BuildDownloadRequest(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, m MediaDescriptor) (*http.Request, error) {
	req, err := defaultDownload(ctx, m)
	if err != nil {
		return nil, err
	}
	// OpenAI 媒体下载 URL 通常已签名，不需要 Authorization；为通用性还是带上。
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+string(plainKey))
	}
	return req, nil
}

func truncateAdapter(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
