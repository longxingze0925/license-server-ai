package handler

import (
	"errors"
	"strconv"

	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"license-server/internal/service"

	"github.com/gin-gonic/gin"
)

// GenerationTaskHandler 任务查询（用户 + admin）。
type GenerationTaskHandler struct {
	svc *service.GenerationTaskService
}

func NewGenerationTaskHandler() *GenerationTaskHandler {
	return &GenerationTaskHandler{svc: service.NewGenerationTaskService()}
}

// MyTask GET /api/proxy/tasks/:id
func (h *GenerationTaskHandler) MyTask(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	t, files, err := h.svc.GetForUser(c.Param("id"), userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTaskNotFound):
			response.NotFound(c, "任务不存在")
		case errors.Is(err, service.ErrTaskForbidden):
			response.Forbidden(c, "无权访问该任务")
		default:
			response.ServerError(c, err.Error())
		}
		return
	}
	response.Success(c, gin.H{
		"task":  t,
		"files": files,
	})
}

// MyList GET /api/proxy/tasks
func (h *GenerationTaskHandler) MyList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	f := service.TaskListFilter{
		UserID:   userID,
		Provider: c.Query("provider"),
		Page:     page,
		PageSize: pageSize,
	}
	if v := c.Query("status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			st := model.GenerationStatus(n)
			f.Status = &st
		}
	}
	rows, total, err := h.svc.ListForUser(f)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.SuccessPage(c, taskListItems(rows), total, page, pageSize)
}

// AdminList GET /api/admin/proxy/tasks
func (h *GenerationTaskHandler) AdminList(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	f := service.AdminTaskFilter{
		TenantID: tenantID,
		UserID:   c.Query("user_id"),
		Provider: c.Query("provider"),
		Page:     page,
		PageSize: pageSize,
	}
	if v := c.Query("status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			st := model.GenerationStatus(n)
			f.Status = &st
		}
	}
	rows, total, err := h.svc.AdminList(f)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.SuccessPage(c, taskListItems(rows), total, page, pageSize)
}

func taskListItems(rows []model.GenerationTask) []gin.H {
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, gin.H{
			"id":               row.ID,
			"user_id":          row.UserID,
			"tenant_id":        row.TenantID,
			"app_id":           row.AppID,
			"provider":         row.Provider,
			"mode":             row.Mode,
			"credential_id":    row.CredentialID,
			"upstream_task_id": row.UpstreamTaskID,
			"status":           row.Status,
			"progress":         row.Progress,
			"rule_id":          row.RuleID,
			"cost":             row.Cost,
			"has_request":      row.RequestJSON != "",
			"has_result":       row.ResultJSON != "",
			"has_error":        row.ErrorJSON != "",
			"completed_at":     row.CompletedAt,
			"created_at":       row.CreatedAt,
			"updated_at":       row.UpdatedAt,
		})
	}
	return items
}
