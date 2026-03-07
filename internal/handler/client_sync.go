package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"license-server/internal/model"
	"license-server/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

type ClientSyncHandler struct{}

func NewClientSyncHandler() *ClientSyncHandler {
	return &ClientSyncHandler{}
}

// PushRequest 推送数据请求
type PushRequest struct {
	AppKey     string `json:"app_key" binding:"required"`
	MachineID  string `json:"machine_id" binding:"required"`
	DeviceName string `json:"device_name"`
	DataType   string `json:"data_type" binding:"required"` // scripts/danmaku_groups/ai_config
	DataJSON   string `json:"data_json" binding:"required"`
	ItemCount  int    `json:"item_count"`
}

// PullRequest 拉取数据请求
type PullRequest struct {
	AppKey    string `json:"app_key" binding:"required"`
	MachineID string `json:"machine_id" binding:"required"`
	DataType  string `json:"data_type"` // 为空则拉取所有类型
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
	app, customer, customerID, err := h.validateAndGetUser(c, req.AppKey, req.MachineID)
	if err != nil {
		return // 错误已在函数内处理
	}

	// 加密敏感数据（ai_config 和 random_word_ai_config 中的 api_key）
	dataJSON := req.DataJSON
	if req.DataType == model.DataTypeAIConfig || req.DataType == model.DataTypeRandomWordAIConfig {
		dataJSON = encryptSensitiveData(dataJSON, app.AppSecret)
	}

	// 计算校验和
	checksum := calculateChecksum(dataJSON)

	// 将旧版本设为非当前
	model.DB.Model(&model.ClientSyncData{}).
		Where("client_user_id = ? AND app_id = ? AND data_type = ? AND is_current = ?",
			customer.ID, app.ID, req.DataType, true).
		Update("is_current", false)

	// 获取当前最大版本号
	var maxVersion int
	model.DB.Model(&model.ClientSyncData{}).
		Where("client_user_id = ? AND app_id = ? AND data_type = ?",
			customer.ID, app.ID, req.DataType).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion)

	// 创建新版本
	syncData := model.ClientSyncData{
		TenantID:     app.TenantID,
		AppID:        app.ID,
		CustomerID:   customerID,
		ClientUserID: customer.ID,
		DataType:     req.DataType,
		DataJSON:     dataJSON,
		Version:      maxVersion + 1,
		DeviceName:   req.DeviceName,
		MachineID:    req.MachineID,
		IsCurrent:    true,
		DataSize:     int64(len(dataJSON)),
		ItemCount:    req.ItemCount,
		Checksum:     checksum,
	}

	if err := model.DB.Create(&syncData).Error; err != nil {
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
	appKey := c.Query("app_key")
	machineID := c.Query("machine_id")
	dataType := c.Query("data_type")

	if appKey == "" || machineID == "" {
		response.BadRequest(c, "缺少必要参数")
		return
	}

	// 验证应用和获取用户信息
	app, customer, _, err := h.validateAndGetUser(c, appKey, machineID)
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
func (h *ClientSyncHandler) validateAndGetUser(c *gin.Context, appKey, machineID string) (*model.Application, *model.Customer, string, error) {
	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", appKey, model.AppStatusActive).Error; err != nil {
		response.Error(c, 400, "无效的应用")
		return nil, nil, "", err
	}

	// 通过设备找到订阅
	var device model.Device
	if err := model.DB.Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ? AND subscription_id IS NOT NULL", machineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		response.Error(c, 400, "设备未注册")
		return nil, nil, "", err
	}

	// 通过订阅找到客户
	if device.SubscriptionID == nil {
		response.Error(c, 400, "设备未关联订阅")
		return nil, nil, "", fmt.Errorf("no subscription")
	}

	var subscription model.Subscription
	if device.Subscription != nil {
		subscription = *device.Subscription
	} else if err := model.DB.First(&subscription, "id = ? AND tenant_id = ?", *device.SubscriptionID, app.TenantID).Error; err != nil {
		response.Error(c, 400, "订阅不存在")
		return nil, nil, "", err
	}
	if subscription.AppID != app.ID || subscription.TenantID != app.TenantID {
		response.Error(c, 400, "设备未绑定当前应用")
		return nil, nil, "", fmt.Errorf("device app mismatch")
	}

	// 通过订阅找到客户
	var customer model.Customer
	if err := model.DB.First(&customer, "id = ? AND tenant_id = ?", subscription.CustomerID, app.TenantID).Error; err != nil {
		response.Error(c, 400, "客户不存在")
		return nil, nil, "", err
	}

	return &app, &customer, subscription.CustomerID, nil
}

// cleanupOldVersions 清理旧版本
func (h *ClientSyncHandler) cleanupOldVersions(clientUserID, appID, dataType string) {
	// 获取所有版本，按版本号降序
	var versions []model.ClientSyncData
	model.DB.Where("client_user_id = ? AND app_id = ? AND data_type = ?",
		clientUserID, appID, dataType).
		Order("version DESC").
		Find(&versions)

	// 删除超出限制的旧版本
	if len(versions) > model.MaxSyncVersions {
		for i := model.MaxSyncVersions; i < len(versions); i++ {
			model.DB.Delete(&versions[i])
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
	// 解析 JSON
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return dataJSON
	}

	// 加密 api_key
	if apiKey, ok := data["api_key"].(string); ok && apiKey != "" {
		encryptedKey, err := aesEncrypt(apiKey, secret)
		if err == nil {
			data["api_key"] = "ENC:" + encryptedKey
		}
	}

	result, _ := json.Marshal(data)
	return string(result)
}

// decryptSensitiveData 解密敏感数据
func decryptSensitiveData(dataJSON, secret string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return dataJSON
	}

	// 解密 api_key
	if apiKey, ok := data["api_key"].(string); ok && len(apiKey) > 4 && apiKey[:4] == "ENC:" {
		decryptedKey, err := aesDecrypt(apiKey[4:], secret)
		if err == nil {
			data["api_key"] = decryptedKey
		}
	}

	result, _ := json.Marshal(data)
	return string(result)
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
		return "", err
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
		model.DB.Where("id IN ?", customerIDs).Find(&customers)
	}

	// 获取每个客户的备份统计
	var results []gin.H
	for _, customer := range customers {
		var stats []struct {
			DataType     string
			VersionCount int64
			LatestAt     string
		}

		model.DB.Model(&model.ClientSyncData{}).
			Select("data_type, COUNT(*) as version_count, MAX(updated_at) as latest_at").
			Where("client_user_id = ? AND tenant_id = ?", customer.ID, tenantID).
			Group("data_type").
			Scan(&stats)

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
	dataType := c.Query("data_type")

	query := model.DB.Where("client_user_id = ? AND tenant_id = ?", userID, tenantID)

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
	model.DB.First(&app, "id = ?", backup.AppID)

	dataJSON := backup.DataJSON
	// 解密敏感数据用于展示（前端负责隐藏/显示）
	if backup.DataType == model.DataTypeAIConfig || backup.DataType == model.DataTypeRandomWordAIConfig {
		dataJSON = decryptSensitiveData(dataJSON, app.AppSecret)
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

	// 将同类型的其他版本设为非当前
	model.DB.Model(&model.ClientSyncData{}).
		Where("client_user_id = ? AND app_id = ? AND data_type = ?",
			backup.ClientUserID, backup.AppID, backup.DataType).
		Update("is_current", false)

	// 设置当前版本
	backup.IsCurrent = true
	model.DB.Save(&backup)

	response.Success(c, gin.H{
		"message": "已设置为当前版本",
	})
}

// maskAPIKey 隐藏 API Key 的部分内容
func maskAPIKey(dataJSON string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return dataJSON
	}

	if apiKey, ok := data["api_key"].(string); ok && len(apiKey) > 8 {
		// 只显示前4位和后4位
		data["api_key"] = apiKey[:4] + "****" + apiKey[len(apiKey)-4:]
	}

	result, _ := json.Marshal(data)
	return string(result)
}
