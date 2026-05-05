package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SecureScriptService 安全脚本服务
type SecureScriptService struct{}

func NewSecureScriptService() *SecureScriptService {
	return &SecureScriptService{}
}

// EncryptedScriptPackage 加密脚本包 (下发给客户端)
type EncryptedScriptPackage struct {
	ScriptID         string `json:"script_id"`
	DeliveryID       string `json:"delivery_id,omitempty"`
	Version          string `json:"version"`
	ScriptType       string `json:"script_type"`
	EntryPoint       string `json:"entry_point"`
	EncryptedContent string `json:"encrypted_content"` // Base64(AES加密内容)
	ContentHash      string `json:"content_hash"`      // 原始内容SHA256
	KeyHint          string `json:"key_hint"`          // 密钥派生提示
	Signature        string `json:"signature"`         // RSA签名
	ExpiresAt        int64  `json:"expires_at"`        // 过期时间戳
	Timeout          int    `json:"timeout"`           // 执行超时(秒)
	MemoryLimit      int    `json:"memory_limit"`      // 内存限制(MB)
	Parameters       string `json:"parameters"`        // 参数定义
	ExecuteOnce      bool   `json:"execute_once"`      // 是否只执行一次
}

// EncryptScriptForStorage 加密脚本用于存储
// 返回: 加密内容, 存储密钥(RSA加密), 内容哈希, 错误
func (s *SecureScriptService) EncryptScriptForStorage(content []byte, appPublicKey string) ([]byte, string, string, error) {
	// 1. 生成随机 AES 密钥
	aesKey, err := crypto.GenerateAESKey()
	if err != nil {
		return nil, "", "", fmt.Errorf("生成密钥失败: %w", err)
	}

	// 2. AES-GCM 加密脚本内容
	encryptedContent, err := crypto.EncryptAESGCM(content, aesKey)
	if err != nil {
		return nil, "", "", fmt.Errorf("加密内容失败: %w", err)
	}

	// 3. 用应用公钥加密 AES 密钥
	encryptedKey, err := crypto.Encrypt(appPublicKey, aesKey)
	if err != nil {
		return nil, "", "", fmt.Errorf("加密密钥失败: %w", err)
	}

	// 4. 计算原始内容哈希
	contentHash := crypto.SHA256Hash(content)

	return encryptedContent, encryptedKey, contentHash, nil
}

// DecryptScriptFromStorage 从存储解密脚本
func (s *SecureScriptService) DecryptScriptFromStorage(encryptedContent []byte, encryptedKey string, appPrivateKey string) ([]byte, error) {
	// 1. 用应用私钥解密 AES 密钥
	aesKey, err := crypto.Decrypt(appPrivateKey, encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("解密密钥失败: %w", err)
	}

	// 2. AES-GCM 解密内容
	content, err := crypto.DecryptAESGCM(encryptedContent, aesKey)
	if err != nil {
		return nil, fmt.Errorf("解密内容失败: %w", err)
	}

	return content, nil
}

// PrepareScriptForDelivery 准备脚本下发包
func (s *SecureScriptService) PrepareScriptForDelivery(
	script *model.SecureScript,
	app *model.Application,
	machineID string,
	validDuration time.Duration,
) (*EncryptedScriptPackage, string, error) {
	// 1. 解密存储的脚本
	content, err := s.DecryptScriptFromStorage(script.EncryptedContent, script.StorageKey, app.PrivateKey)
	if err != nil {
		return nil, "", fmt.Errorf("解密脚本失败: %w", err)
	}

	// 2. 生成本次下发专用的密钥提示
	keyHint, err := crypto.GenerateNonce(16)
	if err != nil {
		return nil, "", fmt.Errorf("生成密钥提示失败: %w", err)
	}

	// 3. 派生设备专用密钥
	// 使用: 应用密钥 + 机器码 + 密钥提示
	masterKey := []byte(app.AppSecret)
	salt := machineID
	info := keyHint
	deliveryKey, err := crypto.DeriveKey(masterKey, salt, info)
	if err != nil {
		return nil, "", fmt.Errorf("派生密钥失败: %w", err)
	}

	// 4. 用派生密钥重新加密脚本
	encryptedContent, err := crypto.EncryptAESGCMBase64(content, deliveryKey)
	if err != nil {
		return nil, "", fmt.Errorf("加密下发内容失败: %w", err)
	}

	// 5. 设置过期时间
	expiresAt := time.Now().Add(validDuration).Unix()

	// 6. 构建签名数据
	signData := fmt.Sprintf("%s:%s:%s:%d", script.ID, encryptedContent, machineID, expiresAt)

	// 7. RSA 签名
	signature, err := crypto.Sign(app.PrivateKey, []byte(signData))
	if err != nil {
		return nil, "", fmt.Errorf("签名失败: %w", err)
	}

	// 8. 构建下发包
	pkg := &EncryptedScriptPackage{
		ScriptID:         script.ID,
		Version:          script.Version,
		ScriptType:       string(script.ScriptType),
		EntryPoint:       script.EntryPoint,
		EncryptedContent: encryptedContent,
		ContentHash:      script.ContentHash,
		KeyHint:          keyHint,
		Signature:        signature,
		ExpiresAt:        expiresAt,
		Timeout:          script.Timeout,
		MemoryLimit:      script.MemoryLimit,
		Parameters:       script.Parameters,
		ExecuteOnce:      true,
	}

	return pkg, keyHint, nil
}

// InstructionPackage 指令包
type InstructionPackage struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
	ExpiresAt int64  `json:"expires_at"`
}

// PrepareInstruction 准备实时指令
func (s *SecureScriptService) PrepareInstruction(
	instruction *model.RealtimeInstruction,
	app *model.Application,
) (*InstructionPackage, error) {
	// 1. 生成时间戳和 Nonce
	timestamp := time.Now().Unix()
	nonce, err := crypto.GenerateNonce(16)
	if err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}

	// 2. 构建签名数据
	signData := fmt.Sprintf("%s:%s:%s:%d:%s", instruction.ID, instruction.Type, instruction.Payload, timestamp, nonce)

	// 3. RSA 签名
	signature, err := crypto.Sign(app.PrivateKey, []byte(signData))
	if err != nil {
		return nil, fmt.Errorf("签名失败: %w", err)
	}

	// 4. 设置过期时间 (5分钟)
	expiresAt := time.Now().Add(5 * time.Minute).Unix()

	return &InstructionPackage{
		ID:        instruction.ID,
		Type:      string(instruction.Type),
		Payload:   instruction.Payload,
		Timestamp: timestamp,
		Nonce:     nonce,
		Signature: signature,
		ExpiresAt: expiresAt,
	}, nil
}

// VerifyExecutionReport 验证执行报告
func (s *SecureScriptService) VerifyExecutionReport(
	scriptID string,
	machineID string,
	resultHash string,
	signature string,
	appPublicKey string,
) error {
	// 构建验证数据
	verifyData := fmt.Sprintf("%s:%s:%s", scriptID, machineID, resultHash)

	// 验证签名
	if err := crypto.Verify(appPublicKey, []byte(verifyData), signature); err != nil {
		return errors.New("签名验证失败")
	}

	return nil
}

// ScriptVersionInfo 脚本版本信息
type ScriptVersionInfo struct {
	ScriptID    string `json:"script_id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
	UpdatedAt   int64  `json:"updated_at"`
}

// GetScriptVersions 获取脚本版本列表 (用于客户端检查更新)
func (s *SecureScriptService) GetScriptVersions(appID string) ([]ScriptVersionInfo, error) {
	var scripts []model.SecureScript
	if err := model.DB.Where("app_id = ? AND status = ?", appID, model.SecureScriptStatusPublished).
		Select("id", "name", "version", "content_hash", "updated_at").
		Find(&scripts).Error; err != nil {
		return nil, err
	}

	versions := make([]ScriptVersionInfo, len(scripts))
	for i, script := range scripts {
		versions[i] = ScriptVersionInfo{
			ScriptID:    script.ID,
			Name:        script.Name,
			Version:     script.Version,
			ContentHash: script.ContentHash,
			UpdatedAt:   script.UpdatedAt.Unix(),
		}
	}

	return versions, nil
}

// CheckDevicePermission 检查设备是否有权限获取脚本
func (s *SecureScriptService) CheckDevicePermission(script *model.SecureScript, machineID string, features []string) error {
	// 1. 检查设备白名单
	if script.AllowedDevices != "" && script.AllowedDevices != "[]" {
		var allowedDevices []string
		if err := json.Unmarshal([]byte(script.AllowedDevices), &allowedDevices); err == nil && len(allowedDevices) > 0 {
			allowed := false
			for _, d := range allowedDevices {
				if d == machineID {
					allowed = true
					break
				}
			}
			if !allowed {
				return errors.New("设备不在允许列表中")
			}
		}
	}

	// 2. 检查功能权限
	if script.RequiredFeatures != "" && script.RequiredFeatures != "[]" {
		var requiredFeatures []string
		if err := json.Unmarshal([]byte(script.RequiredFeatures), &requiredFeatures); err == nil && len(requiredFeatures) > 0 {
			for _, required := range requiredFeatures {
				hasFeature := false
				for _, f := range features {
					if f == required {
						hasFeature = true
						break
					}
				}
				if !hasFeature {
					return fmt.Errorf("缺少功能权限: %s", required)
				}
			}
		}
	}

	// 3. 检查脚本是否过期
	if script.ExpiresAt != nil && script.ExpiresAt.Before(time.Now()) {
		return errors.New("脚本已过期")
	}

	return nil
}

// RecordDelivery 记录脚本下发
func (s *SecureScriptService) RecordDelivery(
	scriptID string,
	deviceID string,
	machineID string,
	licenseID string,
	keyHint string,
	expiresAt time.Time,
	ipAddress string,
) (*model.ScriptDelivery, error) {
	delivery := &model.ScriptDelivery{
		ScriptID:    scriptID,
		DeviceID:    deviceID,
		MachineID:   machineID,
		LicenseID:   licenseID,
		DeliveryKey: keyHint,
		ExpiresAt:   expiresAt,
		IPAddress:   ipAddress,
		Status:      model.ScriptDeliveryStatusPending,
	}

	if err := model.DB.Create(delivery).Error; err != nil {
		return nil, err
	}

	// 更新脚本下发计数
	model.DB.Model(&model.SecureScript{}).Where("id = ?", scriptID).
		UpdateColumn("delivery_count", model.DB.Raw("delivery_count + 1"))

	return delivery, nil
}

// UpdateDeliveryStatus 更新下发状态
func (s *SecureScriptService) UpdateDeliveryStatus(
	deliveryID string,
	status model.ScriptDeliveryStatus,
	result string,
	errorMessage string,
	duration int,
) error {
	var delivery model.ScriptDelivery
	if err := model.DB.First(&delivery, "id = ?", deliveryID).Error; err != nil {
		return err
	}

	return s.updateDeliveryStatus(&delivery, status, result, errorMessage, duration)
}

// UpdateDeliveryStatusForDevice 更新指定设备的下发状态
func (s *SecureScriptService) UpdateDeliveryStatusForDevice(
	deliveryID string,
	appID string,
	deviceID string,
	machineID string,
	status model.ScriptDeliveryStatus,
	result string,
	errorMessage string,
	duration int,
) error {
	var delivery model.ScriptDelivery
	if err := model.DB.Model(&model.ScriptDelivery{}).
		Joins("JOIN secure_scripts ON secure_scripts.id = script_deliveries.script_id").
		Where(
			"script_deliveries.id = ? AND script_deliveries.device_id = ? AND script_deliveries.machine_id = ? AND secure_scripts.app_id = ?",
			deliveryID,
			deviceID,
			machineID,
			appID,
		).
		First(&delivery).Error; err != nil {
		return err
	}

	return s.updateDeliveryStatus(&delivery, status, result, errorMessage, duration)
}

func (s *SecureScriptService) updateDeliveryStatus(
	delivery *model.ScriptDelivery,
	status model.ScriptDeliveryStatus,
	result string,
	errorMessage string,
	duration int,
) error {
	switch status {
	case model.ScriptDeliveryStatusPending,
		model.ScriptDeliveryStatusExecuting,
		model.ScriptDeliveryStatusSuccess,
		model.ScriptDeliveryStatusFailed,
		model.ScriptDeliveryStatusExpired:
	default:
		return errors.New("无效的下发状态")
	}

	return model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ScriptDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", delivery.ID).Error; err != nil {
			return err
		}
		if isTerminalScriptDeliveryStatus(current.Status) {
			return nil
		}

		now := time.Now()
		if now.After(current.ExpiresAt) && status != model.ScriptDeliveryStatusExpired {
			return errors.New("下发已过期")
		}

		updates := map[string]interface{}{
			"status": status,
		}

		if result != "" {
			updates["result"] = result
		}
		if errorMessage != "" {
			updates["error_message"] = errorMessage
		}
		if duration > 0 {
			updates["duration"] = duration
		}

		if status == model.ScriptDeliveryStatusExecuting {
			updates["executed_at"] = &now
		}
		if status == model.ScriptDeliveryStatusSuccess || status == model.ScriptDeliveryStatusFailed {
			updates["completed_at"] = &now
		}

		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}

		switch status {
		case model.ScriptDeliveryStatusSuccess:
			return tx.Model(&model.SecureScript{}).Where("id = ?", current.ScriptID).
				UpdateColumns(map[string]interface{}{
					"execute_count": gorm.Expr("execute_count + 1"),
					"success_count": gorm.Expr("success_count + 1"),
				}).Error
		case model.ScriptDeliveryStatusFailed:
			return tx.Model(&model.SecureScript{}).Where("id = ?", current.ScriptID).
				UpdateColumns(map[string]interface{}{
					"execute_count": gorm.Expr("execute_count + 1"),
					"fail_count":    gorm.Expr("fail_count + 1"),
				}).Error
		default:
			return nil
		}
	})
}

func isTerminalScriptDeliveryStatus(status model.ScriptDeliveryStatus) bool {
	return status == model.ScriptDeliveryStatusSuccess ||
		status == model.ScriptDeliveryStatusFailed ||
		status == model.ScriptDeliveryStatusExpired
}

// EncryptedKeyInfo 用于存储加密密钥的结构
type EncryptedKeyInfo struct {
	EncryptedKey string `json:"encrypted_key"` // RSA加密的AES密钥
	KeyVersion   int    `json:"key_version"`   // 密钥版本
}

// RotateScriptKey 轮换脚本加密密钥
func (s *SecureScriptService) RotateScriptKey(script *model.SecureScript, app *model.Application) error {
	// 1. 解密现有内容
	content, err := s.DecryptScriptFromStorage(script.EncryptedContent, script.StorageKey, app.PrivateKey)
	if err != nil {
		return fmt.Errorf("解密失败: %w", err)
	}

	// 2. 用新密钥重新加密
	encryptedContent, encryptedKey, _, err := s.EncryptScriptForStorage(content, app.PublicKey)
	if err != nil {
		return fmt.Errorf("重新加密失败: %w", err)
	}

	// 3. 更新数据库
	return model.DB.Model(script).Updates(map[string]interface{}{
		"encrypted_content": encryptedContent,
		"storage_key":       encryptedKey,
		"encrypted_size":    int64(len(encryptedContent)),
	}).Error
}
