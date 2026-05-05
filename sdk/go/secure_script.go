package license

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
)

// SecureScriptManager 安全脚本管理器
type SecureScriptManager struct {
	client      *Client
	appSecret   string
	publicKey   *rsa.PublicKey
	scriptCache map[string]*CachedScript
	mu          sync.RWMutex
	onExecute   func(scriptID string, status string, err error)
}

// CachedScript 缓存的脚本
type CachedScript struct {
	ScriptID    string
	DeliveryID  string
	Version     string
	Content     []byte // 解密后的内容
	ContentHash string
	FetchedAt   time.Time
	ExpiresAt   time.Time
}

// EncryptedScriptPackage 加密脚本包
type EncryptedScriptPackage struct {
	ScriptID         string `json:"script_id"`
	DeliveryID       string `json:"delivery_id"`
	Version          string `json:"version"`
	ScriptType       string `json:"script_type"`
	EntryPoint       string `json:"entry_point"`
	EncryptedContent string `json:"encrypted_content"`
	ContentHash      string `json:"content_hash"`
	KeyHint          string `json:"key_hint"`
	Signature        string `json:"signature"`
	ExpiresAt        int64  `json:"expires_at"`
	Timeout          int    `json:"timeout"`
	MemoryLimit      int    `json:"memory_limit"`
	Parameters       string `json:"parameters"`
	ExecuteOnce      bool   `json:"execute_once"`
}

// ScriptVersionInfo 脚本版本信息
type ScriptVersionInfo struct {
	ScriptID    string `json:"script_id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
	UpdatedAt   int64  `json:"updated_at"`
}

// SecureScriptOption 配置选项
type SecureScriptOption func(*SecureScriptManager)

// WithAppSecret 设置应用密钥 (用于密钥派生)
func WithAppSecret(secret string) SecureScriptOption {
	return func(m *SecureScriptManager) {
		m.appSecret = secret
	}
}

// WithExecuteCallback 设置执行回调
func WithExecuteCallback(callback func(scriptID string, status string, err error)) SecureScriptOption {
	return func(m *SecureScriptManager) {
		m.onExecute = callback
	}
}

// WithPublicKey 设置公钥 (用于签名验证)
func WithPublicKey(publicKey *rsa.PublicKey) SecureScriptOption {
	return func(m *SecureScriptManager) {
		m.publicKey = publicKey
	}
}

// NewSecureScriptManager 创建安全脚本管理器
func NewSecureScriptManager(client *Client, opts ...SecureScriptOption) *SecureScriptManager {
	m := &SecureScriptManager{
		client:      client,
		scriptCache: make(map[string]*CachedScript),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GetScriptVersions 获取脚本版本列表
func (m *SecureScriptManager) GetScriptVersions() ([]ScriptVersionInfo, error) {
	data, err := m.client.requestWithClientSession("GET", "/secure-scripts/versions", nil)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化响应失败: %w", err)
	}
	var versions []ScriptVersionInfo
	if err := json.Unmarshal(dataBytes, &versions); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return versions, nil
}

// FetchScript 获取加密脚本并解密
func (m *SecureScriptManager) FetchScript(scriptID string) (*CachedScript, error) {
	// 检查缓存
	m.mu.RLock()
	if cached, ok := m.scriptCache[scriptID]; ok {
		if time.Now().Before(cached.ExpiresAt) {
			m.mu.RUnlock()
			return cached, nil
		}
	}
	m.mu.RUnlock()

	// 请求服务器
	reqBody := map[string]string{
		"script_id": scriptID,
	}

	data, err := m.client.requestWithClientSession("POST", "/secure-scripts/fetch", reqBody)
	if err != nil {
		return nil, fmt.Errorf("获取脚本失败: %w", err)
	}

	// 将 map 转换为 JSON 再解析
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化数据失败: %w", err)
	}

	var pkg EncryptedScriptPackage
	if err := json.Unmarshal(dataBytes, &pkg); err != nil {
		return nil, fmt.Errorf("解析脚本包失败: %w", err)
	}

	// 验证过期时间
	if time.Now().Unix() > pkg.ExpiresAt {
		return nil, errors.New("脚本包已过期")
	}

	// 验证签名
	signData := fmt.Sprintf("%s:%s:%s:%d", pkg.ScriptID, pkg.EncryptedContent, m.client.GetMachineID(), pkg.ExpiresAt)
	if err := m.verifySignature([]byte(signData), pkg.Signature); err != nil {
		return nil, fmt.Errorf("签名验证失败: %w", err)
	}

	// 派生解密密钥
	decryptKey, err := m.deriveKey(pkg.KeyHint)
	if err != nil {
		return nil, fmt.Errorf("派生密钥失败: %w", err)
	}

	// 解密内容
	content, err := m.decryptContent(pkg.EncryptedContent, decryptKey)
	if err != nil {
		return nil, fmt.Errorf("解密失败: %w", err)
	}

	// 验证哈希
	contentHash := sha256Hash(content)
	if contentHash != pkg.ContentHash {
		return nil, errors.New("内容哈希不匹配")
	}

	// 缓存
	cached := &CachedScript{
		ScriptID:    pkg.ScriptID,
		DeliveryID:  pkg.DeliveryID,
		Version:     pkg.Version,
		Content:     content,
		ContentHash: pkg.ContentHash,
		FetchedAt:   time.Now(),
		ExpiresAt:   time.Unix(pkg.ExpiresAt, 0),
	}

	m.mu.Lock()
	m.scriptCache[scriptID] = cached
	m.mu.Unlock()

	return cached, nil
}

// ExecuteScript 执行脚本 (需要外部执行器)
// executor: 执行函数，接收脚本内容和参数，返回结果和错误
func (m *SecureScriptManager) ExecuteScript(
	scriptID string,
	args map[string]interface{},
	executor func(content []byte, args map[string]interface{}) (string, error),
) (string, error) {
	// 获取脚本
	script, err := m.FetchScript(scriptID)
	if err != nil {
		m.reportExecution(scriptID, "", "failed", "", err.Error(), 0)
		if m.onExecute != nil {
			m.onExecute(scriptID, "failed", err)
		}
		return "", err
	}

	// 上报开始执行
	m.reportExecution(scriptID, script.DeliveryID, "executing", "", "", 0)
	if m.onExecute != nil {
		m.onExecute(scriptID, "executing", nil)
	}

	// 执行
	startTime := time.Now()
	result, execErr := executor(script.Content, args)
	duration := int(time.Since(startTime).Milliseconds())

	// 上报结果
	status := "success"
	errMsg := ""
	if execErr != nil {
		status = "failed"
		errMsg = execErr.Error()
	}

	m.reportExecution(scriptID, script.DeliveryID, status, result, errMsg, duration)
	if m.onExecute != nil {
		m.onExecute(scriptID, status, execErr)
	}

	// 执行后清除缓存 (安全考虑)
	m.mu.Lock()
	delete(m.scriptCache, scriptID)
	m.mu.Unlock()

	return result, execErr
}

// ClearCache 清除脚本缓存
func (m *SecureScriptManager) ClearCache() {
	m.mu.Lock()
	m.scriptCache = make(map[string]*CachedScript)
	m.mu.Unlock()
}

// GetCachedScript 获取缓存的脚本 (不请求服务器)
func (m *SecureScriptManager) GetCachedScript(scriptID string) *CachedScript {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scriptCache[scriptID]
}

// 内部方法

func (m *SecureScriptManager) deriveKey(keyHint string) ([]byte, error) {
	if m.appSecret == "" {
		return nil, errors.New("未设置应用密钥")
	}

	masterKey := []byte(m.appSecret)
	salt := m.client.GetMachineID()
	info := keyHint

	hash := sha256.New
	hkdfReader := hkdf.New(hash, masterKey, []byte(salt), []byte(info))

	derivedKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, err
	}

	return derivedKey, nil
}

func (m *SecureScriptManager) decryptContent(encryptedBase64 string, key []byte) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("密文太短")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func (m *SecureScriptManager) verifySignature(data []byte, signatureBase64 string) error {
	if m.publicKey == nil {
		return nil // 如果没有设置公钥，跳过验证
	}

	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return err
	}

	hashed := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(m.publicKey, crypto.SHA256, hashed[:], signature)
}

func (m *SecureScriptManager) reportExecution(scriptID, deliveryID, status, result, errorMsg string, duration int) {
	reqBody := map[string]interface{}{
		"script_id": scriptID,
		"status":    status,
	}
	if deliveryID != "" {
		reqBody["delivery_id"] = deliveryID
	}
	if result != "" {
		reqBody["result"] = result
	}
	if errorMsg != "" {
		reqBody["error_message"] = errorMsg
	}
	if duration > 0 {
		reqBody["duration"] = duration
	}

	// 异步上报
	go m.client.requestWithClientSession("POST", "/secure-scripts/report", reqBody)
}

func sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
