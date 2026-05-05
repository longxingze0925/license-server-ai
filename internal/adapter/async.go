package adapter

import (
	"context"
	"net/http"

	"license-server/internal/model"
)

// AsyncStatus 上游异步任务状态。
type AsyncStatus string

const (
	AsyncStatusRunning   AsyncStatus = "running"
	AsyncStatusSucceeded AsyncStatus = "succeeded"
	AsyncStatusFailed    AsyncStatus = "failed"
)

// MediaDescriptor 异步任务完成后返回的一份媒体（视频/图片/缩略图）。
type MediaDescriptor struct {
	Kind        model.GenerationFileKind
	DownloadURL string            // 上游给出的下载链接
	AuthHeaders map[string]string // 下载时需要带的额外头（部分上游签名 URL 不需要）
	MimeType    string
	DurationMs  int
	Width       int
	Height      int
}

// CreateResult 异步任务创建后的返回。
type CreateResult struct {
	UpstreamTaskID string
	// Media is set by adapters whose upstream call already returns final files
	// (for example image generation). When present, the runner can finalize
	// without writing a large synthetic payload into upstream_task_id.
	Media []MediaDescriptor
	// 建议的首次轮询延迟（秒），poller 可以参考；为 0 时使用默认 5s。
	NextPollAfterSec int
	// 调试用：原始响应 body 摘要。
	RawSnippet string
}

// PollResult 一次轮询的结果。
type PollResult struct {
	Status     AsyncStatus
	Progress   float32           // 0.0 - 1.0
	Media      []MediaDescriptor // succeeded 时填
	Error      string            // failed 时填
	RawSnippet string
}

// AsyncAdapter 异步生成（视频/长任务）的协议适配器。
//
// 协议两步走：Create → Poll；poll 直到拿到 succeeded 或 failed。
// 实现负责：构造上游 HTTP 请求；解析响应 JSON；提取 upstream_task_id 与媒体描述。
type AsyncAdapter interface {
	Provider() model.ProviderKind

	// Create 发起任务。body 是客户端原样转发的请求体（JSON）。
	Create(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, body []byte) (*CreateResult, error)

	// Poll 查询任务状态。upstreamTaskID 来自 Create 返回的同名字段。
	Poll(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, upstreamTaskID string) (*PollResult, error)

	// BuildDownloadRequest 构造下载某份媒体的 HTTP 请求（写到本机磁盘前用一次）。
	// 大多数上游用预签名 URL，可以走默认实现；adapter 想加鉴权头/查询参数时覆盖。
	BuildDownloadRequest(ctx context.Context, cred *model.ProviderCredential, plainKey []byte, m MediaDescriptor) (*http.Request, error)
}

// AsyncRegistry 按 Provider 取 AsyncAdapter。
type AsyncRegistry struct {
	adapters map[model.ProviderKind]AsyncAdapter
}

func NewAsyncRegistry() *AsyncRegistry {
	return &AsyncRegistry{
		adapters: map[model.ProviderKind]AsyncAdapter{
			model.ProviderSora: &SoraAdapter{},
			model.ProviderVeo:  &VeoAdapter{},
			model.ProviderGpt:  &OpenAIImageAdapter{}, // 仅支持图片生成
			model.ProviderGrok: &GrokVideoAdapter{},
		},
	}
}

func (r *AsyncRegistry) Get(p model.ProviderKind) (AsyncAdapter, bool) {
	a, ok := r.adapters[p]
	return a, ok
}

// 默认下载实现：直接 GET，附带 adapter 指定的额外头。
func defaultDownload(ctx context.Context, m MediaDescriptor) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range m.AuthHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}
