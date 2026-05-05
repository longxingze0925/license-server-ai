package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ClientSyncHandler struct{}

func NewClientSyncHandler() *ClientSyncHandler {
	return &ClientSyncHandler{}
}

// PushRequest 推送数据请求
type PushRequest struct {
	DeviceName string `json:"device_name"`
	DataType   string `json:"data_type" binding:"required"` // scripts/danmaku_groups/ai_config
	DataJSON   string `json:"data_json" binding:"required"`
	ItemCount  int    `json:"item_count"`
}

// PullRequest 拉取数据请求
type PullRequest struct {
	DataType string `json:"data_type"` // 为空则拉取所有类型
}

// SyncDataResponse 同步数据响应
type SyncDataResponse struct {
	DataType  string `json:"data_type"`
	DataJSON  string `json:"data_json"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// Push 客户端推送数据到服务器
func (h *ClientSyncHandler) Push(c *gin.Context) {
	var req PushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证数据类型
	if !isValidDataType(req.DataType) {
		response.BadRequest(c, "无效的数据类型")
		return
	}

	// 验证应用和获取用户信息
	app, customer, customerID, machineID, err := h.validateAndGetUser(c)
	if err != nil {
		return // 错误已在函数内处理
	}

	// 加密敏感配置中的密钥类字段，避免备份表落明文。
	dataJSON := req.DataJSON
	if req.DataType == model.DataTypeAIConfig || req.DataType == model.DataTypeRandomWordAIConfig {
		dataJSON = encryptSensitiveData(dataJSON, app.AppSecret)
	}

	// 计算校验和
	checksum := calculateChecksum(dataJSON)

	var syncData model.ClientSyncData
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		var versions []model.ClientSyncData
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("client_user_id = ? AND app_id = ? AND data_type = ?",
				customer.ID, app.ID, req.DataType).
			Order("version DESC").
			Find(&versions).Error; err != nil {
			return err
		}

		maxVersion := 0
		if len(versions) > 0 {
			maxVersion = versions[0].Version
		}

		if err := tx.Model(&model.ClientSyncData{}).
			Where("client_user_id = ? AND app_id = ? AND data_type = ? AND is_current = ?",
				customer.ID, app.ID, req.DataType, true).
			Update("is_current", false).Error; err != nil {
			return err
		}

		syncData = model.ClientSyncData{
			TenantID:     app.TenantID,
			AppID:        app.ID,
			CustomerID:   customerID,
			ClientUserID: customer.ID,
			DataType:     req.DataType,
			DataJSON:     dataJSON,
			Version:      maxVersion + 1,
			DeviceName:   req.DeviceName,
			MachineID:    machineID,
			IsCurrent:    true,
			DataSize:     int64(len(dataJSON)),
			ItemCount:    req.ItemCount,
			Checksum:     checksum,
		}

		return tx.Create(&syncData).Error
	}); err != nil {
		response.Error(c, 500, "保存数据失败")
		return
	}

	// 清理旧版本（保留最新的 MaxSyncVersions 个）
	h.cleanupOldVersions(customer.ID, app.ID, req.DataType)

	response.Success(c, gin.H{
		"version":    syncData.Version,
		"updated_at": syncData.UpdatedAt,
	})
}

// Pull 客户端从服务器拉取数据
func (h *ClientSyncHandler) Pull(c *gin.Context) {
	dataType := c.Query("data_type")

	// 验证应用和获取用户信息
	app, customer, _, _, err := h.validateAndGetUser(c)
	if err != nil {
		return
	}

	var results []SyncDataResponse

	if dataType != "" {
		// 拉取指定类型
		if !isValidDataType(dataType) {
			response.BadRequest(c, "无效的数据类型")
			return
		}

		var syncData model.ClientSyncData
		if err := model.DB.Where("client_user_id = ? AND app_id = ? AND data_type = ? AND is_current = ?",
			customer.ID, app.ID, dataType, true).First(&syncData).Error; err == nil {

			dataJSON := syncData.DataJSON
			// 解密敏感数据
			if dataType == model.DataTypeAIConfig || dataType == model.DataTypeRandomWordAIConfig {
				dataJSON = decryptSensitiveData(dataJSON, app.AppSecret)
			}

			results = append(results, SyncDataResponse{
				DataType:  syncData.DataType,
				DataJSON:  dataJSON,
				Version:   syncData.Version,
				UpdatedAt: syncData.UpdatedAt.Format("2006-01-02 15:04:05"),
			})
		}
	} else {
		// 拉取所有类型
		var syncDataList []model.ClientSyncData
		model.DB.Where("client_user_id = ? AND app_id = ? AND is_current = ?",
			customer.ID, app.ID, true).Find(&syncDataList)

		for _, syncData := range syncDataList {
			dataJSON := syncData.DataJSON
			// 解密敏感数据
			if syncData.DataType == model.DataTypeAIConfig || syncData.DataType == model.DataTypeRandomWordAIConfig {
				dataJSON = decryptSensitiveData(dataJSON, app.AppSecret)
			}

			results = append(results, SyncDataResponse{
				DataType:  syncData.DataType,
				DataJSON:  dataJSON,
				Version:   syncData.Version,
				UpdatedAt: syncData.UpdatedAt.Format("2006-01-02 15:04:05"),
			})
		}
	}

	response.Success(c, gin.H{
		"data": results,
	})
}

// validateAndGetUser 验证应用并获取用户信息
// 返回：应用、客户、客户ID、错误
func (h *ClientSyncHandler) validateAndGetUser(c *gin.Context) (*model.Application, *model.Customer, string, string, error) {
	tenantID := middleware.GetClientTenantID(c)
	appID := middleware.GetClientAppID(c)
	deviceID := middleware.GetClientDeviceID(c)
	machineID := middleware.GetClientMachineID(c)
	authMode := middleware.GetClientAuthMode(c)
	customerID := middleware.GetClientCustomerID(c)
	if tenantID == "" || appID == "" || deviceID == "" || machineID == "" {
		response.Unauthorized(c, "客户端会话无效")
		return nil, nil, "", "", errors.New("invalid client session")
	}

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ? AND status = ?", appID, tenantID, model.AppStatusActive).Error; err != nil {
		response.Error(c, 401, "应用已失效，请重新登录")
		return nil, nil, "", "", err
	}

	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		First(&device, "id = ? AND tenant_id = ? AND machine_id = ?", deviceID, tenantID, machineID).Error; err != nil {
		response.Error(c, 401, "设备已解绑，请重新登录")
		return nil, nil, "", "", err
	}
	if device.Status == model.DeviceStatusBlacklisted {
		response.Error(c, 401, "设备已被禁止使用")
		return nil, nil, "", "", errors.New("device blacklisted")
	}

	switch authMode {
	case "subscription":
		if device.SubscriptionID == nil || *device.SubscriptionID == "" {
			response.Error(c, 401, "订阅已失效，请重新登录")
			return nil, nil, "", "", errors.New("missing subscription")
		}
		var subscription model.Subscription
		if device.Subscription != nil {
			subscription = *device.Subscription
		} else if err := model.DB.First(&subscription, "id = ? AND tenant_id = ?", *device.SubscriptionID, tenantID).Error; err != nil {
			response.Error(c, 401, "订阅已失效，请重新登录")
			return nil, nil, "", "", err
		}
		if subscription.AppID != app.ID || !subscription.IsValid() {
			response.Error(c, 401, "订阅无效，请重新登录")
			return nil, nil, "", "", errors.New("invalid subscription")
		}
		if customerID != "" && customerID != subscription.CustomerID {
			response.Error(c, 401, "订阅归属已变更，请重新登录")
			return nil, nil, "", "", errors.New("subscription owner mismatch")
		}
		customerID = subscription.CustomerID
	case "license":
		if device.LicenseID == nil || *device.LicenseID == "" {
			response.Error(c, 401, "授权已失效，请重新激活")
			return nil, nil, "", "", errors.New("missing license")
		}
		var license model.License
		if device.License != nil {
			license = *device.License
		} else if err := model.DB.First(&license, "id = ? AND tenant_id = ?", *device.LicenseID, tenantID).Error; err != nil {
			response.Error(c, 401, "授权已失效，请重新激活")
			return nil, nil, "", "", err
		}
		if license.AppID != app.ID || !license.IsValid() {
			response.Error(c, 401, "授权无效，请重新激活")
			return nil, nil, "", "", errors.New("invalid license")
		}
		if customerID == "" && license.CustomerID != nil {
			customerID = *license.CustomerID
		}
		if customerID == "" {
			customerID = device.CustomerID
		}
	default:
		response.Error(c, 401, "会话模式无效，请重新登录")
		return nil, nil, "", "", errors.New("invalid auth mode")
	}

	if customerID == "" {
		response.Error(c, 401, "无法确定用户")
		return nil, nil, "", "", errors.New("customer not found")
	}

	var customer model.Customer
	if err := model.DB.First(&customer, "id = ? AND tenant_id = ? AND status = ?", customerID, tenantID, model.CustomerStatusActive).Error; err != nil {
		response.Error(c, 401, "客户不存在或已禁用")
		return nil, nil, "", "", err
	}

	return &app, &customer, customerID, machineID, nil
}

// cleanupOldVersions 清理旧版本
func (h *ClientSyncHandler) cleanupOldVersions(clientUserID, appID, dataType string) {
	// 获取所有版本，按版本号降序
	var versions []model.ClientSyncData
	if err := model.DB.Where("client_user_id = ? AND app_id = ? AND data_type = ?",
		clientUserID, appID, dataType).
		Order("version DESC").
		Find(&versions).Error; err != nil {
		log.Printf("查询旧同步版本失败: client_user_id=%s app_id=%s data_type=%s err=%v", clientUserID, appID, dataType, err)
		return
	}

	// 删除超出限制的旧版本
	if len(versions) > model.MaxSyncVersions {
		for i := model.MaxSyncVersions; i < len(versions); i++ {
			if err := model.DB.Delete(&versions[i]).Error; err != nil {
				log.Printf("删除旧同步版本失败: id=%s err=%v", versions[i].ID, err)
			}
		}
	}
}

// isValidDataType 验证数据类型
func isValidDataType(dataType string) bool {
	return dataType == model.DataTypeScripts ||
		dataType == model.DataTypeDanmakuGroups ||
		dataType == model.DataTypeAIConfig ||
		dataType == model.DataTypeRandomWordAIConfig
}

// calculateChecksum 计算数据校验和
func calculateChecksum(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// encryptSensitiveData 加密敏感数据（AES-256-GCM）
func encryptSensitiveData(dataJSON, secret string) string {
	var data any
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return dataJSON
	}

	data = transformSensitiveStrings(data, func(value string) string {
		if strings.HasPrefix(value, "ENC:") {
			return value
		}
		encryptedValue, err := aesEncrypt(value, secret)
		if err != nil {
			return value
		}
		return "ENC:" + encryptedValue
	})

	result, _ := json.Marshal(data)
	return string(result)
}

// decryptSensitiveData 解密敏感数据
func decryptSensitiveData(dataJSON, secret string) string {
	var data any
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return dataJSON
	}

	data = transformSensitiveStrings(data, func(value string) string {
		if !strings.HasPrefix(value, "ENC:") {
			return value
		}
		decryptedValue, err := aesDecrypt(value[4:], secret)
		if err != nil {
			return value
		}
		return decryptedValue
	})

	result, _ := json.Marshal(data)
	return string(result)
}

func transformSensitiveStrings(value any, transform func(string) string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isSensitiveBackupKey(key) {
				if s, ok := child.(string); ok && strings.TrimSpace(s) != "" {
					typed[key] = transform(s)
					continue
				}
			}
			typed[key] = transformSensitiveStrings(child, transform)
		}
	case []any:
		for i, child := range typed {
			typed[i] = transformSensitiveStrings(child, transform)
		}
	}
	return value
}

func isSensitiveBackupKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return key == "api_key" ||
		key == "apikey" ||
		key == "access_token" ||
		key == "accesstoken" ||
		key == "refresh_token" ||
		key == "refreshtoken" ||
		key == "token" ||
		key == "password" ||
		key == "secret" ||
		key == "app_secret" ||
		key == "private_key" ||
		strings.HasSuffix(key, "_secret") ||
		strings.HasSuffix(key, "_token") ||
		strings.HasSuffix(key, "_key")
}

// aesEncrypt AES-256-GCM 加密
func aesEncrypt(plaintext, secret string) (string, error) {
	// 使用 secret 的 SHA256 作为密钥
	key := sha256.Sum256([]byte(secret))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// aesDecrypt AES-256-GCM 解密
func aesDecrypt(ciphertext, secret string) (string, error) {
	key := sha256.Sum256([]byte(secret))

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// ==================== 管理后台 API ====================

// AdminListUsers 获取有备份数据的用户列表
func (h *ClientSyncHandler) AdminListUsers(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	appID := c.Query("app_id")

	query := model.DB.Model(&model.ClientSyncData{}).
		Select("DISTINCT client_user_id").
		Where("tenant_id = ? AND is_current = ?", tenantID, true)

	if appID != "" {
		query = query.Where("app_id = ?", appID)
	}

	var customerIDs []string
	query.Pluck("client_user_id", &customerIDs)

	// 获取客户详细信息（client_user_id 实际存的是 customer_id）
	var customers []model.Customer
	if len(customerIDs) > 0 {
		model.DB.Where("tenant_id = ? AND id IN ?", tenantID, customerIDs).Find(&customers)
	}

	// 获取每个客户的备份统计
	var results []gin.H
	for _, customer := range customers {
		var stats []struct {
			DataType     string
			VersionCount int64
			LatestAt     string
		}

		statsQuery := model.DB.Model(&model.ClientSyncData{}).
			Select("data_type, COUNT(*) as version_count, MAX(updated_at) as latest_at").
			Where("client_user_id = ? AND tenant_id = ?", customer.ID, tenantID)
		if appID != "" {
			statsQuery = statsQuery.Where("app_id = ?", appID)
		}
		statsQuery.Group("data_type").Scan(&stats)

		results = append(results, gin.H{
			"user_id": customer.ID,
			"email":   customer.Email,
			"name":    customer.Name,
			"stats":   stats,
		})
	}

	response.Success(c, gin.H{
		"users": results,
		"total": len(results),
	})
}

// AdminGetUserBackups 获取用户的备份版本列表
func (h *ClientSyncHandler) AdminGetUserBackups(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	userID := c.Param("user_id")
	appID := c.Query("app_id")
	dataType := c.Query("data_type")

	query := model.DB.Where("client_user_id = ? AND tenant_id = ?", userID, tenantID)

	if appID != "" {
		query = query.Where("app_id = ?", appID)
	}
	if dataType != "" {
		query = query.Where("data_type = ?", dataType)
	}

	var backups []model.ClientSyncData
	query.Order("data_type, version DESC").Find(&backups)

	// 按数据类型分组
	grouped := make(map[string][]gin.H)
	for _, backup := range backups {
		item := gin.H{
			"id":          backup.ID,
			"version":     backup.Version,
			"device_name": backup.DeviceName,
			"machine_id":  backup.MachineID,
			"is_current":  backup.IsCurrent,
			"data_size":   backup.DataSize,
			"item_count":  backup.ItemCount,
			"created_at":  backup.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		grouped[backup.DataType] = append(grouped[backup.DataType], item)
	}

	response.Success(c, gin.H{
		"user_id": userID,
		"backups": grouped,
	})
}

// AdminGetBackupDetail 获取备份详情
func (h *ClientSyncHandler) AdminGetBackupDetail(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	backupID := c.Param("backup_id")

	var backup model.ClientSyncData
	if err := model.DB.Where("id = ? AND tenant_id = ?", backupID, tenantID).First(&backup).Error; err != nil {
		response.NotFound(c, "备份不存在")
		return
	}

	// 获取应用信息用于解密
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", backup.AppID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	dataJSON := backup.DataJSON
	// 管理端默认只返回脱敏后的敏感配置，避免 API 响应泄露明文密钥。
	if backup.DataType == model.DataTypeAIConfig || backup.DataType == model.DataTypeRandomWordAIConfig {
		dataJSON = decryptSensitiveData(dataJSON, app.AppSecret)
		if c.Query("reveal_secret") == "true" {
			if current, ok := c.Get("team_member"); ok {
				member, ok := current.(model.TeamMember)
				if !ok || !member.HasPermission("backup:update") {
					response.Forbidden(c, "没有查看明文密钥权限")
					return
				}
			} else {
				response.Forbidden(c, "没有查看明文密钥权限")
				return
			}
		} else {
			dataJSON = maskAPIKey(dataJSON)
		}
	}

	response.Success(c, gin.H{
		"id":          backup.ID,
		"data_type":   backup.DataType,
		"version":     backup.Version,
		"data_json":   dataJSON,
		"device_name": backup.DeviceName,
		"machine_id":  backup.MachineID,
		"is_current":  backup.IsCurrent,
		"data_size":   backup.DataSize,
		"item_count":  backup.ItemCount,
		"checksum":    backup.Checksum,
		"created_at":  backup.CreatedAt.Format("2006-01-02 15:04:05"),
		"updated_at":  backup.UpdatedAt.Format("2006-01-02 15:04:05"),
	})
}

// AdminSetCurrentVersion 设置当前版本
func (h *ClientSyncHandler) AdminSetCurrentVersion(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	backupID := c.Param("backup_id")

	var backup model.ClientSyncData
	if err := model.DB.Where("id = ? AND tenant_id = ?", backupID, tenantID).First(&backup).Error; err != nil {
		response.NotFound(c, "备份不存在")
		return
	}

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ClientSyncData{}).
			Where("client_user_id = ? AND app_id = ? AND data_type = ? AND tenant_id = ?",
				backup.ClientUserID, backup.AppID, backup.DataType, tenantID).
			Update("is_current", false).Error; err != nil {
			return err
		}

		return tx.Model(&backup).Update("is_current", true).Error
	}); err != nil {
		response.ServerError(c, "设置当前版本失败")
		return
	}

	response.Success(c, gin.H{
		"message": "已设置为当前版本",
	})
}

// maskAPIKey 隐藏密钥类字段的部分内容
func maskAPIKey(dataJSON string) string {
	var data any
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return dataJSON
	}

	data = transformSensitiveStrings(data, maskBackupSecretPreview)

	result, _ := json.Marshal(data)
	return string(result)
}

func maskBackupSecretPreview(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if len(value) <= 8 {
		return "********"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
