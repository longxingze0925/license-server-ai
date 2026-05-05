package handler

import (
	"errors"
	"strconv"

	"license-server/internal/middleware"
	"license-server/internal/pkg/response"
	"license-server/internal/service"

	"github.com/gin-gonic/gin"
)

// CreditHandler 用户额度的 admin + user 端 API。
type CreditHandler struct {
	svc *service.CreditService
}

func NewCreditHandler() *CreditHandler {
	return &CreditHandler{svc: service.NewCreditService()}
}

// ===================== 普通用户：自己看 =====================

// MyBalance GET /api/credits/me
func (h *CreditHandler) MyBalance(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	row, err := h.svc.Get(userID)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.Success(c, row)
}

// MyTransactions GET /api/credits/me/transactions
func (h *CreditHandler) MyTransactions(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	rows, total, err := h.svc.ListTransactions(userID, page, pageSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.SuccessPage(c, rows, total, page, pageSize)
}

// ===================== 后台：管理 =====================

// AdminListUsers GET /admin/credits/users
func (h *CreditHandler) AdminListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	rows, total, err := h.svc.ListUsers(middleware.GetTenantID(c), c.Query("keyword"), page, pageSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.SuccessPage(c, rows, total, page, pageSize)
}

// AdminGetUser GET /admin/credits/users/:id
func (h *CreditHandler) AdminGetUser(c *gin.Context) {
	row, err := h.svc.GetForTenant(c.Param("id"), middleware.GetTenantID(c))
	if err != nil {
		if errors.Is(err, service.ErrUserNotInTenant) {
			response.NotFound(c, "用户不存在")
			return
		}
		response.NotFound(c, err.Error())
		return
	}
	response.Success(c, row)
}

type adjustRequest struct {
	Amount int64  `json:"amount" binding:"required"` // 正负皆可
	Note   string `json:"note"`
}

// AdminAdjust POST /admin/credits/users/:id/adjust
func (h *CreditHandler) AdminAdjust(c *gin.Context) {
	var req adjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	operator := middleware.GetUserID(c)
	row, tx, err := h.svc.AdjustForTenant(c.Param("id"), middleware.GetTenantID(c), req.Amount, operator, req.Note)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientBalance) {
			response.Error(c, 402, "扣减后余额会变负，操作被拒绝")
			return
		}
		if errors.Is(err, service.ErrUserNotInTenant) {
			response.NotFound(c, "用户不存在")
			return
		}
		response.ServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{
		"credit":      row,
		"transaction": tx,
	})
}

type setLimitsRequest struct {
	ConcurrentLimit int `json:"concurrent_limit" binding:"required"`
}

// AdminSetLimits PUT /admin/credits/users/:id/limits
func (h *CreditHandler) AdminSetLimits(c *gin.Context) {
	var req setLimitsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.svc.SetConcurrentLimitForTenant(c.Param("id"), middleware.GetTenantID(c), req.ConcurrentLimit); err != nil {
		if errors.Is(err, service.ErrUserNotInTenant) {
			response.NotFound(c, "用户不存在")
			return
		}
		response.ServerError(c, err.Error())
		return
	}
	row, _ := h.svc.GetForTenant(c.Param("id"), middleware.GetTenantID(c))
	response.Success(c, row)
}

// AdminUserTransactions GET /admin/credits/users/:id/transactions
func (h *CreditHandler) AdminUserTransactions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	rows, total, err := h.svc.ListTransactionsForTenant(c.Param("id"), middleware.GetTenantID(c), page, pageSize)
	if err != nil {
		if errors.Is(err, service.ErrUserNotInTenant) {
			response.NotFound(c, "用户不存在")
			return
		}
		response.ServerError(c, err.Error())
		return
	}
	response.SuccessPage(c, rows, total, page, pageSize)
}
