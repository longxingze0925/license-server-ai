package handler

import (
	"errors"
	"net/http"
	"os"
	"strconv"

	"license-server/internal/middleware"
	"license-server/internal/pkg/response"
	"license-server/internal/service"

	"github.com/gin-gonic/gin"
)

// GenerationFileHandler 用户端文件下载/列表/删除。
type GenerationFileHandler struct {
	svc *service.GenerationFileService
}

func NewGenerationFileHandler() *GenerationFileHandler {
	return &GenerationFileHandler{svc: service.NewGenerationFileService()}
}

// List GET /api/proxy/files
func (h *GenerationFileHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	rows, total, err := h.svc.ListForUser(userID, page, pageSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.SuccessPage(c, rows, total, page, pageSize)
}

// Download GET /api/proxy/files/:id
//
// 用 http.ServeContent 实现，自动支持 Range（视频拖进度条用）+ Last-Modified/ETag。
func (h *GenerationFileHandler) Download(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	row, err := h.svc.GetForUser(c.Param("id"), userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFileNotFound):
			response.NotFound(c, "文件不存在或已过期")
		case errors.Is(err, service.ErrFileForbidden):
			response.Forbidden(c, "无权访问该文件")
		default:
			response.ServerError(c, err.Error())
		}
		return
	}

	r, err := h.svc.Open(c.Request.Context(), row)
	if err != nil {
		if os.IsNotExist(err) {
			response.NotFound(c, "文件已被清理（达到保留天数）")
			return
		}
		response.ServerError(c, "打开文件失败: "+err.Error())
		return
	}
	defer r.Close()

	c.Header("Content-Type", row.MimeType)
	c.Header("X-File-Id", row.ID)
	c.Header("X-Task-Id", row.TaskID)
	// http.ServeContent 接管 Content-Length / Range / ETag / 206
	http.ServeContent(c.Writer, c.Request, "", row.UpdatedAt, r)
}

// Delete DELETE /api/proxy/files/:id
func (h *GenerationFileHandler) Delete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		response.Unauthorized(c, "未登录")
		return
	}
	if err := h.svc.DeleteForUser(c.Request.Context(), c.Param("id"), userID); err != nil {
		switch {
		case errors.Is(err, service.ErrFileNotFound):
			response.NotFound(c, "文件不存在")
		case errors.Is(err, service.ErrFileForbidden):
			response.Forbidden(c, "无权删除该文件")
		default:
			response.ServerError(c, err.Error())
		}
		return
	}
	response.Success(c, gin.H{"id": c.Param("id")})
}
