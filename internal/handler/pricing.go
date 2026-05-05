package handler

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"license-server/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PricingHandler 计价规则后台 API。
type PricingHandler struct {
	svc *service.PricingService
}

func NewPricingHandler() *PricingHandler {
	return &PricingHandler{svc: service.NewPricingService()}
}

type pricingRuleRequest struct {
	Provider  string `json:"provider" binding:"required"` // 通配可填 '*'
	Scope     string `json:"scope" binding:"required"`    // image|video|analysis|chat
	MatchJSON string `json:"match_json"`                  // JSON 字符串，可空（默认 "{}" = 任意）
	Credits   int    `json:"credits"`                     // 公式为空时使用
	Formula   string `json:"formula"`                     // 可选公式
	Priority  int    `json:"priority"`
	Enabled   *bool  `json:"enabled"`
	Note      string `json:"note"`
}

func (h *PricingHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	rows, total, err := h.svc.ListForTenant(middleware.GetTenantID(c), c.Query("provider"), c.Query("scope"), page, pageSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.SuccessPage(c, rows, total, page, pageSize)
}

func (h *PricingHandler) Get(c *gin.Context) {
	id, ok := parsePricingRuleID(c)
	if !ok {
		return
	}
	r, err := h.svc.GetForTenant(middleware.GetTenantID(c), id)
	if err != nil {
		response.NotFound(c, "规则不存在")
		return
	}
	response.Success(c, r)
}

func (h *PricingHandler) Create(c *gin.Context) {
	var req pricingRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	req = normalizePricingRuleRequest(req)
	if err := validatePricingRuleRequest(req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	r := &model.PricingRule{
		TenantID:  middleware.GetTenantID(c),
		Provider:  req.Provider,
		Scope:     model.PricingScope(req.Scope),
		MatchJSON: normalizeMatchJSON(req.MatchJSON),
		Credits:   req.Credits,
		Formula:   req.Formula,
		Priority:  req.Priority,
		Note:      req.Note,
	}
	if req.Enabled != nil {
		r.Enabled = *req.Enabled
	} else {
		r.Enabled = true
	}
	if err := h.svc.Create(r); err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.Success(c, r)
}

func (h *PricingHandler) Update(c *gin.Context) {
	id, ok := parsePricingRuleID(c)
	if !ok {
		return
	}
	r, err := h.svc.GetForTenant(middleware.GetTenantID(c), id)
	if err != nil {
		response.NotFound(c, "规则不存在")
		return
	}
	var req pricingRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	req = normalizePricingRuleRequest(req)
	if err := validatePricingRuleRequest(req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	r.Provider = req.Provider
	r.Scope = model.PricingScope(req.Scope)
	r.MatchJSON = normalizeMatchJSON(req.MatchJSON)
	r.Credits = req.Credits
	r.Formula = req.Formula
	r.Priority = req.Priority
	r.Note = req.Note
	if req.Enabled != nil {
		r.Enabled = *req.Enabled
	}
	if err := h.svc.Update(r); err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.Success(c, r)
}

func (h *PricingHandler) Delete(c *gin.Context) {
	id, ok := parsePricingRuleID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteForTenant(middleware.GetTenantID(c), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "规则不存在")
			return
		}
		response.ServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"id": id})
}

func parsePricingRuleID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "规则 ID 无效")
		return 0, false
	}
	return id, true
}

// Preview 试算：给出 provider/scope/params，返回会扣多少点 + 命中哪条规则。
type previewRequest struct {
	Provider string         `json:"provider" binding:"required"`
	Scope    string         `json:"scope" binding:"required"`
	Params   map[string]any `json:"params"`
}

func (h *PricingHandler) Preview(c *gin.Context) {
	var req previewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	req.Params = service.NormalizePricingParams(req.Params)
	res, err := h.svc.MatchForTenant(middleware.GetTenantID(c), model.ProviderKind(req.Provider), model.PricingScope(req.Scope), req.Params)
	if err != nil {
		response.Success(c, gin.H{
			"matched": false,
			"reason":  err.Error(),
		})
		return
	}
	response.Success(c, gin.H{
		"matched": true,
		"cost":    res.Cost,
		"rule_id": res.RuleID,
		"rule":    res.RuleRef,
	})
}

func normalizeMatchJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}
	return s
}

func normalizePricingRuleRequest(req pricingRuleRequest) pricingRuleRequest {
	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	req.Scope = strings.ToLower(strings.TrimSpace(req.Scope))
	req.MatchJSON = normalizeMatchJSON(req.MatchJSON)
	req.Formula = strings.TrimSpace(req.Formula)
	req.Note = strings.TrimSpace(req.Note)
	return req
}

func validatePricingRuleRequest(req pricingRuleRequest) error {
	req = normalizePricingRuleRequest(req)
	if !isValidPricingRuleProvider(req.Provider) {
		return errors.New("provider 不支持")
	}
	if !isValidPricingRuleScope(req.Scope) {
		return errors.New("scope 不支持")
	}
	if req.Credits <= 0 && strings.TrimSpace(req.Formula) == "" {
		return errors.New("credits 与 formula 至少一个非空")
	}
	if req.Formula != "" {
		if err := service.ValidatePricingFormulaSyntax(req.Formula); err != nil {
			return errors.New("formula 不合法: " + err.Error())
		}
	}
	matchJSON := normalizeMatchJSON(req.MatchJSON)
	var probe map[string]any
	if err := json.Unmarshal([]byte(matchJSON), &probe); err != nil {
		return err
	}
	return nil
}

func isValidPricingRuleProvider(provider string) bool {
	switch model.ProviderKind(provider) {
	case model.ProviderGemini, model.ProviderGpt, model.ProviderVeo, model.ProviderSora, model.ProviderGrok:
		return true
	default:
		return provider == "*"
	}
}

func isValidPricingRuleScope(scope string) bool {
	switch model.PricingScope(scope) {
	case model.PricingScopeImage, model.PricingScopeVideo, model.PricingScopeAnalysis, model.PricingScopeChat:
		return true
	default:
		return false
	}
}
