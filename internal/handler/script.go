package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

type ScriptHandler struct{}

func NewScriptHandler() *ScriptHandler {
	return &ScriptHandler{}
}

// Upload 上传脚本
func (h *ScriptHandler) Upload(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	// 验证应用是否存在
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传文件")
		return
	}
	defer file.Close()

	version := c.PostForm("version")
	if version == "" {
		response.BadRequest(c, "请提供版本号")
		return
	}

	maxScriptUploadBytes := getMaxScriptUploadSizeBytes()
	if header.Size > maxScriptUploadBytes {
		response.BadRequest(c, fmt.Sprintf("脚本文件过大，最大支持 %dMB", maxScriptUploadBytes>>20))
		return
	}

	// 读取文件内容（限制大小，防止内存占用过高）
	content, err := readUploadedContentWithLimit(file, maxScriptUploadBytes)
	if err != nil {
		if err == errUploadFileTooLarge {
			response.BadRequest(c, fmt.Sprintf("脚本文件过大，最大支持 %dMB", maxScriptUploadBytes>>20))
			return
		}
		response.ServerError(c, "读取文件失败")
		return
	}

	// 计算哈希
	hash := sha256.Sum256(content)
	contentHash := hex.EncodeToString(hash[:])

	filename := filepath.Base(header.Filename)

	// 检查是否已存在同名脚本
	var existingScript model.Script
	if err := model.DB.Where("app_id = ? AND filename = ?", appID, filename).First(&existingScript).Error; err == nil {
		// 更新现有脚本
		existingScript.Version = version
		existingScript.Content = content
		existingScript.ContentHash = contentHash
		existingScript.FileSize = int64(len(content))
		if err := model.DB.Save(&existingScript).Error; err != nil {
			response.ServerError(c, "更新脚本失败: "+err.Error())
			return
		}

		response.Success(c, gin.H{
			"id":       existingScript.ID,
			"filename": existingScript.Filename,
			"version":  existingScript.Version,
			"hash":     existingScript.ContentHash,
			"size":     existingScript.FileSize,
			"updated":  true,
		})
		return
	}

	// 创建新脚本
	script := model.Script{
		AppID:       appID,
		Filename:    filename,
		Version:     version,
		Content:     content,
		ContentHash: contentHash,
		FileSize:    int64(len(content)),
		IsEncrypted: false,
		Status:      model.ScriptStatusActive,
	}

	if err := model.DB.Create(&script).Error; err != nil {
		response.ServerError(c, "保存脚本失败")
		return
	}

	response.Success(c, gin.H{
		"id":       script.ID,
		"filename": script.Filename,
		"version":  script.Version,
		"hash":     script.ContentHash,
		"size":     script.FileSize,
		"created":  true,
	})
}

// List 获取脚本列表
func (h *ScriptHandler) List(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	// 验证应用是否属于当前租户
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var scripts []model.Script
	model.DB.Where("app_id = ?", appID).Order("filename ASC").Find(&scripts)

	var result []gin.H
	for _, script := range scripts {
		result = append(result, gin.H{
			"id":                 script.ID,
			"filename":           script.Filename,
			"version":            script.Version,
			"hash":               script.ContentHash,
			"size":               script.FileSize,
			"is_encrypted":       script.IsEncrypted,
			"rollout_percentage": script.RolloutPercentage,
			"status":             script.Status,
			"created_at":         script.CreatedAt,
			"updated_at":         script.UpdatedAt,
		})
	}

	response.Success(c, result)
}

// Get 获取脚本详情
func (h *ScriptHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.Script
	if err := model.DB.Joins("JOIN applications ON applications.id = scripts.app_id").
		Where("scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	response.Success(c, gin.H{
		"id":                 script.ID,
		"app_id":             script.AppID,
		"filename":           script.Filename,
		"version":            script.Version,
		"hash":               script.ContentHash,
		"size":               script.FileSize,
		"is_encrypted":       script.IsEncrypted,
		"rollout_percentage": script.RolloutPercentage,
		"status":             script.Status,
		"created_at":         script.CreatedAt,
		"updated_at":         script.UpdatedAt,
	})
}

// UpdateScript 更新脚本配置
func (h *ScriptHandler) Update(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.Script
	if err := model.DB.Joins("JOIN applications ON applications.id = scripts.app_id").
		Where("scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	var req struct {
		RolloutPercentage *int   `json:"rollout_percentage"`
		Status            string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if req.RolloutPercentage != nil {
		if *req.RolloutPercentage < 0 || *req.RolloutPercentage > 100 {
			response.BadRequest(c, "灰度比例必须在 0 到 100 之间")
			return
		}
		updates["rollout_percentage"] = *req.RolloutPercentage
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}

	if err := model.DB.Model(&script).Updates(updates).Error; err != nil {
		response.ServerError(c, "更新脚本配置失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

// Delete 删除脚本
func (h *ScriptHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.Script
	if err := model.DB.Joins("JOIN applications ON applications.id = scripts.app_id").
		Where("scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	if err := model.DB.Delete(&script).Error; err != nil {
		response.ServerError(c, "删除脚本失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
}

// Download 下载脚本（管理端）
func (h *ScriptHandler) Download(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var script model.Script
	if err := model.DB.Joins("JOIN applications ON applications.id = scripts.app_id").
		Where("scripts.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&script).Error; err != nil {
		response.NotFound(c, "脚本不存在")
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+script.Filename)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Script-Version", script.Version)
	c.Data(200, "application/octet-stream", script.Content)
}
