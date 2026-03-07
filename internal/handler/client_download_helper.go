package handler

import (
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func getSafeDownloadFilename(c *gin.Context) (string, bool) {
	filename := strings.TrimSpace(c.Param("filename"))
	if filename == "" || filename != filepath.Base(filename) || strings.Contains(filename, "..") {
		response.BadRequest(c, "无效的文件名")
		return "", false
	}
	return filename, true
}

func validateClientAppByQuery(c *gin.Context) (*model.Application, string, bool) {
	appKey := strings.TrimSpace(c.Query("app_key"))
	if appKey == "" {
		response.BadRequest(c, "缺少 app_key")
		return nil, "", false
	}

	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", appKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return nil, "", false
	}

	return &app, strings.TrimSpace(c.Query("machine_id")), true
}

func validateClientDeviceForApp(c *gin.Context, app *model.Application, machineID string) bool {
	if machineID == "" {
		return true
	}

	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ?", machineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 401, "设备未授权")
		return false
	}

	if (device.License == nil || device.License.AppID != app.ID) &&
		(device.Subscription == nil || device.Subscription.AppID != app.ID) {
		response.Error(c, 401, "设备未绑定当前应用")
		return false
	}

	return true
}

func validateClientDownloadContext(c *gin.Context, filename, expectedKind string) (*model.Application, string, bool) {
	token := strings.TrimSpace(c.Query("token"))
	if token != "" {
		claims, err := parseDownloadToken(token)
		if err != nil {
			response.Unauthorized(c, "下载令牌无效或已过期")
			return nil, "", false
		}
		if claims.Kind != expectedKind || claims.Filename != filename {
			response.Forbidden(c, "下载令牌与文件不匹配")
			return nil, "", false
		}

		var app model.Application
		if err := model.DB.First(&app, "id = ? AND status = ?", claims.AppID, model.AppStatusActive).Error; err != nil {
			response.Error(c, 400, "无效的应用")
			return nil, "", false
		}
		if !validateClientDeviceForApp(c, &app, claims.MachineID) {
			return nil, "", false
		}

		return &app, claims.MachineID, true
	}

	app, machineID, ok := validateClientAppByQuery(c)
	if !ok {
		return nil, "", false
	}
	if machineID == "" {
		response.BadRequest(c, "缺少 machine_id")
		return nil, "", false
	}
	if !validateClientDeviceForApp(c, app, machineID) {
		return nil, "", false
	}

	return app, machineID, true
}
