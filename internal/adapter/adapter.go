// Package adapter 把客户端请求翻译成各 AI Provider 上游 HTTP 请求。
//
// 第一阶段（M2）只实现 GPT chat（OpenAI Chat Completions 协议）；
// Gemini / Veo / Sora / Grok / Claude 在 M3-M5 里逐个加进 Registry。
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"license-server/internal/model"
)

// ChatAdapter 把"聊天/分析"类请求翻译成上游 HTTP 调用。
//
// 实现要点：
//   - 只在内存中持有明文 Key，函数返回前不写日志。
//   - 上游 URL = credential.UpstreamBase + 各厂商专属路径。
//   - custom_headers 从 credential.CustomHeader 解析后逐个 Set 进 req.Header。
type ChatAdapter interface {
	Provider() model.ProviderKind
	// BuildChat 构造同步 chat 上游请求；body 是客户端原样转发的 OpenAI 风格 JSON。
	BuildChat(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*http.Request, error)
	// ParseError 把上游非 2xx 响应翻译成"统一错误"字符串，提取真正的失败原因。
	ParseError(httpStatus int, respBody []byte) string
}

// AdapterRegistry 提供按 ProviderKind 取 ChatAdapter 的工厂。
type AdapterRegistry struct {
	chats map[model.ProviderKind]ChatAdapter
}

func NewRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		chats: map[model.ProviderKind]ChatAdapter{
			model.ProviderGpt:    &OpenAIChatAdapter{},
			model.ProviderGemini: &GeminiChatAdapter{},
			// 后续扩展：
			// model.ProviderClaude: &AnthropicChatAdapter{},
		},
	}
}

func (r *AdapterRegistry) GetChat(p model.ProviderKind) (ChatAdapter, bool) {
	a, ok := r.chats[p]
	return a, ok
}

// applyCustomHeaders 解析 credential.CustomHeader（JSON 字符串）并写到 req.Header。
// 失败时静默跳过（已经在前端做了一次 JSON 校验，到这里仍然失败属于异常配置）。
func applyCustomHeaders(req *http.Request, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return
	}
	m := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return
	}
	for k, v := range m {
		if isReservedCustomHeader(k) {
			continue
		}
		req.Header.Set(k, fmt.Sprint(v))
	}
}

func isReservedCustomHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "content-type", "content-length", "host":
		return true
	default:
		return false
	}
}

// drainBody 工具：读取上游 body 但限制大小（避免把 GB 级响应吞掉内存）。
func drainBody(r io.Reader, max int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, max))
}

// 占位：drainBody 暂时未在 adapter 内部使用，但 proxy 服务会复用同一个限制策略。
var _ = drainBody
