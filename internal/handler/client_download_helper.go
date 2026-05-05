package handler

import (
	"license-server/internal/middleware"
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

func validateClientDeviceForApp(c *gin.Context, app *model.Application, machineID string) bool {
	if machineID == "" {
		return true
	}

	if _, err := loadActiveDeviceForApp(machineID, app); err != nil {
		response.Error(c, 401, err.Error())
		return false
	}

	return true
}

func loadClientAppFromSession(c *gin.Context) (*model.Application, bool) {
	appID := middleware.GetClientAppID(c)
	tenantID := middleware.GetClientTenantID(c)
	if appID == "" || tenantID == "" {
		response.Unauthorized(c, "客户端会话无效")
		return nil, false
	}

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ? AND status = ?", appID, tenantID, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return nil, false
	}

	return &app, true
}

func loadClientDeviceFromSession(c *gin.Context, app *model.Application) (*model.Device, bool) {
	machineID := middleware.GetClientMachineID(c)
	deviceID := middleware.GetClientDeviceID(c)
	device, err := loadActiveDeviceForApp(machineID, app)
	if err != nil {
		response.Error(c, 401, err.Error())
		return nil, false
	}
	if deviceID != "" && device.ID != deviceID {
		response.Unauthorized(c, "客户端会话与设备不匹配")
		return nil, false
	}
	return device, true
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
		query := model.DB.Where("id = ? AND status = ?", claims.AppID, model.AppStatusActive)
		if claims.TenantID != "" {
			query = query.Where("tenant_id = ?", claims.TenantID)
		}
		if err := query.First(&app).Error; err != nil {
			response.Error(c, 400, "无效的应用")
			return nil, "", false
		}
		if !validateClientDeviceForApp(c, &app, claims.MachineID) {
			return nil, "", false
		}

		return &app, claims.MachineID, true
	}

	session, err := middleware.AuthenticateClientRequest(c)
	if err != nil {
		response.Unauthorized(c, "缺少下载令牌或客户端认证")
		return nil, "", false
	}
	middleware.SetClientSessionContext(c, session)

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ? AND status = ?", session.AppID, session.TenantID, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return nil, "", false
	}
	if !validateClientDeviceForApp(c, &app, session.MachineID) {
		return nil, "", false
	}

	return &app, session.MachineID, true
}
