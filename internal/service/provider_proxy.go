package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"license-server/internal/adapter"
	"license-server/internal/model"

	"github.com/google/uuid"
)

// ProviderProxyService 把"客户端发起一次 chat 请求"翻译成完整后台流程。
//
// 步骤（同步路径）：
//  1. 读 JWT 的 user_id（由调用方传入）
//  2. 计价：Pricing.Match(provider, scope, params) → cost
//  3. 生成 task_id 并预扣点：Credit.Reserve(user, cost, taskID)
//  4. 写 generation_tasks(running)
//  5. 选凭证：Credential.SelectFor(provider, mode)
//  6. 解密 Key（明文只在 stack 上活几毫秒）
//  7. Adapter.BuildChat() → 发上游
//  8. 成功 → 写 generation_tasks(succeeded) → 返回 upstream body
//     失败 → Refund + 写 generation_tasks(failed)
type ProviderProxyService struct {
	pricing  *PricingService
	credit   *CreditService
	creds    *ProviderCredentialService
	adapters *adapter.AdapterRegistry
	http     *http.Client
}

func NewProviderProxyService(
	pricing *PricingService,
	credit *CreditService,
	creds *ProviderCredentialService,
	adapters *adapter.AdapterRegistry,
) *ProviderProxyService {
	return &ProviderProxyService{
		pricing:  pricing,
		credit:   credit,
		creds:    creds,
		adapters: adapters,
		http: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

// ChatInput 入参。Body 是客户端原样发的 OpenAI 风格请求体。
// Mode / Scope 来自 query / body，由 handler 解析后传入。
type ChatInput struct {
	UserID       string
	TenantID     string
	AppID        string
	Provider     model.ProviderKind
	Mode         string
	Scope        model.PricingScope
	CredentialID string
	Body         []byte
}

// ChatOutput 同步返回。
type ChatOutput struct {
	TaskID       string
	HTTPStatus   int
	UpstreamBody []byte
	UpstreamHdrs http.Header
	Cost         int
}

// 错误。
var (
	ErrAdapterNotFound = errors.New("该 Provider 暂不支持转发")
)

// 同步 chat 容灾参数。
const chatMaxAttempts = 3

func (s *ProviderProxyService) Chat(ctx context.Context, in ChatInput) (*ChatOutput, error) {
	if in.UserID == "" {
		return nil, errors.New("user_id 不能为空")
	}
	if in.Provider == "" {
		return nil, errors.New("provider 不能为空")
	}
	if len(in.Body) == 0 {
		return nil, errors.New("请求体不能为空")
	}
	if in.Scope == "" {
		in.Scope = model.PricingScopeChat
	}

	matchParams := extractParams(in.Body)
	matchParams["mode"] = in.Mode

	// 1) 计价
	priced, err := s.pricing.MatchForTenant(in.TenantID, in.Provider, in.Scope, matchParams)
	if err != nil {
		return nil, fmt.Errorf("计价失败: %w", err)
	}

	// 2) Adapter（无可用直接拒，无副作用）
	a, ok := s.adapters.GetChat(in.Provider)
	if !ok {
		return nil, ErrAdapterNotFound
	}

	var fixedCred *model.ProviderCredential
	if in.CredentialID != "" {
		fixedCred, err = s.creds.SelectByIDForTenantUse(in.TenantID, in.CredentialID, in.Provider, in.Mode)
		if err != nil {
			return nil, err
		}
	}

	// 3) 预扣点（一次性）并创建任务。
	taskID := uuid.New().String()
	task := &model.GenerationTask{
		BaseModel:   model.BaseModel{ID: taskID},
		UserID:      in.UserID,
		TenantID:    in.TenantID,
		AppID:       in.AppID,
		Provider:    in.Provider,
		Mode:        in.Mode,
		Status:      model.GenerationRunning,
		RuleID:      priced.RuleID,
		Cost:        priced.Cost,
		RequestJSON: string(in.Body),
	}
	if _, _, err := s.credit.ReserveAndCreateTask(ReserveTaskInput{
		UserID: in.UserID,
		Cost:   int64(priced.Cost),
		Task:   task,
		RuleID: priced.RuleID,
	}); err != nil {
		return nil, err
	}

	// 4) 多 Key 重试：每次选不同的凭证
	excluded := make([]string, 0, chatMaxAttempts)
	maxAttempts := chatMaxAttempts
	if in.CredentialID != "" {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cred, err := s.selectCredentialForChat(in, excluded, fixedCred)
		if err != nil {
			lastErr = fmt.Errorf("没有可用凭证（attempt=%d）: %w", attempt, err)
			break
		}

		// 记录当前用的凭证（成功或最终失败时都需要）
		_ = model.DB.Model(&model.GenerationTask{}).Where("id = ?", taskID).
			Update("credential_id", cred.ID).Error

		out, retriable, attemptErr := s.tryChatOnce(ctx, a, cred, taskID, in.Body)
		if attemptErr == nil {
			// 成功 → 写完成 + 标健康
			completedAt := time.Now()
			_ = model.DB.Model(&model.GenerationTask{}).Where("id = ?", taskID).Updates(map[string]any{
				"status":       model.GenerationSucceeded,
				"result_json":  string(out.UpstreamBody),
				"completed_at": &completedAt,
				"progress":     1.0,
			}).Error
			_ = s.creds.MarkHealth(cred.ID, model.CredentialHealthHealthy)
			out.TaskID = taskID
			out.Cost = priced.Cost
			return out, nil
		}
		lastErr = attemptErr
		excluded = append(excluded, cred.ID)
		if !retriable {
			break // 不是凭证导致的（如计价、参数错误）
		}
	}

	// 5) 所有 attempt 失败 → 退点 + 标 task failed
	_, _, _ = s.credit.Refund(taskID, "上游全部凭证失败")
	s.markTaskFailed(taskID, lastErr.Error())
	return nil, lastErr
}

func (s *ProviderProxyService) selectCredentialForChat(in ChatInput, excluded []string, fixedCred *model.ProviderCredential) (*model.ProviderCredential, error) {
	if fixedCred != nil {
		return fixedCred, nil
	}
	return s.creds.SelectForTenantExcluding(in.TenantID, in.Provider, in.Mode, excluded)
}

// tryChatOnce 用一条凭证尝试一次同步 chat。
// 返回 retriable=true 表示"换一条凭证可能成功"（401/403/网络错误）；false 表示业务级错误（如 400 参数错）。
func (s *ProviderProxyService) tryChatOnce(ctx context.Context, a adapter.ChatAdapter, cred *model.ProviderCredential, taskID string, body []byte) (*ChatOutput, bool, error) {
	plainKey, err := s.creds.Decrypt(cred)
	if err != nil {
		// 解密失败属于配置错误，不重试
		return nil, false, fmt.Errorf("解密 Key 失败: %w", err)
	}
	defer zeroBytesService(plainKey)

	req, err := a.BuildChat(ctx, cred, plainKey, body)
	if err != nil {
		return nil, false, fmt.Errorf("构造上游请求失败: %w", err)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		_ = s.creds.MarkHealth(cred.ID, model.CredentialHealthDown)
		return nil, true, fmt.Errorf("上游网络错误: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &ChatOutput{
			HTTPStatus:   resp.StatusCode,
			UpstreamBody: bodyBytes,
			UpstreamHdrs: resp.Header,
		}, false, nil
	}

	reason := a.ParseError(resp.StatusCode, bodyBytes)
	switch resp.StatusCode {
	case 401, 403:
		_ = s.creds.MarkHealth(cred.ID, model.CredentialHealthDegraded)
		return nil, true, fmt.Errorf("上游 401/403: %s", reason)
	case 429, 502, 503, 504:
		// rate limit / 上游波动 → 健康度不变，但本条凭证暂时换走
		return nil, true, fmt.Errorf("上游 %d: %s", resp.StatusCode, reason)
	default:
		// 4xx 业务错（多半是 prompt 内容/参数问题）→ 不重试
		return nil, false, fmt.Errorf("上游返回错误: %s", reason)
	}
}

// markTaskFailed 写 generation_task(failed) + error_json。
func (s *ProviderProxyService) markTaskFailed(taskID, reason string) {
	now := time.Now()
	errJSON, _ := json.Marshal(map[string]string{"reason": reason})
	model.DB.Model(&model.GenerationTask{}).Where("id = ?", taskID).Updates(map[string]any{
		"status":       model.GenerationFailed,
		"error_json":   string(errJSON),
		"completed_at": &now,
	})
}

// extractParams 从客户端请求体里抓出计价规则可能用到的字段。
// 当前抓取：model / duration(_seconds) / resolution / size / n / aspect_ratio / reference_image_count。
func extractParams(body []byte) map[string]any {
	out := map[string]any{}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return out
	}
	for _, k := range []string{"model", "duration_seconds", "duration", "resolution", "size", "n", "aspect_ratio", "reference_image_count", "input_image_count"} {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	if v, ok := m["durationSeconds"]; ok {
		setPricingParamIfMissing(out, "duration_seconds", v)
	}
	if v, ok := m["aspectRatio"]; ok {
		setPricingParamIfMissing(out, "aspect_ratio", v)
	}
	if parameters, ok := m["parameters"].(map[string]any); ok {
		if v, ok := parameters["durationSeconds"]; ok {
			setPricingParamIfMissing(out, "duration_seconds", v)
		}
		if v, ok := parameters["aspectRatio"]; ok {
			setPricingParamIfMissing(out, "aspect_ratio", v)
		}
		if v, ok := parameters["resolution"]; ok {
			setPricingParamIfMissing(out, "resolution", v)
		}
	}
	return NormalizePricingParams(out)
}

func setPricingParamIfMissing(params map[string]any, key string, value any) {
	if _, ok := params[key]; ok {
		return
	}
	params[key] = value
}

func NormalizePricingParams(params map[string]any) map[string]any {
	out := make(map[string]any, len(params)+2)
	for key, value := range params {
		out[key] = value
	}
	if _, ok := out["duration_seconds"]; !ok {
		if v, ok := out["duration"]; ok {
			out["duration_seconds"] = v
		}
	}
	if _, ok := out["duration"]; !ok {
		if v, ok := out["duration_seconds"]; ok {
			out["duration"] = v
		}
	}
	return out
}

func zeroBytesService(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// 占位：bytes 未直接用，留作以后流式转发使用。
var _ = bytes.NewReader
