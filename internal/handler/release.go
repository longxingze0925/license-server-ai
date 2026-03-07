package handler

import (
	"fmt"
	"io"
	"license-server/internal/config"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type ReleaseHandler struct{}

func NewReleaseHandler() *ReleaseHandler {
	return &ReleaseHandler{}
}

// CreateReleaseRequest 创建版本请求
type CreateReleaseRequest struct {
	Version     string `json:"version" binding:"required"`
	VersionCode int    `json:"version_code" binding:"required"`
	Changelog   string `json:"changelog"`
	ForceUpdate bool   `json:"force_update"`
}

// Create 创建版本（不带文件）
func (h *ReleaseHandler) Create(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var req CreateReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 检查版本号是否已存在
	var existingRelease model.AppRelease
	if err := model.DB.Where("app_id = ? AND version = ?", appID, req.Version).First(&existingRelease).Error; err == nil {
		response.Error(c, 400, "版本号已存在")
		return
	}

	release := model.AppRelease{
		AppID:       appID,
		Version:     req.Version,
		VersionCode: req.VersionCode,
		Changelog:   req.Changelog,
		ForceUpdate: req.ForceUpdate,
		Status:      model.ReleaseStatusDraft,
	}

	if err := model.DB.Create(&release).Error; err != nil {
		response.ServerError(c, "创建版本失败")
		return
	}

	response.Success(c, gin.H{
		"id":           release.ID,
		"version":      release.Version,
		"version_code": release.VersionCode,
		"status":       release.Status,
		"created_at":   release.CreatedAt,
	})
}

// Upload 上传版本文件
func (h *ReleaseHandler) Upload(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 获取表单数据
	version := c.PostForm("version")
	versionCodeStr := c.PostForm("version_code")
	changelog := c.PostForm("changelog")
	forceUpdate := c.PostForm("force_update") == "true"

	if version == "" || versionCodeStr == "" {
		response.BadRequest(c, "请提供版本号")
		return
	}

	versionCode, _ := strconv.Atoi(versionCodeStr)

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传文件")
		return
	}
	defer file.Close()

	// 保存文件并计算哈希（流式处理，避免大文件占用过多内存）
	cfg := config.Get()
	maxReleaseUploadBytes := int64(cfg.Security.MaxReleaseUploadMB) << 20
	if maxReleaseUploadBytes <= 0 {
		maxReleaseUploadBytes = 500 << 20
	}
	if header.Size > maxReleaseUploadBytes {
		response.BadRequest(c, fmt.Sprintf("版本文件过大，最大支持 %dMB", maxReleaseUploadBytes>>20))
		return
	}
	filename := fmt.Sprintf("%s_%s%s", app.AppKey, version, filepath.Ext(header.Filename))
	filePath := filepath.Join(cfg.Storage.ReleasesDir, filename)
	fileSize, fileHash, err := saveUploadedFile(&io.LimitedReader{R: file, N: maxReleaseUploadBytes + 1}, filePath)
	if err != nil {
		response.ServerError(c, "保存文件失败: "+err.Error())
		return
	}
	if fileSize > maxReleaseUploadBytes {
		os.Remove(filePath)
		response.BadRequest(c, fmt.Sprintf("版本文件过大，最大支持 %dMB", maxReleaseUploadBytes>>20))
		return
	}

	downloadURL := fmt.Sprintf("/api/client/releases/download/%s", filename)

	// 检查版本是否已存在
	var existingRelease model.AppRelease
	if err := model.DB.Where("app_id = ? AND version = ?", appID, version).First(&existingRelease).Error; err == nil {
		// 更新现有版本
		existingRelease.DownloadURL = downloadURL
		existingRelease.FileSize = fileSize
		existingRelease.FileHash = fileHash
		existingRelease.Changelog = changelog
		existingRelease.ForceUpdate = forceUpdate
		model.DB.Save(&existingRelease)

		response.Success(c, gin.H{
			"id":           existingRelease.ID,
			"version":      existingRelease.Version,
			"download_url": existingRelease.DownloadURL,
			"file_size":    existingRelease.FileSize,
			"file_hash":    existingRelease.FileHash,
			"updated":      true,
		})
		return
	}

	// 创建新版本
	release := model.AppRelease{
		AppID:       appID,
		Version:     version,
		VersionCode: versionCode,
		DownloadURL: downloadURL,
		Changelog:   changelog,
		FileSize:    fileSize,
		FileHash:    fileHash,
		ForceUpdate: forceUpdate,
		Status:      model.ReleaseStatusDraft,
	}

	if err := model.DB.Create(&release).Error; err != nil {
		response.ServerError(c, "创建版本失败")
		return
	}

	response.Success(c, gin.H{
		"id":           release.ID,
		"version":      release.Version,
		"download_url": release.DownloadURL,
		"file_size":    release.FileSize,
		"file_hash":    release.FileHash,
		"created":      true,
	})
}

// List 获取版本列表
func (h *ReleaseHandler) List(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var releases []model.AppRelease
	model.DB.Where("app_id = ?", appID).Order("version_code DESC").Find(&releases)

	var result []gin.H
	for _, release := range releases {
		result = append(result, gin.H{
			"id":                 release.ID,
			"version":            release.Version,
			"version_code":       release.VersionCode,
			"download_url":       release.DownloadURL,
			"changelog":          release.Changelog,
			"file_size":          release.FileSize,
			"file_hash":          release.FileHash,
			"force_update":       release.ForceUpdate,
			"rollout_percentage": release.RolloutPercentage,
			"status":             release.Status,
			"created_at":         release.CreatedAt,
		})
	}

	response.Success(c, result)
}

// Get 获取版本详情
func (h *ReleaseHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var release model.AppRelease
	if err := model.DB.Joins("JOIN applications ON applications.id = app_releases.app_id").
		Where("app_releases.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&release).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	response.Success(c, gin.H{
		"id":                 release.ID,
		"app_id":             release.AppID,
		"version":            release.Version,
		"version_code":       release.VersionCode,
		"download_url":       release.DownloadURL,
		"changelog":          release.Changelog,
		"file_size":          release.FileSize,
		"file_hash":          release.FileHash,
		"force_update":       release.ForceUpdate,
		"rollout_percentage": release.RolloutPercentage,
		"status":             release.Status,
		"created_at":         release.CreatedAt,
	})
}

// Update 更新版本
func (h *ReleaseHandler) Update(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var release model.AppRelease
	if err := model.DB.Joins("JOIN applications ON applications.id = app_releases.app_id").
		Where("app_releases.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&release).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	var req struct {
		Changelog         string `json:"changelog"`
		ForceUpdate       *bool  `json:"force_update"`
		RolloutPercentage int    `json:"rollout_percentage"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if req.Changelog != "" {
		updates["changelog"] = req.Changelog
	}
	if req.ForceUpdate != nil {
		updates["force_update"] = *req.ForceUpdate
	}
	if req.RolloutPercentage > 0 && req.RolloutPercentage <= 100 {
		updates["rollout_percentage"] = req.RolloutPercentage
	}

	model.DB.Model(&release).Updates(updates)

	response.SuccessWithMessage(c, "更新成功", nil)
}

// Publish 发布版本
func (h *ReleaseHandler) Publish(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var release model.AppRelease
	if err := model.DB.Joins("JOIN applications ON applications.id = app_releases.app_id").
		Where("app_releases.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&release).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	if release.DownloadURL == "" {
		response.Error(c, 400, "请先上传版本文件")
		return
	}

	now := time.Now()
	release.Status = model.ReleaseStatusPublished
	release.PublishedAt = &now
	model.DB.Save(&release)

	response.SuccessWithMessage(c, "发布成功", nil)
}

// Deprecate 废弃版本
func (h *ReleaseHandler) Deprecate(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var release model.AppRelease
	if err := model.DB.Joins("JOIN applications ON applications.id = app_releases.app_id").
		Where("app_releases.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&release).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	release.Status = model.ReleaseStatusDeprecated
	model.DB.Save(&release)

	response.SuccessWithMessage(c, "已废弃", nil)
}

// Delete 删除版本
func (h *ReleaseHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var release model.AppRelease
	if err := model.DB.Joins("JOIN applications ON applications.id = app_releases.app_id").
		Where("app_releases.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&release).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	// 删除文件
	if release.DownloadURL != "" {
		cfg := config.Get()
		filename := filepath.Base(release.DownloadURL)
		filePath := filepath.Join(cfg.Storage.ReleasesDir, filename)
		os.Remove(filePath)
	}

	model.DB.Delete(&release)

	response.SuccessWithMessage(c, "删除成功", nil)
}

// DownloadRelease 下载版本文件（客户端）
func (h *ReleaseHandler) DownloadRelease(c *gin.Context) {
	filename, ok := getSafeDownloadFilename(c)
	if !ok {
		return
	}

	app, _, ok := validateClientDownloadContext(c, filename, downloadTokenKindRelease)
	if !ok {
		return
	}

	var release model.AppRelease
	if err := model.DB.Where(
		"app_id = ? AND status = ? AND download_url LIKE ?",
		app.ID, model.ReleaseStatusPublished, "%/"+filename,
	).Order("version_code DESC").First(&release).Error; err != nil {
		response.NotFound(c, "文件不存在")
		return
	}

	cfg := config.Get()
	filePath := filepath.Join(cfg.Storage.ReleasesDir, filename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		response.NotFound(c, "文件不存在")
		return
	}

	c.File(filePath)
}
