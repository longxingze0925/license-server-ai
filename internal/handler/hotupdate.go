package handler

import (
	"fmt"
	"hash/crc32"
	"license-server/internal/config"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type HotUpdateHandler struct{}

func NewHotUpdateHandler() *HotUpdateHandler {
	return &HotUpdateHandler{}
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

	var app model.Application
	if err := model.DB.First(&app, "id = ?", appID).Error; err != nil {
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

	// 验证必填字段
	if version == "" {
		response.BadRequest(c, "版本号不能为空")
		return
	}

	versionCode, _ := strconv.Atoi(versionCodeStr)
	rolloutPercentage, _ := strconv.Atoi(rolloutStr)
	if rolloutPercentage <= 0 {
		rolloutPercentage = 100
	}

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
		AppID:          appID,
		FromVersion:    "*", // 默认从任意版本更新
		ToVersion:      version,
		VersionCode:    versionCode,
		PatchType:      patchType,
		UpdateMode:     updateMode,
		Changelog:      changelog,
		ForceUpdate:    forceUpdate,
		RestartRequired: restartRequired,
		MinAppVersion:  minAppVersion,
		RolloutPercent: rolloutPercentage,
		Status:         model.HotUpdateStatusDraft,
	}

	// 检查是否有上传文件
	file, header, err := c.Request.FormFile("file")
	if err == nil {
		defer file.Close()

		// 保存文件
		cfg := config.Get()
		hotUpdateDir := filepath.Join(cfg.Storage.ReleasesDir, "hotupdate")
		if err := os.MkdirAll(hotUpdateDir, 0755); err != nil {
			response.ServerError(c, "创建目录失败: "+err.Error())
			return
		}

		uploadType := "full"
		if updateType == "patch" {
			uploadType = "patch"
		}

		filename := fmt.Sprintf("%s_any_to_%s_%s%s",
			app.AppKey, version, uploadType, filepath.Ext(header.Filename))
		filePath := filepath.Join(hotUpdateDir, filename)

		fileSize, fileHash, err := saveUploadedFile(file, filePath)
		if err != nil {
			response.ServerError(c, "保存文件失败: "+err.Error())
			return
		}

		downloadURL := fmt.Sprintf("/api/client/hotupdate/download/%s", filename)

		// 设置文件信息
		if uploadType == "patch" {
			hotUpdate.PatchURL = downloadURL
			hotUpdate.PatchSize = fileSize
			hotUpdate.PatchHash = fileHash
		} else {
			hotUpdate.FullURL = downloadURL
			hotUpdate.FullSize = fileSize
			hotUpdate.FullHash = fileHash
		}
	}

	if err := model.DB.Create(&hotUpdate).Error; err != nil {
		response.ServerError(c, "创建热更新失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"id":           hotUpdate.ID,
		"from_version": hotUpdate.FromVersion,
		"to_version":   hotUpdate.ToVersion,
		"status":       hotUpdate.Status,
		"created_at":   hotUpdate.CreatedAt,
	})
}

// Upload 上传热更新包
func (h *HotUpdateHandler) Upload(c *gin.Context) {
	appID := c.Param("id")
	hotUpdateID := c.Param("hotupdate_id")

	var app model.Application
	if err := model.DB.First(&app, "id = ?", appID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ? AND app_id = ?", hotUpdateID, appID).Error; err != nil {
		response.NotFound(c, "热更新记录不存在")
		return
	}

	// 获取上传类型 (patch 或 full)
	uploadType := c.PostForm("type")
	if uploadType == "" {
		uploadType = "full"
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
	hotUpdateDir := filepath.Join(cfg.Storage.ReleasesDir, "hotupdate")
	if err := os.MkdirAll(hotUpdateDir, 0755); err != nil {
		response.ServerError(c, "创建目录失败: "+err.Error())
		return
	}

	filename := fmt.Sprintf("%s_%s_to_%s_%s%s",
		app.AppKey, hotUpdate.FromVersion, hotUpdate.ToVersion, uploadType, filepath.Ext(header.Filename))
	filePath := filepath.Join(hotUpdateDir, filename)

	fileSize, fileHash, err := saveUploadedFile(file, filePath)
	if err != nil {
		response.ServerError(c, "保存文件失败: "+err.Error())
		return
	}

	downloadURL := fmt.Sprintf("/api/client/hotupdate/download/%s", filename)

	// 更新热更新记录
	if uploadType == "patch" {
		hotUpdate.PatchURL = downloadURL
		hotUpdate.PatchSize = fileSize
		hotUpdate.PatchHash = fileHash
	} else {
		hotUpdate.FullURL = downloadURL
		hotUpdate.FullSize = fileSize
		hotUpdate.FullHash = fileHash
	}

	model.DB.Save(&hotUpdate)

	response.Success(c, gin.H{
		"id":           hotUpdate.ID,
		"type":         uploadType,
		"download_url": downloadURL,
		"file_size":    fileSize,
		"file_hash":    fileHash,
	})
}

// List 获取热更新列表
func (h *HotUpdateHandler) List(c *gin.Context) {
	appID := c.Param("id")

	var hotUpdates []model.HotUpdate
	query := model.DB.Where("app_id = ?", appID).Order("created_at DESC")

	// 分页
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
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
			"patch_type":         hu.PatchType,
			"patch_url":          hu.PatchURL,
			"patch_size":         hu.PatchSize,
			"full_url":           hu.FullURL,
			"full_size":          hu.FullSize,
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

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ?", id).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	response.Success(c, gin.H{
		"id":                 hotUpdate.ID,
		"app_id":             hotUpdate.AppID,
		"from_version":       hotUpdate.FromVersion,
		"to_version":         hotUpdate.ToVersion,
		"patch_type":         hotUpdate.PatchType,
		"patch_url":          hotUpdate.PatchURL,
		"patch_size":         hotUpdate.PatchSize,
		"patch_hash":         hotUpdate.PatchHash,
		"full_url":           hotUpdate.FullURL,
		"full_size":          hotUpdate.FullSize,
		"full_hash":          hotUpdate.FullHash,
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

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ?", id).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	var req struct {
		Changelog         string `json:"changelog"`
		ForceUpdate       *bool  `json:"force_update"`
		MinAppVersion     string `json:"min_app_version"`
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
	if req.MinAppVersion != "" {
		updates["min_app_version"] = req.MinAppVersion
	}
	if req.RolloutPercentage > 0 && req.RolloutPercentage <= 100 {
		updates["rollout_percentage"] = req.RolloutPercentage
	}

	model.DB.Model(&hotUpdate).Updates(updates)

	response.SuccessWithMessage(c, "更新成功", nil)
}

// Publish 发布热更新
func (h *HotUpdateHandler) Publish(c *gin.Context) {
	id := c.Param("id")

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ?", id).Error; err != nil {
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
	model.DB.Save(&hotUpdate)

	response.SuccessWithMessage(c, "发布成功", nil)
}

// Deprecate 废弃热更新
func (h *HotUpdateHandler) Deprecate(c *gin.Context) {
	id := c.Param("id")

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ?", id).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	hotUpdate.Status = model.HotUpdateStatusDeprecated
	model.DB.Save(&hotUpdate)

	response.SuccessWithMessage(c, "已废弃", nil)
}

// Rollback 回滚热更新
func (h *HotUpdateHandler) Rollback(c *gin.Context) {
	id := c.Param("id")

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ?", id).Error; err != nil {
		response.NotFound(c, "热更新不存在")
		return
	}

	hotUpdate.Status = model.HotUpdateStatusRollback
	model.DB.Save(&hotUpdate)

	response.SuccessWithMessage(c, "已回滚", nil)
}

// Delete 删除热更新
func (h *HotUpdateHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ?", id).Error; err != nil {
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
	model.DB.Where("hot_update_id = ?", id).Delete(&model.HotUpdateLog{})

	// 删除记录
	model.DB.Delete(&hotUpdate)

	response.SuccessWithMessage(c, "删除成功", nil)
}

// GetLogs 获取热更新日志
func (h *HotUpdateHandler) GetLogs(c *gin.Context) {
	id := c.Param("id")

	var logs []model.HotUpdateLog
	query := model.DB.Where("hot_update_id = ?", id).Order("created_at DESC")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
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

	var stats struct {
		TotalUpdates   int64 `json:"total_updates"`
		PublishedCount int64 `json:"published_count"`
		TotalDownloads int64 `json:"total_downloads"`
		TotalSuccess   int64 `json:"total_success"`
		TotalFail      int64 `json:"total_fail"`
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
	appKey := c.Query("app_key")
	currentVersion := c.Query("version")
	machineID := c.Query("machine_id")

	if appKey == "" || currentVersion == "" {
		response.BadRequest(c, "缺少参数")
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", appKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	// 查找可用的热更新
	var hotUpdate model.HotUpdate
	err := model.DB.Where("app_id = ? AND from_version = ? AND status = ?",
		app.ID, currentVersion, model.HotUpdateStatusPublished).
		Order("created_at DESC").First(&hotUpdate).Error

	if err != nil {
		// 没有针对当前版本的热更新，检查是否有全量更新到最新版本
		var latestUpdate model.HotUpdate
		err = model.DB.Where("app_id = ? AND status = ? AND (min_app_version = '' OR min_app_version <= ?)",
			app.ID, model.HotUpdateStatusPublished, currentVersion).
			Order("created_at DESC").First(&latestUpdate).Error

		if err != nil {
			response.Success(c, gin.H{
				"has_update": false,
			})
			return
		}

		// 检查是否已经是最新版本
		if latestUpdate.ToVersion == currentVersion {
			response.Success(c, gin.H{
				"has_update": false,
			})
			return
		}

		hotUpdate = latestUpdate
	}

	// 灰度检查
	if hotUpdate.RolloutPercent < 100 && machineID != "" {
		// 使用机器码的哈希值来决定是否在灰度范围内
		hash := crc32.ChecksumIEEE([]byte(machineID))
		if int(hash%100) >= hotUpdate.RolloutPercent {
			response.Success(c, gin.H{
				"has_update": false,
			})
			return
		}
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
		result["download_url"] = hotUpdate.PatchURL
		result["file_size"] = hotUpdate.PatchSize
		result["file_hash"] = hotUpdate.PatchHash
		result["update_type"] = "patch"
	} else if hotUpdate.FullURL != "" {
		result["download_url"] = hotUpdate.FullURL
		result["file_size"] = hotUpdate.FullSize
		result["file_hash"] = hotUpdate.FullHash
		result["update_type"] = "full"
	}

	response.Success(c, result)
}

// DownloadUpdate 下载热更新包
func (h *HotUpdateHandler) DownloadUpdate(c *gin.Context) {
	filename := c.Param("filename")

	cfg := config.Get()
	filePath := filepath.Join(cfg.Storage.ReleasesDir, "hotupdate", filename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		response.NotFound(c, "文件不存在")
		return
	}

	// 更新下载计数
	// 从文件名解析热更新ID（简化处理，实际可通过查询参数传递）
	go func() {
		// 异步更新下载计数
		model.DB.Model(&model.HotUpdate{}).
			Where("patch_url LIKE ? OR full_url LIKE ?", "%"+filename, "%"+filename).
			UpdateColumn("download_count", model.DB.Raw("download_count + 1"))
	}()

	c.File(filePath)
}

// ReportUpdateStatus 客户端上报更新状态
func (h *HotUpdateHandler) ReportUpdateStatus(c *gin.Context) {
	var req struct {
		AppKey       string `json:"app_key" binding:"required"`
		HotUpdateID  string `json:"hot_update_id" binding:"required"`
		MachineID    string `json:"machine_id" binding:"required"`
		FromVersion  string `json:"from_version"`
		ToVersion    string `json:"to_version"`
		Status       string `json:"status" binding:"required"` // downloading, installing, success, failed, rollback
		ErrorMessage string `json:"error_message"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ?", req.AppKey).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

	// 验证热更新
	var hotUpdate model.HotUpdate
	if err := model.DB.First(&hotUpdate, "id = ?", req.HotUpdateID).Error; err != nil {
		response.Error(c, 400, "无效的热更新ID")
		return
	}

	// 查找设备
	var deviceID string
	var device model.Device
	if err := model.DB.Where("machine_id = ?", req.MachineID).First(&device).Error; err == nil {
		deviceID = device.ID
	}

	now := time.Now()

	// 查找或创建日志
	var log model.HotUpdateLog
	err := model.DB.Where("hot_update_id = ? AND machine_id = ?", req.HotUpdateID, req.MachineID).First(&log).Error

	status := model.HotUpdateLogStatus(req.Status)

	if err != nil {
		// 创建新日志
		log = model.HotUpdateLog{
			HotUpdateID:  req.HotUpdateID,
			DeviceID:     deviceID,
			MachineID:    req.MachineID,
			FromVersion:  req.FromVersion,
			ToVersion:    req.ToVersion,
			Status:       status,
			ErrorMessage: req.ErrorMessage,
			IPAddress:    c.ClientIP(),
			StartedAt:    &now,
		}
		model.DB.Create(&log)
	} else {
		// 更新日志
		log.Status = status
		if req.ErrorMessage != "" {
			log.ErrorMessage = req.ErrorMessage
		}
		if status == model.HotUpdateLogStatusSuccess || status == model.HotUpdateLogStatusFailed || status == model.HotUpdateLogStatusRollback {
			log.CompletedAt = &now
		}
		model.DB.Save(&log)
	}

	// 更新热更新统计
	switch status {
	case model.HotUpdateLogStatusSuccess:
		model.DB.Model(&hotUpdate).UpdateColumn("success_count", model.DB.Raw("success_count + 1"))
	case model.HotUpdateLogStatusFailed:
		model.DB.Model(&hotUpdate).UpdateColumn("fail_count", model.DB.Raw("fail_count + 1"))
	}

	response.SuccessWithMessage(c, "上报成功", nil)
}

// GetUpdateHistory 获取设备更新历史
func (h *HotUpdateHandler) GetUpdateHistory(c *gin.Context) {
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")

	if appKey == "" || machineID == "" {
		response.BadRequest(c, "缺少参数")
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ?", appKey).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return
	}

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
