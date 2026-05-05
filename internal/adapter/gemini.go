package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"license-server/internal/model"
)

// GeminiChatAdapter 适配 Google Gemini generateContent 协议（用于聊天/分析/视频反推）。
//
// 上游路径：
//
//	POST {base}/v1beta/models/{model}:generateContent?key={api_key}
//	Body: {"contents":[{"role":"user","parts":[{"text":"..."},{"inline_data":{...}}]}], "generationConfig":{...}}
//	Resp: {"candidates":[{"content":{"parts":[{"text":"..."}]}}], ...}
//
// 客户端发来的 body 是"Gemini 风格"的（不是 OpenAI 风格），原样转发。
// 模型名取自 body.model 或 cred.DefaultModel。
type GeminiChatAdapter struct{}

func (GeminiChatAdapter) Provider() model.ProviderKind { return model.ProviderGemini }

func (a GeminiChatAdapter) BuildChat(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*http.Request, error) {
	// 取 model：优先 body.model，否则 cred.DefaultModel
	modelID := extractFieldString(body, "model")
	if modelID == "" {
		modelID = cred.DefaultModel
	}
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	// 构造 upstream URL
	upURL := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		strings.TrimRight(cred.UpstreamBase, "/"),
		url.PathEscape(modelID),
		url.QueryEscape(string(plainKey)))

	// 把 body 中可能存在的 OpenAI 字段（model / messages / mode / scope）剥掉，
	// 只保留 Gemini 原生字段（contents / generationConfig / systemInstruction / tools）。
	cleanBody := stripNonGeminiFields(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upURL, bytes.NewReader(cleanBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyCustomHeaders(req, cred.CustomHeader)
	return req, nil
}

func (GeminiChatAdapter) ParseError(httpStatus int, respBody []byte) string {
	// Gemini 错误格式：{"error":{"code":...,"message":"...","status":"..."}}
	var wrap struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &wrap); err == nil && wrap.Error.Message != "" {
		return fmt.Sprintf("[%d %s] %s", httpStatus, wrap.Error.Status, wrap.Error.Message)
	}
	return fmt.Sprintf("[HTTP %d] %s", httpStatus, truncateAdapter(string(respBody), 200))
}

// stripNonGeminiFields 把 OpenAI 风格的 model/messages/mode/scope 等字段去掉，
// 留下 Gemini 原生字段。客户端发的就是 Gemini 风格也无副作用。
func stripNonGeminiFields(body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	for _, k := range []string{"model", "messages", "mode", "scope", "stream", "temperature_top_k"} {
		delete(m, k)
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}
