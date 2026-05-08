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

// GeminiImageAdapter adapts Gemini image generation through third-party
// OpenAI-compatible image endpoints. Currently only DuoYuan mode is supported.
type GeminiImageAdapter struct{}

func (GeminiImageAdapter) Provider() model.ProviderKind { return model.ProviderGemini }

func (GeminiImageAdapter) Create(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error) {
	if !isGeminiDuoYuanMode(cred) {
		return nil, errors.New("Gemini 图片生成当前仅支持多元 duoyuan 模式")
	}
	body, err := buildDuoYuanGeminiImageBody(body, parseServerUploads(body))
	if err != nil {
		return nil, err
	}

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
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamHTTPError("gemini image create "+url, resp.StatusCode, respBody, 200)
	}

	res, err := packageImageResponse(respBody)
	if err != nil {
		return nil, err
	}
	res.RawSnippet = truncateAdapter(string(respBody), 256)
	return res, nil
}

func (GeminiImageAdapter) Poll(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error) {
	pkg, err := decodeImagePackage(upstreamTaskID)
	if err != nil {
		return nil, err
	}
	media, err := mediaFromImagePackage(pkg)
	if err != nil {
		return nil, err
	}
	return &PollResult{
		Status:   AsyncStatusSucceeded,
		Progress: 1,
		Media:    media,
	}, nil
}

func (GeminiImageAdapter) BuildDownloadRequest(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, m MediaDescriptor) (*http.Request, error) {
	if strings.HasPrefix(m.DownloadURL, "data:") {
		return newDataURIRequest(ctx, m.DownloadURL)
	}
	return defaultDownload(ctx, m)
}

func isGeminiDuoYuanMode(cred *model.ProviderCredential) bool {
	if cred == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cred.Mode), "duoyuan")
}

func buildDuoYuanGeminiImageBody(body []byte, uploads []serverUpload) ([]byte, error) {
	cleanBody := stripServerOnlyFields(body)
	var m map[string]any
	if err := json.Unmarshal(cleanBody, &m); err != nil {
		return nil, fmt.Errorf("解析多元 Gemini 图片请求失败: %w", err)
	}

	if len(uploads) > 0 {
		images, err := buildDuoYuanImageRefs(uploads)
		if err != nil {
			return nil, err
		}
		m["images"] = images
		delete(m, "image")
		delete(m, "reference_images")
	}

	return json.Marshal(m)
}
