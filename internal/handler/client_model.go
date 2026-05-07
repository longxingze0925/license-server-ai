package handler

import (
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

// ClientModelHandler 管理后台配置客户端可见模型和真实渠道路由。
type ClientModelHandler struct {
	svc *service.ClientModelService
}

func NewClientModelHandler() *ClientModelHandler {
	return &ClientModelHandler{svc: service.NewClientModelService()}
}

type clientModelRequest struct {
	ModelKey        string   `json:"model_key" binding:"required"`
	DisplayName     string   `json:"display_name" binding:"required"`
	Provider        string   `json:"provider" binding:"required"`
	Scope           string   `json:"scope" binding:"required"`
	Enabled         *bool    `json:"enabled"`
	SortOrder       int      `json:"sort_order"`
	SupportedModes  []string `json:"supported_modes"`
	SupportedScopes []string `json:"supported_scopes"`
	AspectRatios    []string `json:"aspect_ratios"`
	Durations       []string `json:"durations"`
	Note            string   `json:"note"`
}

type clientModelRouteRequest struct {
	CredentialID  string   `json:"credential_id" binding:"required"`
	UpstreamModel string   `json:"upstream_model" binding:"required"`
	Enabled       *bool    `json:"enabled"`
	IsDefault     *bool    `json:"is_default"`
	Priority      int      `json:"priority"`
	SortOrder     int      `json:"sort_order"`
	AspectRatios  []string `json:"aspect_ratios"`
	Durations     []string `json:"durations"`
	Resolutions   []string `json:"resolutions"`
	MaxImages     int      `json:"max_images"`
	Note          string   `json:"note"`
}

func (h *ClientModelHandler) List(c *gin.Context) {
	includeDisabled := c.Query("include_disabled") == "true" || c.Query("include_disabled") == "1"
	rows, err := h.svc.List(middleware.GetTenantID(c), includeDisabled)
	if err != nil {
		response.ServerError(c, "查询失败: "+err.Error())
		return
	}

	list := make([]gin.H, 0, len(rows))
	for _, item := range rows {
		list = append(list, clientModelToView(item.Model, item.Routes))
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(max(len(list), 1))))
	response.SuccessPage(c, list, int64(len(list)), page, pageSize)
}

func (h *ClientModelHandler) Get(c *gin.Context) {
	row, routes, err := h.svc.Get(middleware.GetTenantID(c), c.Param("id"))
	if err != nil {
		response.NotFound(c, "客户端模型不存在")
		return
	}
	response.Success(c, clientModelToView(*row, routes))
}

func (h *ClientModelHandler) Create(c *gin.Context) {
	var req clientModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	row, err := h.svc.Create(middleware.GetTenantID(c), clientModelInputFromRequest(req))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, clientModelToView(*row, nil))
}

func (h *ClientModelHandler) Update(c *gin.Context) {
	var req clientModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	row, err := h.svc.Update(middleware.GetTenantID(c), c.Param("id"), clientModelInputFromRequest(req))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "客户端模型不存在")
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	_, routes, _ := h.svc.Get(middleware.GetTenantID(c), row.ID)
	response.Success(c, clientModelToView(*row, routes))
}

func (h *ClientModelHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(middleware.GetTenantID(c), c.Param("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "客户端模型不存在")
			return
		}
		response.ServerError(c, "删除失败: "+err.Error())
		return
	}
	response.Success(c, gin.H{"id": c.Param("id")})
}

func (h *ClientModelHandler) CreateRoute(c *gin.Context) {
	var req clientModelRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	row, err := h.svc.CreateRoute(middleware.GetTenantID(c), c.Param("id"), clientModelRouteInputFromRequest(req))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "客户端模型不存在")
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, clientModelRouteToView(*row))
}

func (h *ClientModelHandler) UpdateRoute(c *gin.Context) {
	var req clientModelRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	row, err := h.svc.UpdateRoute(middleware.GetTenantID(c), c.Param("route_id"), clientModelRouteInputFromRequest(req))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "路由不存在")
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, clientModelRouteToView(*row))
}

func (h *ClientModelHandler) DeleteRoute(c *gin.Context) {
	if err := h.svc.DeleteRoute(middleware.GetTenantID(c), c.Param("route_id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "路由不存在")
			return
		}
		response.ServerError(c, "删除失败: "+err.Error())
		return
	}
	response.Success(c, gin.H{"id": c.Param("route_id")})
}

func (h *ClientModelHandler) ListUpstreamCapabilities(c *gin.Context) {
	provider := model.ProviderKind(strings.ToLower(strings.TrimSpace(c.Query("provider"))))
	mode := strings.TrimSpace(c.Query("mode"))
	response.Success(c, service.ListUpstreamModelCapabilities(provider, mode))
}

func clientModelInputFromRequest(req clientModelRequest) service.ClientModelInput {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return service.ClientModelInput{
		ModelKey:        strings.TrimSpace(req.ModelKey),
		DisplayName:     strings.TrimSpace(req.DisplayName),
		Provider:        model.ProviderKind(strings.ToLower(strings.TrimSpace(req.Provider))),
		Scope:           model.PricingScope(strings.ToLower(strings.TrimSpace(req.Scope))),
		Enabled:         enabled,
		SortOrder:       req.SortOrder,
		SupportedModes:  req.SupportedModes,
		SupportedScopes: req.SupportedScopes,
		AspectRatios:    req.AspectRatios,
		Durations:       req.Durations,
		Note:            strings.TrimSpace(req.Note),
	}
}

func clientModelRouteInputFromRequest(req clientModelRouteRequest) service.ClientModelRouteInput {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	isDefault := false
	if req.IsDefault != nil {
		isDefault = *req.IsDefault
	}
	return service.ClientModelRouteInput{
		CredentialID:  strings.TrimSpace(req.CredentialID),
		UpstreamModel: strings.TrimSpace(req.UpstreamModel),
		Enabled:       enabled,
		IsDefault:     isDefault,
		Priority:      req.Priority,
		SortOrder:     req.SortOrder,
		AspectRatios:  req.AspectRatios,
		Durations:     req.Durations,
		Resolutions:   req.Resolutions,
		MaxImages:     req.MaxImages,
		Note:          strings.TrimSpace(req.Note),
	}
}

func clientModelToView(row model.ClientModel, routes []model.ClientModelRoute) gin.H {
	routeViews := make([]gin.H, 0, len(routes))
	for _, route := range routes {
		routeViews = append(routeViews, clientModelRouteToView(route))
	}
	return gin.H{
		"id":               row.ID,
		"tenant_id":        row.TenantID,
		"model_key":        row.ModelKey,
		"display_name":     row.DisplayName,
		"provider":         row.Provider,
		"scope":            row.Scope,
		"enabled":          row.Enabled,
		"sort_order":       row.SortOrder,
		"supported_modes":  service.ParseClientModelJSONStrings(row.SupportedModes),
		"supported_scopes": service.ParseClientModelJSONStrings(row.SupportedScopes),
		"aspect_ratios":    service.ParseClientModelJSONStrings(row.AspectRatios),
		"durations":        service.ParseClientModelJSONStrings(row.Durations),
		"note":             row.Note,
		"routes":           routeViews,
		"created_at":       row.CreatedAt,
		"updated_at":       row.UpdatedAt,
	}
}

func clientModelRouteToView(row model.ClientModelRoute) gin.H {
	view := gin.H{
		"id":                      row.ID,
		"tenant_id":               row.TenantID,
		"client_model_id":         row.ClientModelID,
		"credential_id":           row.CredentialID,
		"upstream_model":          row.UpstreamModel,
		"enabled":                 row.Enabled,
		"is_default":              row.IsDefault,
		"priority":                row.Priority,
		"sort_order":              row.SortOrder,
		"aspect_ratios":           service.ParseClientModelJSONStrings(row.AspectRatios),
		"durations":               service.ParseClientModelJSONStrings(row.Durations),
		"resolutions":             service.ParseClientModelJSONStrings(row.Resolutions),
		"max_images":              row.MaxImages,
		"effective_aspect_ratios": service.ResolveRouteAspectRatios(row),
		"effective_durations":     service.ResolveRouteDurations(row),
		"effective_resolutions":   service.ResolveRouteResolutions(row),
		"effective_max_images":    service.ResolveRouteMaxImages(row),
		"note":                    row.Note,
		"created_at":              row.CreatedAt,
		"updated_at":              row.UpdatedAt,
	}
	if row.Credential != nil {
		view["credential"] = credentialToView(row.Credential)
	}
	return view
}
