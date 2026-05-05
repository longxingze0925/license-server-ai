package handler

import (
	"errors"
	"fmt"
	"io"
	"license-server/internal/config"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HotUpdateHandler struct{}

func NewHotUpdateHandler() *HotUpdateHandler {
	return &HotUpdateHandler{}
}

func normalizeHotUpdateLogStatus(status string) (model.HotUpdateLogStatus, bool) {
	switch model.HotUpdateLogStatus(strings.ToLower(strings.TrimSpace(status))) {
	case model.HotUpdateLogStatusPending:
		return model.HotUpdateLogStatusPending, true
	case model.HotUpdateLogStatusDownloading:
		return model.HotUpdateLogStatusDownloading, true
	case model.HotUpdateLogStatusInstalling:
		return model.HotUpdateLogStatusInstalling, true
	case model.HotUpdateLogStatusSuccess:
		return model.HotUpdateLogStatusSuccess, true
	case model.HotUpdateLogStatusFailed:
		return model.HotUpdateLogStatusFailed, true
	case model.HotUpdateLogStatusRollback:
		return model.HotUpdateLogStatusRollback, true
	default:
		return "", false
	}
}

func isTerminalHotUpdateLogStatus(status model.HotUpdateLogStatus) bool {
	return status == model.HotUpdateLogStatusSuccess ||
		status == model.HotUpdateLogStatusFailed ||
		status == model.HotUpdateLogStatusRollback
}

func shouldApplyHotUpdateLogStatusTransition(previousStatus, nextStatus model.HotUpdateLogStatus) bool {
	if previousStatus == "" {
		return true
	}
	if previousStatus == nextStatus {
		return true
	}
	if previousStatus == model.HotUpdateLogStatusFailed {
		return true
	}
	return !isTerminalHotUpdateLogStatus(previousStatus)
}

func hotUpdateStatusCounterDelta(previousStatus, nextStatus model.HotUpdateLogStatus) (int, int) {
	if previousStatus == nextStatus {
		return 0, 0
	}

	successDelta := 0
	failDelta := 0
	switch previousStatus {
	case model.HotUpdateLogStatusSuccess:
		successDelta--
	case model.HotUpdateLogStatusFailed:
		failDelta--
	}
	switch nextStatus {
	case model.HotUpdateLogStatusSuccess:
		successDelta++
	case model.HotUpdateLogStatusFailed:
		failDelta++
	}
	return successDelta, failDelta
}

func nonNegativeCounterExpr(column string, delta int) interface{} {
	if delta >= 0 {
		return gorm.Expr(column+" + ?", delta)
	}
	step := -delta
	return gorm.Expr("CASE WHEN "+column+" >= ? THEN "+column+" - ? ELSE 0 END", step, step)
}

func removeFileIfSaved(filePath string) {
	if filePath != "" {
		_ = os.Remove(filePath)
	}
}

func normalizeHotUpdateUploadType(value string) (string, bool) {
	uploadType := strings.ToLower(strings.TrimSpace(value))
	if uploadType == "" {
		return "full", true
	}
	if uploadType == "full" || uploadType == "patch" {
		return uploadType, true
	}
	return "", false
}

func removeReplacedHotUpdateFile(root, oldURL, newURL string) {
	oldName := filepath.Base(oldURL)
	if oldName == "" || oldName == "." || oldName == filepath.Base(newURL) {
		return
	}
	_ = os.Remove(filepath.Join(root, oldName))
}

func applyHotUpdateStatusCounterDelta(tx *gorm.DB, hotUpdateID string, previousStatus, nextStatus model.HotUpdateLogStatus) error {
	successDelta, failDelta := hotUpdateStatusCounterDelta(previousStatus, nextStatus)
	if successDelta == 0 && failDelta == 0 {
		return nil
	}

	updates := map[string]interface{}{}
	if successDelta != 0 {
		updates["success_count"] = nonNegativeCounterExpr("success_count", successDelta)
	}
	if failDelta != 0 {
		updates["fail_count"] = nonNegativeCounterExpr("fail_count", failDelta)
	}
	return tx.Model(&model.HotUpdate{}).Where("id = ?", hotUpdateID).Updates(updates).Error
}

func hotUpdateSupportsCurrentVersion(hotUpdate model.HotUpdate, currentVersion string) bool {
	minVersion := strings.TrimSpace(hotUpdate.MinAppVersion)
	return minVersion == "" || compareVersionStrings(currentVersion, minVersion) >= 0
}

func compareVersionStrings(left, right string) int {
	leftParts := splitVersionParts(left)
	rightParts := splitVersionParts(right)
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}

	for i := 0; i < maxLen; i++ {
		lv := "0"
		if i < len(leftParts) {
			lv = leftParts[i]
		}
		rv := "0"
		if i < len(rightParts) {
			rv = rightParts[i]
		}

		li, lErr := strconv.Atoi(lv)
		ri, rErr := strconv.Atoi(rv)
		if lErr == nil && rErr == nil {
			if li < ri {
				return -1
			}
			if li > ri {
				return 1
			}
			continue
		}

		cmp := strings.Compare(strings.ToLower(lv), strings.ToLower(rv))
		if cmp < 0 {
			return -1
		}
		if cmp > 0 {
			return 1
		}
	}
	return 0
}

func splitVersionParts(version string) []string {
	fields := strings.FieldsFunc(strings.TrimSpace(version), func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == '+' || r == ' '
	})
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			parts = append(parts, field)
		}
	}
	return parts
}

// ==================== 管理端接口 ====================

// CreateRequest 创建热更新请求
type CreateHotUpdateRequest struct {
	FromVersion       string `json:"from_version" form:"from_version"`
	ToVersion         string `json:"to_version" form:"to_version"`
	VersionCode       int    `json:"version_code" form:"version_code"`
	PatchType         string `json:"patch_type" form:"update_type"`
	UpdateMode        string `json:"update_mode" form:"update_mode"`
	Changelog         string `json:"changelog" form:"changelog"`
	ForceUpdate       bool   `json:"force_update" form:"force_update"`
	RestartRequired   bool   `json:"restart_required" form:"restart_required"`
	RolloutPercentage int    `json:"rollout_percentage" form:"rollout_percentage"`
	MinAppVersion     string `json:"min_app_version" form:"min_app_version"`
}

// Create 创建热更新记录（支持同时上传文件）
func (h *HotUpdateHandler) Create(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 获取表单字段
	version := c.PostForm("version")
	versionCodeStr := c.PostForm("version_code")
	updateType := c.PostForm("update_type")
	updateMode := c.PostForm("update_mode")
	changelog := c.PostForm("changelog")
	rolloutStr := c.PostForm("rollout_percentage")
	forceUpdate := c.PostForm("force_update") == "true"
	restartRequired := c.PostForm("restart_required") == "true"
	minAppVersion := c.PostForm("min_app_version")
	savedFilePath := ""

	// 验证必填字段
	if version == "" {
		response.BadRequest(c, "版本号不能为空")
		return
	}
	normalizedVersion, err := normalizePackageVersion(version)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	version = normalizedVersion

	versionCode := scriptVersionCode(version)
	if strings.TrimSpace(versionCodeStr) != "" {
		parsedVersionCode, err := strconv.Atoi(versionCodeStr)
		if err != nil || parsedVersionCode < 0 {
			response.BadRequest(c, "版本代码必须是非负整数")
			return
		}
		versionCode = parsedVersionCode
	}
	if versionCode <= 0 {
		response.BadRequest(c, "版本代码必须大于 0，或使用可解析的版本号")
		return
	}

	rolloutPercentage := 100
	if strings.TrimSpace(rolloutStr) != "" {
		parsedRollout, err := strconv.Atoi(rolloutStr)
		if err != nil || parsedRollout < 0 || parsedRollout > 100 {
			response.BadRequest(c, "灰度比例必须在 0 到 100 之间")
			return
		}
		rolloutPercentage = parsedRollout
	}

	uploadType, ok := normalizeHotUpdateUploadType(updateType)
	if !ok {
		response.BadRequest(c, "更新类型只能是 full 或 patch")
		return
	}
	updateType = uploadType

	// 确定更新类型
	patchType := model.HotUpdateTypeFull
	if updateType == "patch" {
		patchType = model.HotUpdateTypePatch
	}

	// 检查是否已存在相同版本的热更新
	var existing model.HotUpdate
	if err := model.DB.Where("app_id = ? AND to_version = ?",
		appID, version).First(&existing).Error; err == nil {
		response.Error(c, 400, "该版本热更新已存在")
		return
	}

	hotUpdate := model.HotUpdate{
		AppID:           appID,
		FromVersion:     "*", // 默认从任意版本更新
		ToVersion:       version,
		VersionCode:     versionCode,
		PatchType:       patchType,
		UpdateMode:      updateMode,
		Changelog:       changelog,
		ForceUpdate:     forceUpdate,
		RestartRequired: restartRequired,
		MinAppVersion:   minAppVersion,
		RolloutPercent:  rolloutPercentage,
		Status:          model.HotUpdateStatusDraft,
	}

	// 检查是否有上传文件
	file, header, err := c.Request.FormFile("file")
	if err == nil {
		defer file.Close()

		// 保存文件
		cfg := config.Get()
		maxUploadBytes := int64(cfg.Security.MaxReleaseUploadMB) << 20
		if maxUploadBytes <= 0 {
			maxUploadBytes = 500 << 20
		}
		if header.Size > maxUploadBytes {
			response.BadRequest(c, fmt.Sprintf("更新包过大，最大支持 %dMB", maxUploadBytes>>20))
			return
		}
		hotUpdateDir := filepath.Join(cfg.Storage.ReleasesDir, "hotupdate")
		if err := os.MkdirAll(hotUpdateDir, 0755); err != nil {
			response.ServerError(c, "创建目录失败: "+err.Error())
			return
		}

		filename := fmt.Sprintf("%s_any_to_%s_%s_%d%s",
			app.AppKey, version, uploadType, time.Now().UnixNano(), filepath.Ext(header.Filename))
		filePath := filepath.Join(hotUpdateDir, filename)

		stagedFile, err := stageUploadedFile(&io.LimitedReader{R: file, N: maxUploadBytes + 1}, filePath)
		if err != nil {
			response.ServerError(c, "保存文件失败: "+err.Error())
			return
		}
		defer stagedFile.Cleanup()
		fileSize := stagedFile.Size
		fileHash := stagedFile.Hash
		if fileSize > maxUploadBytes {
			response.BadRequest(c, fmt.Sprintf("更新包过大，最大支持 %dMB", maxUploadBytes>>20))
			return
		}
		savedFilePath = filePath
		fileSignature, err := signFileSignature(app.PrivateKey, fileHash, fileSize)
		if err != nil {
			response.ServerError(c, err.Error())
			return
		}
		if err := stagedFile.Commit(); err != nil {
			response.ServerError(c, "保存文件失败: "+err.Error())
			return
		}

		downloadURL := fmt.Sprintf("/api/client/hotupdate/download/%s", filename)

		// 设置文件信息
		if uploadType == "patch" {
			hotUpdate.PatchURL = downloadURL
			hotUpdate.PatchSize = fileSize
			hotUpdate.PatchHash = fileHash
			hotUpdate.PatchSignature = fileSignature
		} else {
			hotUpdate.FullURL = downloadURL
			hotUpdate.FullSize = fileSize
			hotUpdate.FullHash = fileHash
			hotUpdate.FullSignature = fileSignature
		}
	}

	tx := model.DB.Begin()
	if err := tx.Create(&hotUpdate).Error; err != nil {
		tx.Rollback()
		removeFileIfSaved(savedFilePath)
		response.ServerError(c, "创建热更新失败: "+err.Error())
		return
	}
	if rolloutPercentage == 0 {
		if err := tx.Model(&hotUpdate).UpdateColumn("rollout_percent", 0).Error; err != nil {
			tx.Rollback()
			removeFileIfSaved(savedFilePath)
			response.ServerError(c, "创建热更新失败: "+err.Error())
			return
		}
		hotUpdate.RolloutPercent = 0
	}
	if err := tx.Commit().Error; err != nil {
		removeFileIfSaved(savedFilePath)
		response.ServerError(c, "创建热更新失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"id":           hotUpdate.ID,
		"from_version": hotUpdate.FromVersion,
		"to_version":   hotUpdate.ToVersion,
		"version_code": hotUpdate.VersionCode,
		"status":       hotUpdate.Status,
		"created_at":   hotUpdate.CreatedAt,
	})
}

// Upload 上传热更新包
func (h *HotUpdateHandler) Upload(c *gin.Context) {
	appID := c.Param("id")
	hotUpdateID := c.Param("hotupdate_id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ? AND app_id = ?", hotUpdateID, appID).Error; err != nil {
		response.NotFound(c, "热更新记录不存在")
		return
	}

	// 获取上传类型 (patch 或 full)
	uploadType, ok := normalizeHotUpdateUploadType(c.PostForm("type"))
	if !ok {
		response.BadRequest(c, "上传类型只能是 full 或 patch")
		return
	}

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传文件")
		return
	}
	defer file.Close()

	// 保存文件
	cfg := config.Get()
	maxUploadBytes := int64(cfg.Security.MaxReleaseUploadMB) << 20
	if maxUploadBytes <= 0 {
		maxUploadBytes = 500 << 20
	}
	if header.Size > maxUploadBytes {
		response.BadRequest(c, fmt.Sprintf("更新包过大，最大支持 %dMB", maxUploadBytes>>20))
		return
	}
	hotUpdateDir := filepath.Join(cfg.Storage.ReleasesDir, "hotupdate")
	if err := os.MkdirAll(hotUpdateDir, 0755); err != nil {
		response.ServerError(c, "创建目录失败: "+err.Error())
		return
	}

	filename := fmt.Sprintf("%s_%s_to_%s_%s_%d%s",
		app.AppKey,
		packageVersionFilenamePart(hotUpdate.FromVersion),
		packageVersionFilenamePart(hotUpdate.ToVersion),
		uploadType,
		time.Now().UnixNano(),
		filepath.Ext(header.Filename))
	filePath := filepath.Join(hotUpdateDir, filename)

	stagedFile, err := stageUploadedFile(&io.LimitedReader{R: file, N: maxUploadBytes + 1}, filePath)
	if err != nil {
		response.ServerError(c, "保存文件失败: "+err.Error())
		return
	}
	defer stagedFile.Cleanup()
	fileSize := stagedFile.Size
	fileHash := stagedFile.Hash
	if fileSize > maxUploadBytes {
		response.BadRequest(c, fmt.Sprintf("更新包过大，最大支持 %dMB", maxUploadBytes>>20))
		return
	}
	fileSignature, err := signFileSignature(app.PrivateKey, fileHash, fileSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}

	downloadURL := fmt.Sprintf("/api/client/hotupdate/download/%s", filename)

	// 更新热更新记录
	oldURL := hotUpdate.FullURL
	if uploadType == "patch" {
		oldURL = hotUpdate.PatchURL
		hotUpdate.PatchURL = downloadURL
		hotUpdate.PatchSize = fileSize
		hotUpdate.PatchHash = fileHash
		hotUpdate.PatchSignature = fileSignature
	} else {
		hotUpdate.FullURL = downloadURL
		hotUpdate.FullSize = fileSize
		hotUpdate.FullHash = fileHash
		hotUpdate.FullSignature = fileSignature
	}

	if err := stagedFile.Commit(); err != nil {
		response.ServerError(c, "保存文件失败: "+err.Error())
		return
	}
	if err := model.DB.Save(&hotUpdate).Error; err != nil {
		_ = os.Remove(filePath)
		response.ServerError(c, "保存热更新文件信息失败: "+err.Error())
		return
	}
	removeReplacedHotUpdateFile(hotUpdateDir, oldURL, downloadURL)

	response.Success(c, gin.H{
		"id":             hotUpdate.ID,
		"type":           uploadType,
		"download_url":   downloadURL,
		"file_size":      fileSize,
		"file_hash":      fileHash,
		"file_signature": fileSignature,
		"signature_alg":  fileSignatureAlgorithm,
	})
}

// List 获取热更新列表
func (h *HotUpdateHandler) List(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var hotUpdates []model.HotUpdate
	query := model.DB.Where("app_id = ?", appID).Order("created_at DESC")
	status := c.Query("status")
	if status != "" {
		query = query.Where("status = ?", status)
	}

	// 分页
	page, pageSize := parsePageParams(c, 20, 100)
	offset := (page - 1) * pageSize

	var total int64
	query.Model(&model.HotUpdate{}).Count(&total)
	query.Offset(offset).Limit(pageSize).Find(&hotUpdates)

	var result []gin.H
	for _, hu := range hotUpdates {
		result = append(result, gin.H{
			"id":                 hu.ID,
			"from_version":       hu.FromVersion,
			"to_version":         hu.ToVersion,
			"version_code":       hu.VersionCode,
			"patch_type":         hu.PatchType,
			"patch_url":          hu.PatchURL,
			"patch_size":         hu.PatchSize,
			"patch_hash":         hu.PatchHash,
			"patch_signature":    hu.PatchSignature,
			"full_url":           hu.FullURL,
			"full_size":          hu.FullSize,
			"full_hash":          hu.FullHash,
			"full_signature":     hu.FullSignature,
			"changelog":          hu.Changelog,
			"force_update":       hu.ForceUpdate,
			"rollout_percentage": hu.RolloutPercent,
			"status":             hu.Status,
			"download_count":     hu.DownloadCount,
			"success_count":      hu.SuccessCount,
			"fail_count":         hu.FailCount,
			"published_at":       hu.PublishedAt,
			"created_at":         hu.CreatedAt,
		})
	}

	response.Success(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Get 获取热更新详情
func (h *HotUpdateHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	response.Success(c, gin.H{
		"id":                 hotUpdate.ID,
		"app_id":             hotUpdate.AppID,
		"from_version":       hotUpdate.FromVersion,
		"to_version":         hotUpdate.ToVersion,
		"version_code":       hotUpdate.VersionCode,
		"patch_type":         hotUpdate.PatchType,
		"patch_url":          hotUpdate.PatchURL,
		"patch_size":         hotUpdate.PatchSize,
		"patch_hash":         hotUpdate.PatchHash,
		"patch_signature":    hotUpdate.PatchSignature,
		"full_url":           hotUpdate.FullURL,
		"full_size":          hotUpdate.FullSize,
		"full_hash":          hotUpdate.FullHash,
		"full_signature":     hotUpdate.FullSignature,
		"changelog":          hotUpdate.Changelog,
		"force_update":       hotUpdate.ForceUpdate,
		"min_app_version":    hotUpdate.MinAppVersion,
		"rollout_percentage": hotUpdate.RolloutPercent,
		"status":             hotUpdate.Status,
		"download_count":     hotUpdate.DownloadCount,
		"success_count":      hotUpdate.SuccessCount,
		"fail_count":         hotUpdate.FailCount,
		"published_at":       hotUpdate.PublishedAt,
		"created_at":         hotUpdate.CreatedAt,
	})
}

// Update 更新热更新配置
func (h *HotUpdateHandler) Update(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	var req struct {
		Changelog         *string `json:"changelog"`
		ForceUpdate       *bool   `json:"force_update"`
		MinAppVersion     *string `json:"min_app_version"`
		RolloutPercentage *int    `json:"rollout_percentage"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if req.Changelog != nil {
		updates["changelog"] = *req.Changelog
	}
	if req.ForceUpdate != nil {
		updates["force_update"] = *req.ForceUpdate
	}
	if req.MinAppVersion != nil {
		updates["min_app_version"] = *req.MinAppVersion
	}
	if req.RolloutPercentage != nil {
		if *req.RolloutPercentage < 0 || *req.RolloutPercentage > 100 {
			response.BadRequest(c, "灰度比例必须在 0 到 100 之间")
			return
		}
		updates["rollout_percent"] = *req.RolloutPercentage
	}

	if err := model.DB.Model(&hotUpdate).Updates(updates).Error; err != nil {
		response.ServerError(c, "更新热更新配置失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

// Publish 发布热更新
func (h *HotUpdateHandler) Publish(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	// 检查是否有可下载的文件
	if hotUpdate.FullURL == "" && hotUpdate.PatchURL == "" {
		response.Error(c, 400, "请先上传更新包")
		return
	}

	now := time.Now()
	hotUpdate.Status = model.HotUpdateStatusPublished
	hotUpdate.PublishedAt = &now
	if err := model.DB.Save(&hotUpdate).Error; err != nil {
		response.ServerError(c, "发布热更新失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "发布成功", nil)
}

// Deprecate 废弃热更新
func (h *HotUpdateHandler) Deprecate(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	hotUpdate.Status = model.HotUpdateStatusDeprecated
	if err := model.DB.Save(&hotUpdate).Error; err != nil {
		response.ServerError(c, "废弃热更新失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "已废弃", nil)
}

// Rollback 回滚热更新
func (h *HotUpdateHandler) Rollback(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	hotUpdate.Status = model.HotUpdateStatusRollback
	if err := model.DB.Save(&hotUpdate).Error; err != nil {
		response.ServerError(c, "回滚热更新失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "已回滚", nil)
}

// Delete 删除热更新
func (h *HotUpdateHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	// 删除文件
	cfg := config.Get()
	hotUpdateDir := filepath.Join(cfg.Storage.ReleasesDir, "hotupdate")

	if hotUpdate.PatchURL != "" {
		filename := filepath.Base(hotUpdate.PatchURL)
		os.Remove(filepath.Join(hotUpdateDir, filename))
	}
	if hotUpdate.FullURL != "" {
		filename := filepath.Base(hotUpdate.FullURL)
		os.Remove(filepath.Join(hotUpdateDir, filename))
	}

	// 删除日志
	if err := model.DB.Where("hot_update_id = ?", id).Delete(&model.HotUpdateLog{}).Error; err != nil {
		response.ServerError(c, "删除热更新日志失败: "+err.Error())
		return
	}

	// 删除记录
	if err := model.DB.Delete(&hotUpdate).Error; err != nil {
		response.ServerError(c, "删除热更新失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "删除成功", nil)
}

// GetLogs 获取热更新日志
func (h *HotUpdateHandler) GetLogs(c *gin.Context) {
	id := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var hotUpdate model.HotUpdate
	if err := model.DB.Joins("JOIN applications ON applications.id = hot_updates.app_id").
		Where("hot_updates.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	var logs []model.HotUpdateLog
	query := model.DB.Where("hot_update_id = ?", id).Order("created_at DESC")
	status := c.Query("status")
	hasError := c.Query("has_error")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if hasError == "true" {
		query = query.Where("error_message <> ''")
	}

	page, pageSize := parsePageParams(c, 20, 100)
	offset := (page - 1) * pageSize

	var total int64
	query.Model(&model.HotUpdateLog{}).Count(&total)
	query.Offset(offset).Limit(pageSize).Find(&logs)

	var result []gin.H
	for _, log := range logs {
		result = append(result, gin.H{
			"id":            log.ID,
			"device_id":     log.DeviceID,
			"machine_id":    log.MachineID,
			"from_version":  log.FromVersion,
			"to_version":    log.ToVersion,
			"status":        log.Status,
			"error_message": log.ErrorMessage,
			"ip_address":    log.IPAddress,
			"started_at":    log.StartedAt,
			"completed_at":  log.CompletedAt,
			"created_at":    log.CreatedAt,
		})
	}

	response.Success(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetStats 获取热更新统计
func (h *HotUpdateHandler) GetStats(c *gin.Context) {
	appID := c.Param("id")
	tenantID := middleware.GetTenantID(c)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var stats struct {
		TotalUpdates   int64   `json:"total_updates"`
		PublishedCount int64   `json:"published_count"`
		TotalDownloads int64   `json:"total_downloads"`
		TotalSuccess   int64   `json:"total_success"`
		TotalFail      int64   `json:"total_fail"`
		SuccessRate    float64 `json:"success_rate"`
	}

	model.DB.Model(&model.HotUpdate{}).Where("app_id = ?", appID).Count(&stats.TotalUpdates)
	model.DB.Model(&model.HotUpdate{}).Where("app_id = ? AND status = ?", appID, model.HotUpdateStatusPublished).Count(&stats.PublishedCount)

	var sums struct {
		Downloads int64
		Success   int64
		Fail      int64
	}
	model.DB.Model(&model.HotUpdate{}).Where("app_id = ?", appID).
		Select("COALESCE(SUM(download_count), 0) as downloads, COALESCE(SUM(success_count), 0) as success, COALESCE(SUM(fail_count), 0) as fail").
		Scan(&sums)

	stats.TotalDownloads = sums.Downloads
	stats.TotalSuccess = sums.Success
	stats.TotalFail = sums.Fail

	if stats.TotalSuccess+stats.TotalFail > 0 {
		stats.SuccessRate = float64(stats.TotalSuccess) / float64(stats.TotalSuccess+stats.TotalFail) * 100
	}

	response.Success(c, stats)
}

// ==================== 客户端接口 ====================

// CheckUpdate 客户端检查热更新
func (h *HotUpdateHandler) CheckUpdate(c *gin.Context) {
	currentVersion := c.Query("version")

	if currentVersion == "" {
		response.BadRequest(c, "缺少参数")
		return
	}

	app, ok := loadClientAppFromSession(c)
	if !ok {
		return
	}
	device, ok := loadClientDeviceFromSession(c, app)
	if !ok {
		return
	}
	machineID := device.MachineID

	// 查找可用的热更新
	var hotUpdate model.HotUpdate
	err := model.DB.Where("app_id = ? AND from_version = ? AND status = ?",
		app.ID, currentVersion, model.HotUpdateStatusPublished).
		Order("created_at DESC").First(&hotUpdate).Error

	if err != nil {
		// 没有针对当前版本的热更新，检查是否有全量更新到最新版本
		var latestUpdates []model.HotUpdate
		if err := model.DB.Where("app_id = ? AND status = ?", app.ID, model.HotUpdateStatusPublished).
			Order("version_code DESC, created_at DESC").
			Find(&latestUpdates).Error; err != nil {
			response.ServerError(c, "查询更新失败")
			return
		}

		found := false
		for _, latestUpdate := range latestUpdates {
			if latestUpdate.ToVersion == currentVersion || !hotUpdateSupportsCurrentVersion(latestUpdate, currentVersion) {
				continue
			}
			hotUpdate = latestUpdate
			found = true
			break
		}
		if !found {
			response.Success(c, gin.H{
				"has_update": false,
			})
			return
		}
	}

	// 灰度检查
	if !isMachineInRollout(machineID, hotUpdate.RolloutPercent) {
		response.Success(c, gin.H{
			"has_update": false,
		})
		return
	}

	// 返回更新信息
	result := gin.H{
		"has_update":      true,
		"id":              hotUpdate.ID,
		"from_version":    hotUpdate.FromVersion,
		"to_version":      hotUpdate.ToVersion,
		"patch_type":      hotUpdate.PatchType,
		"changelog":       hotUpdate.Changelog,
		"force_update":    hotUpdate.ForceUpdate,
		"min_app_version": hotUpdate.MinAppVersion,
	}

	// 优先返回增量包
	// 当 from_version 是 "*" 时，表示匹配任意版本
	fromVersionMatch := hotUpdate.FromVersion == currentVersion || hotUpdate.FromVersion == "*"
	if hotUpdate.PatchURL != "" && fromVersionMatch {
		downloadURL, err := buildClientDownloadURLWithToken(hotUpdate.PatchURL, app.TenantID, app.ID, machineID, downloadTokenKindHotUpdate)
		if err != nil {
			response.ServerError(c, "生成下载链接失败")
			return
		}
		result["download_url"] = downloadURL
		result["file_size"] = hotUpdate.PatchSize
		result["file_hash"] = hotUpdate.PatchHash
		result["file_signature"] = hotUpdate.PatchSignature
		result["signature_alg"] = fileSignatureAlgorithm
		result["update_type"] = "patch"
	} else if hotUpdate.FullURL != "" {
		downloadURL, err := buildClientDownloadURLWithToken(hotUpdate.FullURL, app.TenantID, app.ID, machineID, downloadTokenKindHotUpdate)
		if err != nil {
			response.ServerError(c, "生成下载链接失败")
			return
		}
		result["download_url"] = downloadURL
		result["file_size"] = hotUpdate.FullSize
		result["file_hash"] = hotUpdate.FullHash
		result["file_signature"] = hotUpdate.FullSignature
		result["signature_alg"] = fileSignatureAlgorithm
		result["update_type"] = "full"
	}

	response.Success(c, result)
}

// DownloadUpdate 下载热更新包
func (h *HotUpdateHandler) DownloadUpdate(c *gin.Context) {
	filename, ok := getSafeDownloadFilename(c)
	if !ok {
		return
	}

	app, machineID, ok := validateClientDownloadContext(c, filename, downloadTokenKindHotUpdate)
	if !ok {
		return
	}

	var hotUpdate model.HotUpdate
	if err := model.DB.Where(
		"app_id = ? AND status = ? AND (patch_url LIKE ? OR full_url LIKE ?)",
		app.ID, model.HotUpdateStatusPublished, "%/"+filename, "%/"+filename,
	).Order("created_at DESC").First(&hotUpdate).Error; err != nil {
		response.NotFound(c, "文件不存在")
		return
	}
	if !isMachineInRollout(machineID, hotUpdate.RolloutPercent) {
		response.NotFound(c, "文件不存在")
		return
	}

	cfg := config.Get()
	filePath := filepath.Join(cfg.Storage.ReleasesDir, "hotupdate", filename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		response.NotFound(c, "文件不存在")
		return
	}

	if hotUpdate.PatchURL != "" && filepath.Base(hotUpdate.PatchURL) == filename {
		c.Header("X-File-Hash", hotUpdate.PatchHash)
		c.Header("X-File-Signature", hotUpdate.PatchSignature)
	} else {
		c.Header("X-File-Hash", hotUpdate.FullHash)
		c.Header("X-File-Signature", hotUpdate.FullSignature)
	}
	c.Header("X-File-Signature-Alg", fileSignatureAlgorithm)

	// 更新下载计数
	// 从文件名解析热更新ID（简化处理，实际可通过查询参数传递）
	go func() {
		// 异步更新下载计数
		if err := model.DB.Model(&model.HotUpdate{}).
			Where("id = ? AND app_id = ?", hotUpdate.ID, app.ID).
			UpdateColumn("download_count", model.DB.Raw("download_count + 1")).Error; err != nil {
			log.Printf("[warn] 更新热更新下载计数失败: hot_update_id=%s app_id=%s err=%v", hotUpdate.ID, app.ID, err)
		}
	}()

	c.File(filePath)
}

// ReportUpdateStatus 客户端上报更新状态
func (h *HotUpdateHandler) ReportUpdateStatus(c *gin.Context) {
	var req struct {
		HotUpdateID  string `json:"hot_update_id" binding:"required"`
		FromVersion  string `json:"from_version"`
		ToVersion    string `json:"to_version"`
		Status       string `json:"status" binding:"required"` // downloading, installing, success, failed, rollback
		ErrorMessage string `json:"error_message"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	status, ok := normalizeHotUpdateLogStatus(req.Status)
	if !ok {
		response.BadRequest(c, "无效的更新状态")
		return
	}

	app, ok := loadClientAppFromSession(c)
	if !ok {
		return
	}
	device, ok := loadClientDeviceFromSession(c, app)
	if !ok {
		return
	}
	machineID := device.MachineID

	// 验证热更新
	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ? AND app_id = ?", req.HotUpdateID, app.ID).Error; err != nil {
		response.Error(c, 400, "无效的热更新ID")
		return
	}

	now := time.Now()

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		var log model.HotUpdateLog
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("hot_update_id = ? AND machine_id = ?", req.HotUpdateID, machineID).
			First(&log).Error

		previousStatus := model.HotUpdateLogStatus("")
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			log = model.HotUpdateLog{
				HotUpdateID:  req.HotUpdateID,
				DeviceID:     device.ID,
				MachineID:    machineID,
				FromVersion:  req.FromVersion,
				ToVersion:    req.ToVersion,
				Status:       status,
				ErrorMessage: req.ErrorMessage,
				IPAddress:    c.ClientIP(),
				StartedAt:    &now,
			}
			if isTerminalHotUpdateLogStatus(status) {
				log.CompletedAt = &now
			}
			if err := tx.Create(&log).Error; err != nil {
				return err
			}
		} else {
			previousStatus = log.Status
			if !shouldApplyHotUpdateLogStatusTransition(previousStatus, status) {
				return nil
			}
			updates := map[string]interface{}{
				"status":     status,
				"device_id":  device.ID,
				"ip_address": c.ClientIP(),
			}
			if req.FromVersion != "" {
				updates["from_version"] = req.FromVersion
			}
			if req.ToVersion != "" {
				updates["to_version"] = req.ToVersion
			}
			if req.ErrorMessage != "" {
				updates["error_message"] = req.ErrorMessage
			} else if status == model.HotUpdateLogStatusSuccess {
				updates["error_message"] = ""
			}
			if isTerminalHotUpdateLogStatus(status) {
				updates["completed_at"] = &now
			} else {
				updates["completed_at"] = nil
			}
			if err := tx.Model(&log).Updates(updates).Error; err != nil {
				return err
			}
		}

		return applyHotUpdateStatusCounterDelta(tx, hotUpdate.ID, previousStatus, status)
	}); err != nil {
		response.ServerError(c, "上报更新状态失败")
		return
	}

	response.SuccessWithMessage(c, "上报成功", nil)
}

// GetUpdateHistory 获取设备更新历史
func (h *HotUpdateHandler) GetUpdateHistory(c *gin.Context) {
	app, ok := loadClientAppFromSession(c)
	if !ok {
		return
	}
	device, ok := loadClientDeviceFromSession(c, app)
	if !ok {
		return
	}
	machineID := device.MachineID

	var logs []model.HotUpdateLog
	model.DB.Preload("HotUpdate").
		Joins("JOIN hot_updates ON hot_updates.id = hot_update_logs.hot_update_id").
		Where("hot_updates.app_id = ? AND hot_update_logs.machine_id = ?", app.ID, machineID).
		Order("hot_update_logs.created_at DESC").
		Limit(20).
		Find(&logs)

	var result []gin.H
	for _, log := range logs {
		item := gin.H{
			"id":            log.ID,
			"from_version":  log.FromVersion,
			"to_version":    log.ToVersion,
			"status":        log.Status,
			"error_message": log.ErrorMessage,
			"started_at":    log.StartedAt,
			"completed_at":  log.CompletedAt,
		}
		if log.HotUpdate != nil {
			item["changelog"] = log.HotUpdate.Changelog
		}
		result = append(result, item)
	}

	response.Success(c, result)
}
