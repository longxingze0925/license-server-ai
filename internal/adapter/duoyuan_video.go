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
	"strconv"
	"strings"

	"license-server/internal/model"
)

func buildDuoYuanVideoBody(body []byte, uploads []serverUpload) ([]byte, error) {
	cleanBody := stripServerOnlyFields(body)
	var m map[string]any
	if err := json.Unmarshal(cleanBody, &m); err != nil {
		return nil, fmt.Errorf("解析多元视频请求失败: %w", err)
	}

	if v, ok := m["duration_seconds"]; ok {
		if _, exists := m["durationSeconds"]; !exists {
			m["durationSeconds"] = v
		}
	}
	if v, ok := m["resolution"].(string); ok && strings.TrimSpace(v) != "" {
		if _, exists := m["size"]; !exists {
			m["size"] = strings.ToUpper(strings.TrimSpace(v))
		}
	}

	if len(uploads) > 0 {
		images, err := buildDuoYuanImageRefs(uploads)
		if err != nil {
			return nil, err
		}
		m["image"] = images[0]
		if len(images) > 1 {
			m["reference_images"] = images
		}
	}

	return json.Marshal(m)
}

func createDuoYuanVideo(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error) {
	upURL := duoYuanVideoCreateURL(cred)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upURL, bytes.NewReader(body))
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
		return nil, newUpstreamHTTPError("create "+upURL, resp.StatusCode, respBody, 200)
	}

	var parsed struct {
		Code        int    `json:"code"`
		Msg         string `json:"msg"`
		ID          string `json:"id"`
		TaskID      string `json:"task_id"`
		TaskIDCamel string `json:"taskId"`
		Data        struct {
			ID          string `json:"id"`
			TaskID      string `json:"task_id"`
			TaskIDCamel string `json:"taskId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("create 响应解析失败: %w", err)
	}
	if parsed.Code != 0 && parsed.Code != http.StatusOK {
		if parsed.Msg == "" {
			parsed.Msg = "上游返回业务失败"
		}
		return nil, errors.New(parsed.Msg)
	}
	taskID := firstNonEmptyAdapter(
		parsed.ID,
		parsed.TaskID,
		parsed.TaskIDCamel,
		parsed.Data.ID,
		parsed.Data.TaskID,
		parsed.Data.TaskIDCamel,
	)
	if taskID == "" {
		return nil, errors.New("create 响应缺少 id/task_id/taskId 字段")
	}
	return &CreateResult{
		UpstreamTaskID:   taskID,
		NextPollAfterSec: 5,
		RawSnippet:       truncateAdapter(string(respBody), 256),
	}, nil
}

func pollDuoYuanVideo(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error) {
	upURL := duoYuanVideoPollURL(cred, upstreamTaskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upURL, nil)
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

	var parsed duoYuanVideoPollResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("poll 响应解析失败: %w", err)
	}

	out := &PollResult{
		Progress:   0.5,
		RawSnippet: truncateAdapter(string(respBody), 256),
	}
	if parsed.Code != 0 && parsed.Code != http.StatusOK {
		out.Status = AsyncStatusFailed
		out.Error = firstNonEmptyAdapter(parsed.Msg, parsed.Message, "多元视频任务查询失败")
		return out, nil
	}

	status := strings.ToLower(firstNonEmptyAdapter(parsed.Status, parsed.State, parsed.Data.Status, parsed.Data.State))
	switch status {
	case "done", "completed", "succeeded", "success":
		out.Status = AsyncStatusSucceeded
		out.Progress = 1.0
		appendDuoYuanVideoURL(out, firstNonEmptyAdapter(parsed.URL, parsed.VideoURL, parsed.DownloadURL, parsed.OutputURL), 0, "", 0, 0)
		appendDuoYuanResultURLs(out, parsed.ResultURLs, 0, "", 0, 0)
		appendDuoYuanResultURLs(out, parsed.ResultURLsSnake, 0, "", 0, 0)
		appendDuoYuanVideoURL(out, firstNonEmptyAdapter(parsed.Data.URL, parsed.Data.VideoURL, parsed.Data.DownloadURL, parsed.Data.OutputURL), 0, "", 0, 0)
		appendDuoYuanResultURLs(out, parsed.Data.ResultURLs, 0, "", 0, 0)
		appendDuoYuanResultURLs(out, parsed.Data.ResultURLsSnake, 0, "", 0, 0)
		appendDuoYuanResultJSON(out, firstNonEmptyAdapter(parsed.ResultJSON, parsed.Data.ResultJSON))
		if len(out.Media) == 0 {
			out.Status = AsyncStatusFailed
			out.Error = firstNonEmptyAdapter(parsed.FailMsg, parsed.Message, parsed.Msg, parsed.Data.FailMsg, parsed.Data.Message, "多元视频任务成功但缺少视频 URL")
		}
	case "failed", "error", "expired":
		out.Status = AsyncStatusFailed
		out.Error = firstNonEmptyAdapter(parsed.FailMsg, parsed.Message, parsed.Msg, parsed.Data.FailMsg, parsed.Data.Message, "多元视频生成失败或已过期")
	default:
		out.Status = AsyncStatusRunning
	}
	return out, nil
}

type duoYuanVideoPollResponse struct {
	Code            int      `json:"code"`
	Msg             string   `json:"msg"`
	Status          string   `json:"status"`
	State           string   `json:"state"`
	URL             string   `json:"url"`
	VideoURL        string   `json:"video_url"`
	DownloadURL     string   `json:"download_url"`
	OutputURL       string   `json:"output_url"`
	ResultURLs      []string `json:"resultUrls"`
	ResultURLsSnake []string `json:"result_urls"`
	ResultJSON      string   `json:"resultJson"`
	FailMsg         string   `json:"failMsg"`
	Message         string   `json:"message"`
	Data            struct {
		Status          string   `json:"status"`
		State           string   `json:"state"`
		URL             string   `json:"url"`
		VideoURL        string   `json:"video_url"`
		DownloadURL     string   `json:"download_url"`
		OutputURL       string   `json:"output_url"`
		ResultURLs      []string `json:"resultUrls"`
		ResultURLsSnake []string `json:"result_urls"`
		ResultJSON      string   `json:"resultJson"`
		FailMsg         string   `json:"failMsg"`
		Message         string   `json:"message"`
	} `json:"data"`
}

func duoYuanVideoCreateURL(cred *model.ProviderCredential) string {
	return strings.TrimRight(cred.UpstreamBase, "/") + "/api/v1/video/generations"
}

func duoYuanVideoPollURL(cred *model.ProviderCredential, upstreamTaskID string) string {
	return strings.TrimRight(cred.UpstreamBase, "/") + "/api/v1/video/generations?task_id=" + url.QueryEscape(upstreamTaskID)
}

func appendDuoYuanResultURLs(out *PollResult, urls []string, duration float64, mimeType string, width, height int) {
	for _, u := range urls {
		appendDuoYuanVideoURL(out, u, duration, mimeType, width, height)
	}
}

func appendDuoYuanResultJSON(out *PollResult, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return
	}
	var parsed struct {
		ResultURLs      []string `json:"resultUrls"`
		ResultURLsSnake []string `json:"result_urls"`
		VideoDuration   float64  `json:"videoDuration"`
		VideoSize       string   `json:"videoSize"`
		MimeType        string   `json:"mime_type"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return
	}
	width, height := parseDuoYuanVideoSize(parsed.VideoSize)
	appendDuoYuanResultURLs(out, parsed.ResultURLs, parsed.VideoDuration, parsed.MimeType, width, height)
	appendDuoYuanResultURLs(out, parsed.ResultURLsSnake, parsed.VideoDuration, parsed.MimeType, width, height)
}

func appendDuoYuanVideoURL(out *PollResult, u string, duration float64, mimeType string, width, height int) {
	if strings.TrimSpace(u) == "" {
		return
	}
	mime := mimeType
	if mime == "" {
		mime = "video/mp4"
	}
	out.Media = append(out.Media, MediaDescriptor{
		Kind:        model.FileKindVideo,
		DownloadURL: strings.TrimSpace(u),
		MimeType:    mime,
		DurationMs:  int(duration * 1000),
		Width:       width,
		Height:      height,
	})
}

func parseDuoYuanVideoSize(size string) (int, int) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return 0, 0
	}
	width, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return width, height
}

func buildDuoYuanImageRefs(uploads []serverUpload) ([]string, error) {
	refs := make([]string, 0, len(uploads))
	for _, upload := range uploads {
		bytes, err := readUploadBytes(upload)
		if err != nil {
			return nil, fmt.Errorf("读取参考图失败: %w", err)
		}
		mimeType := strings.TrimSpace(upload.MimeType)
		if mimeType == "" {
			mimeType = "image/png"
		}
		refs = append(refs, fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(bytes)))
	}
	return refs, nil
}
