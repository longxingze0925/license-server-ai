package handler

import (
	"encoding/json"
	"fmt"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

type PublishTaskHandler struct{}

func NewPublishTaskHandler() *PublishTaskHandler {
	return &PublishTaskHandler{}
}

type createPublishTaskRequest struct {
	Action string `json:"action" binding:"required"`
}

// CreateHotUpdateTask 创建热更新异步任务
func (h *PublishTaskHandler) CreateHotUpdateTask(c *gin.Context) {
	hotUpdateID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", hotUpdateID, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	var req createPublishTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	action, ok := parsePublishTaskAction(req.Action)
	if !ok {
		response.BadRequest(c, "不支持的动作")
		return
	}

	task, reused, err := h.createTaskIfNotRunning(
		tenantID,
		hotUpdate.AppID,
		model.PublishTaskResourceHotUpdate,
		hotUpdate.ID,
		action,
		middleware.GetUserID(c),
		middleware.GetUserEmail(c),
	)
	if err != nil {
		response.ServerError(c, "创建任务失败")
		return
	}

	if !reused {
		go h.runHotUpdateTask(task.ID)
	}

	response.Success(c, gin.H{
		"task_id": task.ID,
		"status":  task.Status,
		"reused":  reused,
	})
}

// CreateReleaseTask 创建发布版本异步任务
func (h *PublishTaskHandler) CreateReleaseTask(c *gin.Context) {
	releaseID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var release model.AppRelease
	if err := model.DB.Joins("JOIN applications ON applications.id = app_releases.app_id").
		Where("app_releases.id = ? AND applications.tenant_id = ?", releaseID, tenantID).
		First(&release).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	var req createPublishTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	action, ok := parsePublishTaskAction(req.Action)
	if !ok || action == model.PublishTaskActionRollback {
		response.BadRequest(c, "版本任务仅支持 publish / deprecate")
		return
	}

	task, reused, err := h.createTaskIfNotRunning(
		tenantID,
		release.AppID,
		model.PublishTaskResourceRelease,
		release.ID,
		action,
		middleware.GetUserID(c),
		middleware.GetUserEmail(c),
	)
	if err != nil {
		response.ServerError(c, "创建任务失败")
		return
	}

	if !reused {
		go h.runReleaseTask(task.ID)
	}

	response.Success(c, gin.H{
		"task_id": task.ID,
		"status":  task.Status,
		"reused":  reused,
	})
}

// GetTask 获取任务状态
func (h *PublishTaskHandler) GetTask(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var task model.PublishTask
	if err := model.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error; err != nil {
		response.NotFound(c, "任务不存在")
		return
	}

	var result map[string]interface{}
	if task.ResultJSON != "" {
		_ = json.Unmarshal([]byte(task.ResultJSON), &result)
	}

	response.Success(c, gin.H{
		"id":              task.ID,
		"resource":        task.Resource,
		"resource_id":     task.ResourceID,
		"action":          task.Action,
		"status":          task.Status,
		"progress":        task.Progress,
		"message":         task.Message,
		"error_message":   task.ErrorMessage,
		"result":          result,
		"requested_by":    task.RequestedBy,
		"requested_email": task.RequestedEmail,
		"started_at":      task.StartedAt,
		"finished_at":     task.FinishedAt,
		"created_at":      task.CreatedAt,
		"updated_at":      task.UpdatedAt,
	})
}

func (h *PublishTaskHandler) createTaskIfNotRunning(
	tenantID, appID string,
	resource model.PublishTaskResource,
	resourceID string,
	action model.PublishTaskAction,
	userID, userEmail string,
) (*model.PublishTask, bool, error) {
	var existing model.PublishTask
	err := model.DB.Where(
		"tenant_id = ? AND resource = ? AND resource_id = ? AND action = ? AND status IN ?",
		tenantID,
		resource,
		resourceID,
		action,
		[]model.PublishTaskStatus{model.PublishTaskStatusPending, model.PublishTaskStatusRunning},
	).Order("created_at DESC").First(&existing).Error
	if err == nil {
		return &existing, true, nil
	}

	task := model.PublishTask{
		TenantID:       tenantID,
		AppID:          appID,
		Resource:       resource,
		ResourceID:     resourceID,
		Action:         action,
		Status:         model.PublishTaskStatusPending,
		Progress:       0,
		Message:        "任务已创建",
		RequestedBy:    userID,
		RequestedEmail: userEmail,
	}

	if err := model.DB.Create(&task).Error; err != nil {
		return nil, false, err
	}
	return &task, false, nil
}

func (h *PublishTaskHandler) runHotUpdateTask(taskID string) {
	h.markTaskRunning(taskID, 10, "准备执行")

	var task model.PublishTask
	if err := model.DB.First(&task, "id = ?", taskID).Error; err != nil {
		return
	}

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", task.ResourceID, task.TenantID).
		First(&hotUpdate).Error; err != nil {
		h.markTaskFailed(taskID, "热更新不存在")
		return
	}

	oldStatus := hotUpdate.Status
	newStatus := oldStatus
	now := time.Now()

	h.markTaskRunning(taskID, 50, "执行中")

	switch task.Action {
	case model.PublishTaskActionPublish:
		if hotUpdate.FullURL == "" && hotUpdate.PatchURL == "" {
			h.markTaskFailed(taskID, "请先上传更新包")
			return
		}
		newStatus = model.HotUpdateStatusPublished
		hotUpdate.PublishedAt = &now
	case model.PublishTaskActionDeprecate:
		newStatus = model.HotUpdateStatusDeprecated
	case model.PublishTaskActionRollback:
		newStatus = model.HotUpdateStatusRollback
	default:
		h.markTaskFailed(taskID, "不支持的动作")
		return
	}

	hotUpdate.Status = newStatus
	if err := model.DB.Save(&hotUpdate).Error; err != nil {
		h.markTaskFailed(taskID, "更新状态失败")
		return
	}

	result := map[string]interface{}{
		"resource":      task.Resource,
		"resource_id":   task.ResourceID,
		"action":        task.Action,
		"before_status": oldStatus,
		"after_status":  newStatus,
		"from_version":  hotUpdate.FromVersion,
		"to_version":    hotUpdate.ToVersion,
		"app_id":        hotUpdate.AppID,
	}

	h.writeTaskAuditLog(&task, model.ResourceHotUpdate, task.ResourceID, oldStatus, newStatus, result, 200, "")
	h.markTaskSuccess(taskID, "执行完成", result)
}

func (h *PublishTaskHandler) runReleaseTask(taskID string) {
	h.markTaskRunning(taskID, 10, "准备执行")

	var task model.PublishTask
	if err := model.DB.First(&task, "id = ?", taskID).Error; err != nil {
		return
	}

	var release model.AppRelease
	if err := model.DB.Joins("JOIN applications ON applications.id = app_releases.app_id").
		Where("app_releases.id = ? AND applications.tenant_id = ?", task.ResourceID, task.TenantID).
		First(&release).Error; err != nil {
		h.markTaskFailed(taskID, "版本不存在")
		return
	}

	oldStatus := release.Status
	newStatus := oldStatus
	now := time.Now()

	h.markTaskRunning(taskID, 50, "执行中")

	switch task.Action {
	case model.PublishTaskActionPublish:
		if release.DownloadURL == "" {
			h.markTaskFailed(taskID, "请先上传版本文件")
			return
		}
		newStatus = model.ReleaseStatusPublished
		release.PublishedAt = &now
	case model.PublishTaskActionDeprecate:
		newStatus = model.ReleaseStatusDeprecated
	default:
		h.markTaskFailed(taskID, "不支持的动作")
		return
	}

	release.Status = newStatus
	if err := model.DB.Save(&release).Error; err != nil {
		h.markTaskFailed(taskID, "更新状态失败")
		return
	}

	result := map[string]interface{}{
		"resource":      task.Resource,
		"resource_id":   task.ResourceID,
		"action":        task.Action,
		"before_status": oldStatus,
		"after_status":  newStatus,
		"version":       release.Version,
		"version_code":  release.VersionCode,
		"app_id":        release.AppID,
	}

	h.writeTaskAuditLog(&task, model.ResourceRelease, task.ResourceID, oldStatus, newStatus, result, 200, "")
	h.markTaskSuccess(taskID, "执行完成", result)
}

func (h *PublishTaskHandler) markTaskRunning(taskID string, progress int, message string) {
	now := time.Now()
	updates := map[string]interface{}{
		"status":     model.PublishTaskStatusRunning,
		"progress":   progress,
		"message":    message,
		"started_at": &now,
	}
	if err := model.DB.Model(&model.PublishTask{}).Where("id = ?", taskID).Updates(updates).Error; err != nil {
		log.Printf("更新发布任务运行状态失败: task=%s err=%v", taskID, err)
	}
}

func (h *PublishTaskHandler) markTaskFailed(taskID, errMsg string) {
	now := time.Now()
	if err := model.DB.Model(&model.PublishTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"status":        model.PublishTaskStatusFailed,
		"progress":      100,
		"message":       "执行失败",
		"error_message": errMsg,
		"finished_at":   &now,
	}).Error; err != nil {
		log.Printf("更新发布任务失败状态失败: task=%s err=%v", taskID, err)
	}
}

func (h *PublishTaskHandler) markTaskSuccess(taskID, message string, result map[string]interface{}) {
	resultJSON, _ := json.Marshal(result)
	now := time.Now()
	if err := model.DB.Model(&model.PublishTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"status":      model.PublishTaskStatusSuccess,
		"progress":    100,
		"message":     message,
		"result_json": string(resultJSON),
		"finished_at": &now,
	}).Error; err != nil {
		log.Printf("更新发布任务成功状态失败: task=%s err=%v", taskID, err)
	}
}

func (h *PublishTaskHandler) writeTaskAuditLog(
	task *model.PublishTask,
	resource string,
	resourceID string,
	beforeStatus interface{},
	afterStatus interface{},
	result map[string]interface{},
	responseCode int,
	errMsg string,
) {
	action := model.ActionUpdate
	description := "异步任务执行"
	switch task.Action {
	case model.PublishTaskActionPublish:
		action = model.ActionPublish
		description = "发布"
	case model.PublishTaskActionDeprecate:
		action = model.ActionDeprecate
		description = "废弃"
	case model.PublishTaskActionRollback:
		action = model.ActionRollback
		description = "回滚"
	}

	payload := map[string]interface{}{
		"task_id":         task.ID,
		"task_status":     task.Status,
		"before_status":   beforeStatus,
		"after_status":    afterStatus,
		"result":          result,
		"error_message":   errMsg,
		"requested_by":    task.RequestedBy,
		"requested_email": task.RequestedEmail,
	}
	payloadJSON, _ := json.Marshal(payload)

	auditLog := model.AuditLog{
		TenantID:     task.TenantID,
		UserID:       task.RequestedBy,
		UserEmail:    task.RequestedEmail,
		Action:       action,
		Resource:     resource,
		ResourceID:   resourceID,
		Description:  fmt.Sprintf("%s%s", description, displayAuditResource(resource)),
		IPAddress:    "task",
		UserAgent:    "publish-task-worker",
		RequestBody:  string(payloadJSON),
		ResponseCode: responseCode,
	}
	if err := model.DB.Create(&auditLog).Error; err != nil {
		log.Printf("写入发布任务审计日志失败: task=%s err=%v", task.ID, err)
	}
}

func displayAuditResource(resource string) string {
	switch resource {
	case model.ResourceHotUpdate:
		return "热更新"
	case model.ResourceRelease:
		return "版本"
	default:
		return resource
	}
}

func parsePublishTaskAction(raw string) (model.PublishTaskAction, bool) {
	switch model.PublishTaskAction(raw) {
	case model.PublishTaskActionPublish:
		return model.PublishTaskActionPublish, true
	case model.PublishTaskActionDeprecate:
		return model.PublishTaskActionDeprecate, true
	case model.PublishTaskActionRollback:
		return model.PublishTaskActionRollback, true
	default:
		return "", false
	}
}
