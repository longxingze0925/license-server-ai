package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"license-server/internal/model"
)

// OpenAIChatAdapter 适配 OpenAI Chat Completions 协议。
//
// 上游 URL = credential.UpstreamBase + "/v1/chat/completions"
// Auth: Authorization: Bearer <api_key>
// Body: 原样透传（包含 model / messages / temperature / stream / ... 等字段）
//
// 注意：客户端发的 body 不一定带 model 字段；如果 credential.DefaultModel 非空且 body 没填 model，
// 会自动填上 default_model（方便管理员把"GPT-4o-mini 通道"和"GPT-4-Turbo 通道"分开管理）。
type OpenAIChatAdapter struct{}

func (OpenAIChatAdapter) Provider() model.ProviderKind { return model.ProviderGpt }

func (a OpenAIChatAdapter) BuildChat(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*http.Request, error) {
	body = stripServerOnlyFields(injectDefaultModel(body, cred.DefaultModel))

	url := strings.TrimRight(cred.UpstreamBase, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(plainKey))
	applyCustomHeaders(req, cred.CustomHeader)
	return req, nil
}

func (OpenAIChatAdapter) ParseError(httpStatus int, respBody []byte) string {
	// OpenAI 错误格式：{"error":{"message":"...","type":"...","code":"..."}}
	var wrap struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &wrap); err == nil && wrap.Error.Message != "" {
		return fmt.Sprintf("[%d %s] %s", httpStatus, wrap.Error.Type, wrap.Error.Message)
	}
	// 不是标准 OpenAI 错误结构，截一段原文
	preview := string(respBody)
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return fmt.Sprintf("[HTTP %d] %s", httpStatus, preview)
}

// injectDefaultModel 如果客户端 body 里没有 model 字段且 credential 有默认模型，自动填上。
// 解析失败或 body 已含 model → 原样返回。
func injectDefaultModel(body []byte, defaultModel string) []byte {
	if defaultModel == "" {
		return body
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	if v, ok := m["model"]; ok && fmt.Sprint(v) != "" {
		return body
	}
	m["model"] = defaultModel
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}
