package handler

import (
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type AuditHandler struct{}

func NewAuditHandler() *AuditHandler {
	return &AuditHandler{}
}

// List 获取审计日志列表
func (h *AuditHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	userID := c.Query("user_id")
	action := c.Query("action")
	resource := c.Query("resource")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	tenantID := middleware.GetTenantID(c)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := model.DB.Model(&model.AuditLog{}).Where("tenant_id = ?", tenantID)

	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if action != "" {
		query = query.Where("action = ?", action)
	}
	if resource != "" {
		query = query.Where("resource = ?", resource)
	}
	var ok bool
	query, ok = applyCreatedAtDateRange(c, query, startDate, endDate)
	if !ok {
		return
	}

	var total int64
	query.Count(&total)

	var logs []model.AuditLog
	query.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&logs)

	response.SuccessPage(c, logs, total, page, pageSize)
}

// Get 获取审计日志详情
func (h *AuditHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var log model.AuditLog
	if err := model.DB.First(&log, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		response.NotFound(c, "日志不存在")
		return
	}

	response.Success(c, log)
}

// GetStats 获取审计统计
func (h *AuditHandler) GetStats(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 365 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)
	tenantID := middleware.GetTenantID(c)

	// 按操作类型统计
	var actionStats []struct {
		Action string `json:"action"`
		Count  int64  `json:"count"`
	}
	model.DB.Model(&model.AuditLog{}).
		Select("action, count(*) as count").
		Where("tenant_id = ? AND created_at >= ?", tenantID, since).
		Group("action").
		Find(&actionStats)

	// 按资源类型统计
	var resourceStats []struct {
		Resource string `json:"resource"`
		Count    int64  `json:"count"`
	}
	model.DB.Model(&model.AuditLog{}).
		Select("resource, count(*) as count").
		Where("tenant_id = ? AND created_at >= ?", tenantID, since).
		Group("resource").
		Find(&resourceStats)

	// 按用户统计
	var userStats []struct {
		UserEmail string `json:"user_email"`
		Count     int64  `json:"count"`
	}
	model.DB.Model(&model.AuditLog{}).
		Select("user_email, count(*) as count").
		Where("tenant_id = ? AND created_at >= ?", tenantID, since).
		Where("user_email != ''").
		Group("user_email").
		Order("count DESC").
		Limit(10).
		Find(&userStats)

	response.Success(c, gin.H{
		"action_stats":   actionStats,
		"resource_stats": resourceStats,
		"user_stats":     userStats,
	})
}
