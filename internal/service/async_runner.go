package service

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
	"time"

	"license-server/internal/adapter"
	"license-server/internal/config"
	"license-server/internal/model"

	"github.com/google/uuid"
)

// AsyncRunnerService 处理异步生成任务（视频）的入口与轮询。
//
// 入口（StartTask）：
//  1. Pricing.Match → cost
//  2. Credential.SelectFor → credential
//  3. AdapterRegistry.Get → adapter
//  4. 生成 task_id，Credit.Reserve（事务性预扣）
//  5. 写 generation_tasks(running)
//  6. adapter.Create 调上游，拿到 upstream_task_id（失败立即退点 + 标 failed）
//  7. 立即返回 task_id；后台 poller 异步推进
//
// 轮询（PollOnce）：
//  1. 取出 status=running 的任务
//  2. Decrypt + adapter.Poll
//  3. running → 更新 progress
//     succeeded → 下载所有媒体到 FileStorage、写 generation_files、标任务 succeeded、推 WS（M3.5）
//     failed → 退点、标任务 failed、推 WS
//     超时（默认 2 小时，可配置） → 等同失败
type AsyncRunnerService struct {
	pricing      *PricingService
	credit       *CreditService
	creds        *ProviderCredentialService
	files        *GenerationFileService
	adapters     *adapter.AsyncRegistry
	httpDownload *http.Client

	taskTimeout time.Duration
	notifier    TaskStatusNotifier
}

type TaskStatusNotifier interface {
	NotifyTaskStatus(task *model.GenerationTask)
}

func NewAsyncRunnerService(
	pricing *PricingService,
	credit *CreditService,
	creds *ProviderCredentialService,
	files *GenerationFileService,
	adapters *adapter.AsyncRegistry,
) *AsyncRunnerService {
	return &AsyncRunnerService{
		pricing:      pricing,
		credit:       credit,
		creds:        creds,
		files:        files,
		adapters:     adapters,
		httpDownload: &http.Client{Timeout: 120 * time.Second},
		taskTimeout:  configuredAsyncTaskTimeout(),
	}
}

func configuredAsyncTaskTimeout() time.Duration {
	cfg := config.Get()
	if cfg != nil && cfg.AI.AsyncTaskTimeoutMinutes > 0 {
		return time.Duration(cfg.AI.AsyncTaskTimeoutMinutes) * time.Minute
	}
	return defaultAsyncTaskTimeout
}

func (r *AsyncRunnerService) SetTaskTimeout(timeout time.Duration) {
	r.taskTimeout = timeout
}

func (r *AsyncRunnerService) TaskTimeout() time.Duration {
	return r.taskTimeout
}

func (r *AsyncRunnerService) SetTaskStatusNotifier(notifier TaskStatusNotifier) {
	r.notifier = notifier
}

// StartInput 创建异步任务入参。
type StartInput struct {
	UserID        string
	TenantID      string
	AppID         string
	Provider      model.ProviderKind
	Mode          string
	Scope         model.PricingScope // 通常 video / image
	CredentialID  string             // 为空时按 provider/mode 自动选择；非空时精确使用该后台渠道
	Body          []byte             // 客户端原样转发的 JSON
	PersistedBody []byte             // 写入 DB 的请求摘要；为空时使用 Body
}

// StartOutput 入口返回。
type StartOutput struct {
	TaskID string
	Cost   int
}

// 异步 create 容灾参数。
const asyncCreateMaxAttempts = 3
const defaultAsyncTaskTimeout = 2 * time.Hour

// StartTask 同步部分：扣点 + create 上游 + 返回 task_id。
func (r *AsyncRunnerService) StartTask(ctx context.Context, in StartInput) (*StartOutput, error) {
	if in.UserID == "" || in.Provider == "" || len(in.Body) == 0 {
		return nil, errors.New("缺少必需参数")
	}
	if in.Scope == "" {
		in.Scope = model.PricingScopeVideo
	}

	persistedBody := in.PersistedBody
	if len(persistedBody) == 0 {
		persistedBody = in.Body
	}

	matchParams := extractParams(persistedBody)
	matchParams["mode"] = in.Mode
	priced, err := r.pricing.MatchForTenant(in.TenantID, in.Provider, in.Scope, matchParams)
	if err != nil {
		return nil, fmt.Errorf("计价失败: %w", err)
	}

	a, ok := r.adapters.Get(in.Provider)
	if !ok {
		return nil, ErrAdapterNotFound
	}

	var fixedCred *model.ProviderCredential
	if in.CredentialID != "" {
		fixedCred, err = r.creds.SelectByIDForTenantUse(in.TenantID, in.CredentialID, in.Provider, in.Mode)
		if err != nil {
			return nil, err
		}
	}

	// 预扣（一次）并创建任务。
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
		RequestJSON: string(persistedBody),
	}
	if _, _, err := r.credit.ReserveAndCreateTask(ReserveTaskInput{
		UserID: in.UserID,
		Cost:   int64(priced.Cost),
		Task:   task,
		RuleID: priced.RuleID,
	}); err != nil {
		return nil, err
	}

	// 多 Key 重试 create
	excluded := make([]string, 0, asyncCreateMaxAttempts)
	maxAttempts := asyncCreateMaxAttempts
	if in.CredentialID != "" {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cred, err := r.selectCredentialForCreate(in, excluded, fixedCred)
		if err != nil {
			lastErr = fmt.Errorf("没有可用凭证（attempt=%d）: %w", attempt, err)
			break
		}
		_ = model.DB.Model(&model.GenerationTask{}).Where("id = ?", taskID).
			Update("credential_id", cred.ID).Error

		plainKey, decErr := r.creds.Decrypt(cred)
		if decErr != nil {
			lastErr = fmt.Errorf("解密 Key 失败: %w", decErr)
			excluded = append(excluded, cred.ID)
			continue
		}

		cr, createErr := a.Create(ctx, cred, plainKey, in.Body)
		if createErr == nil {
			if cr == nil {
				zeroBytesService(plainKey)
				lastErr = errors.New("create 返回空结果")
				break
			}
			if cr != nil && len(cr.Media) > 0 {
				pollRes := &adapter.PollResult{
					Status:     adapter.AsyncStatusSucceeded,
					Progress:   1,
					Media:      cr.Media,
					RawSnippet: cr.RawSnippet,
				}
				if err := r.finalizeSucceeded(ctx, task, cred, plainKey, a, pollRes); err != nil {
					zeroBytesService(plainKey)
					if r.files != nil {
						if cleanupErr := r.files.DeleteForTask(ctx, taskID); cleanupErr != nil {
							err = fmt.Errorf("%w; 清理已保存结果失败: %v", err, cleanupErr)
						}
					}
					_, _, _ = r.credit.Refund(taskID, "保存上游媒体失败")
					r.markTaskFailed(taskID, err.Error())
					return nil, err
				}
				zeroBytesService(plainKey)
				return &StartOutput{TaskID: taskID, Cost: priced.Cost}, nil
			}
			if cr.UpstreamTaskID == "" {
				zeroBytesService(plainKey)
				lastErr = errors.New("create 响应缺少 upstream task id")
				break
			}
			if err := model.DB.Model(&model.GenerationTask{}).
				Where("id = ?", taskID).
				Update("upstream_task_id", cr.UpstreamTaskID).Error; err != nil {
				zeroBytesService(plainKey)
				lastErr = fmt.Errorf("更新 upstream_task_id 失败: %w", err)
				break
			}
			zeroBytesService(plainKey)
			return &StartOutput{TaskID: taskID, Cost: priced.Cost}, nil
		}
		zeroBytesService(plainKey)

		lastErr = createErr
		if !adapter.CreateErrorNeedsCredentialRetry(createErr) {
			break
		}
		if adapter.CreateErrorDegradesCredential(createErr) {
			_ = r.creds.MarkHealth(cred.ID, model.CredentialHealthDegraded)
		} else if adapter.CreateErrorMarksCredentialDown(createErr) {
			_ = r.creds.MarkHealth(cred.ID, model.CredentialHealthDown)
		}
		excluded = append(excluded, cred.ID)
	}

	// 创建失败 → 退点 + 标 failed。参数错误不会继续换 Key，也不会误伤凭证健康度。
	if lastErr == nil {
		lastErr = errors.New("上游全部凭证失败")
	}
	refundReason := "上游全部凭证失败"
	if !adapter.CreateErrorNeedsCredentialRetry(lastErr) {
		refundReason = "任务创建失败"
	}
	_, _, _ = r.credit.Refund(taskID, refundReason)
	r.markTaskFailed(taskID, lastErr.Error())
	return nil, lastErr
}

func (r *AsyncRunnerService) selectCredentialForCreate(in StartInput, excluded []string, fixedCred *model.ProviderCredential) (*model.ProviderCredential, error) {
	if fixedCred != nil {
		return fixedCred, nil
	}
	return r.creds.SelectForTenantExcluding(in.TenantID, in.Provider, in.Mode, excluded)
}

// PollOnce 轮询所有 status=running 的任务推进一次。返回处理数量。
//
// 调用方（worker）每隔几秒调一次。第一版没做任务粒度的 next_poll_at，
// 是否到点全部一并轮询；任务量起来后再加 next_poll_at 字段做精细化调度。
func (r *AsyncRunnerService) PollOnce(ctx context.Context) (int, error) {
	recovered, err := r.cleanupTimedOutTasksWithoutUpstreamID(ctx)
	if err != nil {
		return recovered, err
	}

	var tasks []model.GenerationTask
	if err := model.DB.
		Where("status = ?", model.GenerationRunning).
		Where("upstream_task_id <> ''").
		Order("updated_at ASC").
		Limit(20). // 一次最多推进 20 个，避免长事务
		Find(&tasks).Error; err != nil {
		return 0, err
	}

	count := recovered
	for i := range tasks {
		t := &tasks[i]
		if err := r.advanceOne(ctx, t); err != nil {
			// 单条失败不阻塞批次（多半是上游网络抖动），下次再轮
			continue
		}
		count++
	}
	return count, nil
}

func (r *AsyncRunnerService) cleanupTimedOutTasksWithoutUpstreamID(ctx context.Context) (int, error) {
	if r.taskTimeout <= 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var tasks []model.GenerationTask
	if err := model.DB.
		Where("status = ?", model.GenerationRunning).
		Where("(upstream_task_id IS NULL OR upstream_task_id = '')").
		Where("created_at < ?", time.Now().Add(-r.taskTimeout)).
		Order("created_at ASC").
		Limit(20).
		Find(&tasks).Error; err != nil {
		return 0, err
	}

	for i := range tasks {
		task := &tasks[i]
		_, _, _ = r.credit.Refund(task.ID, "任务超时且未获取上游任务编号")
		r.markTaskFailed(task.ID, fmt.Sprintf("任务超时（>%s），未获取上游任务编号", r.taskTimeout))
	}
	return len(tasks), nil
}

func (r *AsyncRunnerService) advanceOne(ctx context.Context, t *model.GenerationTask) error {
	// 超时检查
	if r.taskTimeout > 0 && time.Since(t.CreatedAt) > r.taskTimeout {
		_, _, _ = r.credit.Refund(t.ID, "任务超时")
		r.markTaskFailed(t.ID, fmt.Sprintf("任务超时（>%s）", r.taskTimeout))
		return nil
	}

	// 取凭证
	cred, err := r.creds.Get(t.CredentialID)
	if err != nil {
		// 凭证被删 → 标任务失败 + 退点
		_, _, _ = r.credit.Refund(t.ID, "凭证已被删除")
		r.markTaskFailed(t.ID, "凭证已被删除: "+err.Error())
		return nil
	}
	plainKey, err := r.creds.Decrypt(cred)
	if err != nil {
		return err
	}
	defer zeroBytesService(plainKey)

	a, ok := r.adapters.Get(t.Provider)
	if !ok {
		return ErrAdapterNotFound
	}

	pollRes, err := a.Poll(ctx, cred, plainKey, t.UpstreamTaskID)
	if err != nil {
		return err // 网络抖动等，下次再来
	}

	switch pollRes.Status {
	case adapter.AsyncStatusRunning:
		updates := map[string]any{
			"progress":   pollRes.Progress,
			"updated_at": time.Now(),
		}
		_ = model.DB.Model(&model.GenerationTask{}).Where("id = ?", t.ID).Updates(updates).Error
	case adapter.AsyncStatusFailed:
		_, _, _ = r.credit.Refund(t.ID, "上游报告失败")
		r.markTaskFailed(t.ID, pollRes.Error)
	case adapter.AsyncStatusSucceeded:
		if len(pollRes.Media) == 0 {
			_, _, _ = r.credit.Refund(t.ID, "上游未返回媒体文件")
			r.markTaskFailed(t.ID, "上游任务成功但未返回任何媒体文件")
			return nil
		}
		if err := r.finalizeSucceeded(ctx, t, cred, plainKey, a, pollRes); err != nil {
			// 下载阶段失败：保留任务为 running，等下次再试一次；如果反复失败，等 timeout 兜底
			return err
		}
	}
	return nil
}

func (r *AsyncRunnerService) finalizeSucceeded(
	ctx context.Context,
	t *model.GenerationTask,
	cred *model.ProviderCredential,
	plainKey []byte,
	a adapter.AsyncAdapter,
	res *adapter.PollResult,
) error {
	if r.files == nil {
		return errors.New("file service 未初始化")
	}
	if len(res.Media) == 0 {
		return errors.New("上游任务成功但未返回任何媒体文件")
	}

	existingFiles, err := r.files.ListForTask(t.ID)
	if err != nil {
		return fmt.Errorf("查询已有结果文件失败: %w", err)
	}

	savedFileIDs, resumeFrom := existingSavedFileIDs(existingFiles, len(res.Media))

	for _, m := range res.Media[resumeFrom:] {
		reader, closeReader, err := r.openMediaReader(ctx, cred, plainKey, a, m)
		if err != nil {
			return err
		}
		row, err := r.files.SaveResult(ctx, t.ID, t.UserID, m.Kind, m.MimeType, m.DownloadURL, reader, m.DurationMs, m.Width, m.Height)
		closeReader()
		if err != nil {
			return fmt.Errorf("保存到本地失败: %w", err)
		}
		savedFileIDs = append(savedFileIDs, row.ID)
	}

	resultJSON, _ := json.Marshal(map[string]any{
		"file_ids": savedFileIDs,
	})
	now := time.Now()
	updates := map[string]any{
		"status":       model.GenerationSucceeded,
		"result_json":  string(resultJSON),
		"completed_at": &now,
		"progress":     1.0,
	}
	if err := model.DB.Model(&model.GenerationTask{}).Where("id = ?", t.ID).Updates(updates).Error; err != nil {
		return err
	}

	t.Status = model.GenerationSucceeded
	t.ResultJSON = string(resultJSON)
	t.CompletedAt = &now
	t.Progress = 1.0
	r.notifyTaskStatus(t)
	return nil
}

func existingSavedFileIDs(existingFiles []model.GenerationFile, mediaCount int) ([]string, int) {
	resumeFrom := len(existingFiles)
	if resumeFrom > mediaCount {
		resumeFrom = mediaCount
	}

	savedFileIDs := make([]string, 0, mediaCount)
	for i := 0; i < resumeFrom; i++ {
		savedFileIDs = append(savedFileIDs, existingFiles[i].ID)
	}
	return savedFileIDs, resumeFrom
}

func (r *AsyncRunnerService) openMediaReader(
	ctx context.Context,
	cred *model.ProviderCredential,
	plainKey []byte,
	a adapter.AsyncAdapter,
	m adapter.MediaDescriptor,
) (io.Reader, func(), error) {
	if strings.HasPrefix(m.DownloadURL, "data:") {
		reader, err := decodeDataURI(m.DownloadURL)
		if err != nil {
			return nil, func() {}, fmt.Errorf("解析内联媒体失败: %w", err)
		}
		return reader, func() {}, nil
	}

	req, err := a.BuildDownloadRequest(ctx, cred, plainKey, m)
	if err != nil {
		return nil, func() {}, fmt.Errorf("构造下载请求失败: %w", err)
	}
	resp, err := r.httpDownload.Do(req)
	if err != nil {
		return nil, func() {}, fmt.Errorf("下载上游媒体失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, func() {}, fmt.Errorf("下载上游媒体 HTTP %d", resp.StatusCode)
	}
	return resp.Body, func() { resp.Body.Close() }, nil
}

func decodeDataURI(uri string) (io.Reader, error) {
	parts := strings.SplitN(uri, ",", 2)
	if len(parts) != 2 || !strings.Contains(strings.ToLower(parts[0]), ";base64") {
		return nil, errors.New("不支持的 data URI")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (r *AsyncRunnerService) markTaskFailed(taskID, reason string) {
	now := time.Now()
	errJSON, _ := json.Marshal(map[string]string{"reason": reason})
	updates := map[string]any{
		"status":       model.GenerationFailed,
		"error_json":   string(errJSON),
		"completed_at": &now,
	}
	model.DB.Model(&model.GenerationTask{}).Where("id = ?", taskID).Updates(updates)

	var task model.GenerationTask
	if err := model.DB.First(&task, "id = ?", taskID).Error; err == nil {
		task.Status = model.GenerationFailed
		task.ErrorJSON = string(errJSON)
		task.CompletedAt = &now
		r.notifyTaskStatus(&task)
	}
}

func (r *AsyncRunnerService) notifyTaskStatus(task *model.GenerationTask) {
	if r.notifier != nil && task != nil {
		r.notifier.NotifyTaskStatus(task)
	}
}
