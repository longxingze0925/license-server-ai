// Package license 提供授权管理系统的 Go 客户端 SDK
//
// 支持两种授权模式：
// 1. 授权码模式 - 使用授权码激活
// 2. 账号密码模式 - 使用账号密码登录
//
// 安全特性：
// - 证书固定（Certificate Pinning）防止中间人攻击
// - 缓存加密（AES-256-GCM）
// - 机器码绑定
//
// 使用示例：
//
//	// 方式1：使用证书指纹（推荐）
//	client := license.NewClient("https://192.168.1.100:8080", "your_app_key",
//	    license.WithCertFingerprint("SHA256:AB:CD:EF:..."),
//	)
//
//	// 方式2：使用证书文件
//	client := license.NewClient("https://192.168.1.100:8080", "your_app_key",
//	    license.WithCertFile("./server.crt"),
//	)
//
//	// 方式3：跳过验证（仅测试用）
//	client := license.NewClient("https://192.168.1.100:8080", "your_app_key",
//	    license.WithSkipVerify(true),
//	)
//
//	// 授权码模式激活
//	result, err := client.Activate("XXXX-XXXX-XXXX-XXXX")
//
//	// 或账号密码模式登录
//	result, err := client.Login("user@example.com", "password")
//
//	// 检查授权状态
//	if client.IsValid() {
//	    fmt.Println("授权有效")
//	}
package license

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
)

// ErrCertificatePinning 证书固定验证失败错误
var ErrCertificatePinning = errors.New("证书指纹不匹配，可能存在中间人攻击")

// ErrSignatureVerification 签名验证失败错误
var ErrSignatureVerification = errors.New("签名验证失败，数据可能被篡改")

// ErrSignatureMissing 缺少签名错误
var ErrSignatureMissing = errors.New("响应缺少签名，无法验证数据完整性")

// ErrSignatureExpired 签名过期错误
var ErrSignatureExpired = errors.New("签名已过期，可能是重放攻击")

// ErrInvalidPublicKey 无效公钥错误
var ErrInvalidPublicKey = errors.New("无效的公钥格式")

// ErrCacheIntegrity 缓存完整性校验失败
var ErrCacheIntegrity = errors.New("缓存完整性校验失败，数据可能被篡改")

// ErrCacheExpired 缓存已过期
var ErrCacheExpired = errors.New("缓存已过期，需要重新验证")

const clientTokenRefreshSkew = time.Minute

// LicenseInfo 授权信息
type LicenseInfo struct {
	Valid            bool     `json:"valid"`
	LicenseID        string   `json:"license_id,omitempty"`
	SubscriptionID   string   `json:"subscription_id,omitempty"`
	CustomerID       string   `json:"customer_id,omitempty"`
	DeviceID         string   `json:"device_id"`
	Type             string   `json:"type,omitempty"`
	PlanType         string   `json:"plan_type,omitempty"`
	ExpireAt         *string  `json:"expire_at,omitempty"`
	RemainingDays    int      `json:"remaining_days"`
	Features         []string `json:"features"`
	Signature        string   `json:"signature,omitempty"`
	LicenseKey       string   `json:"license_key,omitempty"`
	Email            string   `json:"email,omitempty"`
	LastVerifiedAt   int64    `json:"last_verified_at,omitempty"`
	AccessToken      string   `json:"access_token,omitempty"`
	RefreshToken     string   `json:"refresh_token,omitempty"`
	TokenType        string   `json:"token_type,omitempty"`
	ExpiresIn        int      `json:"expires_in,omitempty"`
	RefreshExpiresIn int      `json:"refresh_expires_in,omitempty"`
	AccessExpiresAt  int64    `json:"access_expires_at,omitempty"`
	RefreshExpiresAt int64    `json:"refresh_expires_at,omitempty"`
	SessionID        string   `json:"session_id,omitempty"`
	AuthMode         string   `json:"auth_mode,omitempty"`
}

// DeviceInfo 设备信息
type DeviceInfo struct {
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	OSVersion  string `json:"os_version"`
	AppVersion string `json:"app_version"`
}

// UpdateInfo 更新信息
type UpdateInfo struct {
	Version       string `json:"version"`
	VersionCode   int    `json:"version_code"`
	DownloadURL   string `json:"download_url"`
	Changelog     string `json:"changelog"`
	FileSize      int64  `json:"file_size"`
	FileHash      string `json:"file_hash"`
	FileSignature string `json:"file_signature"`
	SignatureAlg  string `json:"signature_alg"`
	ForceUpdate   bool   `json:"force_update"`
}

// Client 授权客户端
type Client struct {
	serverURL         string
	appKey            string
	cacheDir          string
	heartbeatInterval time.Duration
	offlineGraceDays  int
	appVersion        string
	encryptCache      bool

	// 证书固定配置
	certFingerprint string // 证书指纹 SHA256
	certFile        string // 证书文件路径
	skipVerify      bool   // 跳过验证（仅测试用）

	// 签名验证配置
	publicKeyPEM        string // 服务端公钥 PEM 格式
	requireSignature    bool   // 是否强制要求签名验证
	signatureTimeWindow int64  // 签名时间窗口（秒），防止重放攻击

	// 连接配置
	timeout    time.Duration
	maxRetries int

	licenseInfo *LicenseInfo
	machineID   string
	httpClient  *http.Client

	stopHeartbeat chan struct{}
	heartbeatOnce sync.Once
	mu            sync.RWMutex
}

// Option 客户端配置选项
type Option func(*Client)

// WithCacheDir 设置缓存目录
func WithCacheDir(dir string) Option {
	return func(c *Client) {
		c.cacheDir = dir
	}
}

// WithHeartbeatInterval 设置心跳间隔
func WithHeartbeatInterval(d time.Duration) Option {
	return func(c *Client) {
		c.heartbeatInterval = d
	}
}

// WithOfflineGraceDays 设置离线宽限期
func WithOfflineGraceDays(days int) Option {
	return func(c *Client) {
		c.offlineGraceDays = days
	}
}

// WithAppVersion 设置应用版本
func WithAppVersion(version string) Option {
	return func(c *Client) {
		c.appVersion = version
	}
}

// WithEncryptCache 设置是否加密缓存
func WithEncryptCache(encrypt bool) Option {
	return func(c *Client) {
		c.encryptCache = encrypt
	}
}

// WithCertFingerprint 设置证书指纹（防止中间人攻击）
// 格式: "SHA256:AB:CD:EF:..." 或 "ABCDEF..."
func WithCertFingerprint(fingerprint string) Option {
	return func(c *Client) {
		c.certFingerprint = fingerprint
	}
}

// WithCertFile 设置证书文件路径（PEM 格式）
func WithCertFile(certPath string) Option {
	return func(c *Client) {
		c.certFile = certPath
	}
}

// WithSkipVerify 跳过证书验证（仅测试环境使用！）
func WithSkipVerify(skip bool) Option {
	return func(c *Client) {
		c.skipVerify = skip
	}
}

// WithTimeout 设置请求超时时间
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.timeout = d
	}
}

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		c.maxRetries = n
	}
}

// WithServerPublicKey 设置服务端公钥（用于签名验证，强烈推荐）
// publicKeyPEM: PEM 格式的 RSA 公钥
func WithServerPublicKey(publicKeyPEM string) Option {
	return func(c *Client) {
		c.publicKeyPEM = publicKeyPEM
		c.requireSignature = true // 设置公钥后默认启用签名验证
	}
}

// WithRequireSignature 设置是否强制要求签名验证
// 如果设置为 true，没有签名或签名验证失败的响应将被拒绝
func WithRequireSignature(require bool) Option {
	return func(c *Client) {
		c.requireSignature = require
	}
}

// WithSignatureTimeWindow 设置签名时间窗口（防止重放攻击）
// 默认 300 秒（5分钟），设置为 0 表示不检查时间
func WithSignatureTimeWindow(seconds int64) Option {
	return func(c *Client) {
		c.signatureTimeWindow = seconds
	}
}

// NewClient 创建授权客户端
func NewClient(serverURL, appKey string, opts ...Option) *Client {
	homeDir, _ := os.UserHomeDir()
	c := &Client{
		serverURL:           serverURL,
		appKey:              appKey,
		cacheDir:            filepath.Join(homeDir, ".license_cache"),
		heartbeatInterval:   time.Hour,
		offlineGraceDays:    7,
		appVersion:          "1.0.0",
		encryptCache:        true,
		timeout:             30 * time.Second,
		maxRetries:          3,
		signatureTimeWindow: 300, // 默认5分钟时间窗口
		stopHeartbeat:       make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	// 确保缓存目录存在
	os.MkdirAll(c.cacheDir, 0755)

	// 生成机器码
	c.machineID = c.generateMachineID()

	// 初始化 HTTP 客户端（带证书固定）
	c.initHTTPClient()

	// 加载缓存
	c.loadCache()

	return c
}

// initHTTPClient 初始化 HTTP 客户端，配置证书固定
func (c *Client) initHTTPClient() {
	tlsConfig := &tls.Config{}

	if c.skipVerify {
		// 跳过验证（仅测试用）
		tlsConfig.InsecureSkipVerify = true
	} else if c.certFile != "" {
		// 使用指定的证书文件
		certPool := x509.NewCertPool()
		certData, err := os.ReadFile(c.certFile)
		if err == nil {
			certPool.AppendCertsFromPEM(certData)
			tlsConfig.RootCAs = certPool
		}
	}

	// 如果配置了证书指纹，添加验证回调
	if c.certFingerprint != "" && !c.skipVerify {
		expectedFingerprint := c.normalizeFingerprint(c.certFingerprint)
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return ErrCertificatePinning
			}
			// 计算服务器证书的指纹
			actualFingerprint := sha256.Sum256(rawCerts[0])
			actualFingerprintHex := hex.EncodeToString(actualFingerprint[:])

			if actualFingerprintHex != expectedFingerprint {
				return fmt.Errorf("%w: 期望 %s, 实际 %s",
					ErrCertificatePinning, expectedFingerprint, actualFingerprintHex)
			}
			return nil
		}
		// 使用自定义验证时需要跳过默认验证
		if c.certFile == "" {
			tlsConfig.InsecureSkipVerify = true
		}
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		DialContext: (&net.Dialer{
			Timeout:   c.timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	c.httpClient = &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
	}
}

// normalizeFingerprint 标准化证书指纹格式
func (c *Client) normalizeFingerprint(fp string) string {
	fp = strings.ToUpper(fp)
	if strings.HasPrefix(fp, "SHA256:") {
		fp = fp[7:]
	}
	fp = strings.ReplaceAll(fp, ":", "")
	return strings.ToLower(fp)
}

// GetServerCertificateFingerprint 获取服务器证书的 SHA256 指纹
// 用于首次配置证书固定时获取服务器证书指纹
func GetServerCertificateFingerprint(host string, port int) (string, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	conn, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return "", fmt.Errorf("连接服务器失败: %w", err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("未获取到服务器证书")
	}

	fingerprint := sha256.Sum256(certs[0].Raw)
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	// 格式化为易读格式
	var formatted []string
	for i := 0; i < len(fingerprintHex); i += 2 {
		formatted = append(formatted, strings.ToUpper(fingerprintHex[i:i+2]))
	}

	return "SHA256:" + strings.Join(formatted, ":"), nil
}

// GetCertificateFingerprintFromFile 从证书文件获取指纹
func GetCertificateFingerprintFromFile(certPath string) (string, error) {
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("读取证书文件失败: %w", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return "", errors.New("无法解析 PEM 格式证书")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("解析证书失败: %w", err)
	}

	fingerprint := sha256.Sum256(cert.Raw)
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	var formatted []string
	for i := 0; i < len(fingerprintHex); i += 2 {
		formatted = append(formatted, strings.ToUpper(fingerprintHex[i:i+2]))
	}

	return "SHA256:" + strings.Join(formatted, ":"), nil
}

// GetServerURL 获取服务器地址
func (c *Client) GetServerURL() string {
	return c.serverURL
}

// GetAppKey 获取应用密钥
func (c *Client) GetAppKey() string {
	return c.appKey
}

// GetMachineID 获取机器码
func (c *Client) GetMachineID() string {
	return c.machineID
}

// GetHTTPClient 获取 HTTP 客户端
func (c *Client) GetHTTPClient() *http.Client {
	return c.httpClient
}

// generateMachineID 生成机器码（增强版）
func (c *Client) generateMachineID() string {
	var infoParts []string

	hostname, _ := os.Hostname()
	infoParts = append(infoParts, hostname, runtime.GOOS, runtime.GOARCH)

	// 获取 MAC 地址
	if mac := c.getMACAddress(); mac != "" {
		infoParts = append(infoParts, mac)
	}

	// 获取平台特定的硬件信息
	if hwID := c.getHardwareID(); hwID != "" {
		infoParts = append(infoParts, hwID)
	}

	info := strings.Join(infoParts, "|")
	hash := sha256.Sum256([]byte(info))
	return hex.EncodeToString(hash[:])[:32]
}

// getMACAddress 获取 MAC 地址
func (c *Client) getMACAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		// 跳过回环接口和无效接口
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		// 跳过虚拟接口
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		// 检查是否是真实 MAC（非本地管理地址）
		if iface.HardwareAddr[0]&0x02 == 0 {
			return iface.HardwareAddr.String()
		}
	}
	return ""
}

// getHardwareID 获取硬件 ID（平台特定）
func (c *Client) getHardwareID() string {
	switch runtime.GOOS {
	case "windows":
		return c.getWindowsHardwareID()
	case "linux":
		return c.getLinuxMachineID()
	case "darwin":
		return c.getMacOSHardwareUUID()
	}
	return ""
}

// getWindowsHardwareID 获取 Windows 硬盘序列号
func (c *Client) getWindowsHardwareID() string {
	cmd := exec.Command("wmic", "diskdrive", "get", "serialnumber")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && line != "SerialNumber" {
			return line
		}
	}
	return ""
}

// getLinuxMachineID 获取 Linux 机器 ID
func (c *Client) getLinuxMachineID() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// getMacOSHardwareUUID 获取 macOS 硬件 UUID
func (c *Client) getMacOSHardwareUUID() string {
	cmd := exec.Command("system_profiler", "SPHardwareDataType")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "Hardware UUID") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// getDeviceInfo 获取设备信息
func (c *Client) getDeviceInfo() DeviceInfo {
	hostname, _ := os.Hostname()
	return DeviceInfo{
		Name:       hostname,
		Hostname:   hostname,
		OS:         runtime.GOOS,
		OSVersion:  runtime.GOARCH,
		AppVersion: c.appVersion,
	}
}

// getCachePath 获取缓存文件路径
func (c *Client) getCachePath() string {
	suffix := ".json"
	if c.encryptCache {
		suffix = ".enc"
	}
	return filepath.Join(c.cacheDir, c.appKey+suffix)
}

// deriveKey 使用 HKDF 从机器码和应用密钥派生加密密钥
// 使用多个因素增加破解难度
func (c *Client) deriveKey(purpose string) []byte {
	// 主密钥材料：机器码 + 应用密钥 + 硬编码盐值
	masterKey := []byte(c.machineID + c.appKey)

	// 使用 HKDF 派生密钥
	// salt 使用机器码的哈希，增加唯一性
	salt := sha256.Sum256([]byte(c.machineID + "license_salt_v2"))

	// info 包含用途，使不同用途的密钥不同
	info := []byte("license_cache_" + purpose + "_v2")

	hkdfReader := hkdf.New(sha256.New, masterKey, salt[:], info)
	derivedKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		// 降级到简单哈希
		hash := sha256.Sum256([]byte(c.machineID + c.appKey + purpose))
		return hash[:]
	}
	return derivedKey
}

// deriveHMACKey 派生 HMAC 密钥（用于完整性校验）
func (c *Client) deriveHMACKey() []byte {
	return c.deriveKey("hmac")
}

// deriveEncryptionKey 派生加密密钥
func (c *Client) deriveEncryptionKey() []byte {
	return c.deriveKey("encryption")
}

// computeHMAC 计算数据的 HMAC
func (c *Client) computeHMAC(data []byte) []byte {
	h := hmac.New(sha256.New, c.deriveHMACKey())
	h.Write(data)
	return h.Sum(nil)
}

// verifyHMAC 验证数据的 HMAC
func (c *Client) verifyHMAC(data, expectedMAC []byte) bool {
	actualMAC := c.computeHMAC(data)
	return hmac.Equal(actualMAC, expectedMAC)
}

// encrypt 加密数据（带完整性校验）
func (c *Client) encrypt(data []byte) ([]byte, error) {
	key := c.deriveEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// 添加时间戳到数据中（用于检测过期）
	timestamp := time.Now().Unix()
	dataWithTime := map[string]interface{}{
		"data":      base64.StdEncoding.EncodeToString(data),
		"timestamp": timestamp,
	}
	jsonData, _ := json.Marshal(dataWithTime)

	ciphertext := gcm.Seal(nonce, nonce, jsonData, nil)

	// 计算 HMAC
	mac := c.computeHMAC(ciphertext)

	// 格式: base64(HMAC + ciphertext)
	result := append(mac, ciphertext...)
	return []byte(base64.StdEncoding.EncodeToString(result)), nil
}

// decrypt 解密数据（带完整性校验）
func (c *Client) decrypt(data []byte) ([]byte, error) {
	combined, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}

	// 分离 HMAC 和密文
	if len(combined) < 32 {
		return nil, ErrCacheIntegrity
	}
	mac := combined[:32]
	ciphertext := combined[32:]

	// 验证 HMAC
	if !c.verifyHMAC(ciphertext, mac) {
		return nil, ErrCacheIntegrity
	}

	key := c.deriveEncryptionKey()
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
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	jsonData, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	// 解析带时间戳的数据
	var dataWithTime struct {
		Data      string `json:"data"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(jsonData, &dataWithTime); err != nil {
		return nil, err
	}

	// 检查缓存是否过期（最大缓存时间：30天）
	maxCacheAge := int64(30 * 24 * 60 * 60)
	if time.Now().Unix()-dataWithTime.Timestamp > maxCacheAge {
		return nil, ErrCacheExpired
	}

	return base64.StdEncoding.DecodeString(dataWithTime.Data)
}

// loadCache 加载缓存（带完整性校验）
func (c *Client) loadCache() {
	cachePath := c.getCachePath()
	data, err := os.ReadFile(cachePath)
	if err != nil {
		// 尝试加载旧格式缓存并迁移
		oldPath := filepath.Join(c.cacheDir, c.appKey+".json")
		if c.encryptCache {
			if oldData, err := os.ReadFile(oldPath); err == nil {
				var info LicenseInfo
				if json.Unmarshal(oldData, &info) == nil {
					// 旧格式缓存不可信，需要重新验证
					// 只有在有签名的情况下才接受
					if info.Signature != "" && c.publicKeyPEM != "" {
						c.licenseInfo = &info
						c.saveCache() // 保存为新加密格式
					}
					os.Remove(oldPath) // 删除旧文件
				}
			}
		}
		return
	}

	var jsonData []byte
	if c.encryptCache {
		jsonData, err = c.decrypt(data)
		if err != nil {
			// 缓存损坏或被篡改，删除并要求重新验证
			os.Remove(cachePath)
			return
		}
	} else {
		jsonData = data
	}

	var info LicenseInfo
	if err := json.Unmarshal(jsonData, &info); err != nil {
		return
	}

	// 验证缓存中的签名（如果启用了签名验证）
	if c.requireSignature && c.publicKeyPEM != "" {
		if info.Signature == "" {
			// 缓存中没有签名，不可信
			os.Remove(cachePath)
			return
		}
		// 重建数据并验证签名
		if err := c.verifyCachedLicenseSignature(&info); err != nil {
			// 签名验证失败，缓存不可信
			os.Remove(cachePath)
			return
		}
	}

	c.licenseInfo = &info
}

// verifyCachedLicenseSignature 验证缓存的授权信息签名
func (c *Client) verifyCachedLicenseSignature(info *LicenseInfo) error {
	if c.publicKeyPEM == "" || info.Signature == "" {
		return nil
	}

	// 构建用于签名验证的数据
	dataMap := map[string]interface{}{
		"valid":          info.Valid,
		"device_id":      info.DeviceID,
		"remaining_days": info.RemainingDays,
		"features":       info.Features,
	}
	if info.LicenseID != "" {
		dataMap["license_id"] = info.LicenseID
	}
	if info.SubscriptionID != "" {
		dataMap["subscription_id"] = info.SubscriptionID
	}
	if info.CustomerID != "" {
		dataMap["customer_id"] = info.CustomerID
	}
	if info.Type != "" {
		dataMap["type"] = info.Type
	}
	if info.PlanType != "" {
		dataMap["plan_type"] = info.PlanType
	}
	if info.ExpireAt != nil {
		dataMap["expire_at"] = *info.ExpireAt
	} else {
		dataMap["expire_at"] = nil
	}
	if info.AccessToken != "" {
		dataMap["access_token"] = info.AccessToken
	}
	if info.RefreshToken != "" {
		dataMap["refresh_token"] = info.RefreshToken
	}
	if info.TokenType != "" {
		dataMap["token_type"] = info.TokenType
	}
	if info.ExpiresIn > 0 {
		dataMap["expires_in"] = info.ExpiresIn
	}
	if info.RefreshExpiresIn > 0 {
		dataMap["refresh_expires_in"] = info.RefreshExpiresIn
	}
	if info.AccessExpiresAt > 0 {
		dataMap["access_expires_at"] = info.AccessExpiresAt
	}
	if info.RefreshExpiresAt > 0 {
		dataMap["refresh_expires_at"] = info.RefreshExpiresAt
	}
	if info.SessionID != "" {
		dataMap["session_id"] = info.SessionID
	}
	if info.AuthMode != "" {
		dataMap["auth_mode"] = info.AuthMode
	}

	return c.verifyResponseSignature(dataMap, info.Signature)
}

// saveCache 保存缓存
func (c *Client) saveCache() {
	if c.licenseInfo == nil {
		return
	}
	data, err := json.MarshalIndent(c.licenseInfo, "", "  ")
	if err != nil {
		return
	}

	var saveData []byte
	if c.encryptCache {
		saveData, err = c.encrypt(data)
		if err != nil {
			return
		}
	} else {
		saveData = data
	}

	os.WriteFile(c.getCachePath(), saveData, 0600) // 更严格的文件权限
}

// clearCache 清除缓存
func (c *Client) clearCache() {
	os.Remove(c.getCachePath())
	// 也清除旧格式缓存
	os.Remove(filepath.Join(c.cacheDir, c.appKey+".json"))
	c.licenseInfo = nil
}

// request 发送 HTTP 请求
func (c *Client) request(method, endpoint string, data interface{}) (map[string]interface{}, error) {
	return c.requestWithHeaders(method, endpoint, data, nil)
}

func (c *Client) requestWithHeaders(method, endpoint string, data interface{}, headers map[string]string) (map[string]interface{}, error) {
	url := c.serverURL + "/api/client" + endpoint

	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if k != "" && v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API error: %s", result.Message)
	}

	return result.Data, nil
}

// Activate 使用授权码激活
func (c *Client) Activate(licenseKey string) (*LicenseInfo, error) {
	data := map[string]interface{}{
		"app_key":     c.appKey,
		"license_key": licenseKey,
		"machine_id":  c.machineID,
		"device_info": c.getDeviceInfo(),
	}

	result, err := c.request("POST", "/auth/activate", data)
	if err != nil {
		return nil, err
	}

	// 使用带签名验证的解析
	info, err := c.parseResultWithVerification(result)
	if err != nil {
		return nil, fmt.Errorf("激活响应验证失败: %w", err)
	}

	info.LicenseKey = licenseKey
	info.LastVerifiedAt = time.Now().Unix()

	c.mu.Lock()
	c.licenseInfo = info
	c.mu.Unlock()
	c.saveCache()
	c.startHeartbeat()

	return info, nil
}

// hashPassword 客户端预哈希密码
// 使用 SHA256(password + email) 作为预哈希
// 这样即使 HTTPS 被破解，攻击者也无法获得原始密码
func (c *Client) hashPassword(password, email string) string {
	// 使用 email 作为盐值，防止彩虹表攻击
	salted := fmt.Sprintf("%s:%s:license_salt_v1", password, strings.ToLower(email))
	hash := sha256.Sum256([]byte(salted))
	return hex.EncodeToString(hash[:])
}

// Login 使用账号密码登录
func (c *Client) Login(email, password string) (*LicenseInfo, error) {
	// 客户端预哈希密码，防止明文传输
	hashedPassword := c.hashPassword(password, email)

	data := map[string]interface{}{
		"app_key":         c.appKey,
		"email":           email,
		"password":        hashedPassword,
		"password_hashed": true, // 标记密码已预哈希
		"machine_id":      c.machineID,
		"device_info":     c.getDeviceInfo(),
	}

	result, err := c.request("POST", "/auth/login", data)
	if err != nil {
		return nil, err
	}

	// 使用带签名验证的解析
	info, err := c.parseResultWithVerification(result)
	if err != nil {
		return nil, fmt.Errorf("登录响应验证失败: %w", err)
	}

	info.Email = email
	info.LastVerifiedAt = time.Now().Unix()

	c.mu.Lock()
	c.licenseInfo = info
	c.mu.Unlock()
	c.saveCache()
	c.startHeartbeat()

	return info, nil
}

// Register 注册新用户
func (c *Client) Register(email, password, name string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"app_key":         c.appKey,
		"email":           email,
		"password":        password,
		"password_hashed": false,
		"name":            name,
	}
	return c.request("POST", "/auth/register", data)
}

// ChangePassword 修改密码
func (c *Client) ChangePassword(oldPassword, newPassword, email string) (map[string]interface{}, error) {
	userEmail := email
	if userEmail == "" {
		c.mu.RLock()
		if c.licenseInfo != nil {
			userEmail = c.licenseInfo.Email
		}
		c.mu.RUnlock()
	}

	if userEmail == "" {
		return nil, fmt.Errorf("需要提供邮箱")
	}

	data := map[string]interface{}{
		"old_password":    oldPassword,
		"new_password":    newPassword,
		"password_hashed": false,
	}
	return c.requestWithClientSession("PUT", "/auth/password", data)
}

// Verify 验证授权状态
func (c *Client) Verify() bool {
	data := map[string]interface{}{
		"app_key":    c.appKey,
		"machine_id": c.machineID,
	}

	result, err := c.request("POST", "/auth/verify", data)
	if err != nil {
		return false
	}

	// 验证签名
	signature, _ := result["signature"].(string)
	if err := c.verifyResponseSignature(result, signature); err != nil {
		return false // 签名验证失败，拒绝响应
	}

	c.mu.Lock()
	if c.licenseInfo != nil {
		c.licenseInfo.LastVerifiedAt = time.Now().Unix()
		if valid, ok := result["valid"].(bool); ok {
			c.licenseInfo.Valid = valid
		}
	}
	c.mu.Unlock()
	c.saveCache()

	if valid, ok := result["valid"].(bool); ok {
		return valid
	}
	return false
}

// Heartbeat 发送心跳
func (c *Client) Heartbeat() bool {
	data := map[string]interface{}{
		"app_key":     c.appKey,
		"machine_id":  c.machineID,
		"app_version": c.appVersion,
	}

	result, err := c.request("POST", "/auth/heartbeat", data)
	if err != nil {
		return false
	}

	// 验证签名
	signature, _ := result["signature"].(string)
	if err := c.verifyResponseSignature(result, signature); err != nil {
		return false // 签名验证失败，拒绝响应
	}

	c.mu.Lock()
	if c.licenseInfo != nil {
		c.licenseInfo.LastVerifiedAt = time.Now().Unix()
	}
	c.mu.Unlock()
	c.saveCache()

	if valid, ok := result["valid"].(bool); ok {
		return valid
	}
	return false
}

// SubscriptionHeartbeat 订阅模式心跳
func (c *Client) SubscriptionHeartbeat() bool {
	data := map[string]interface{}{
		"app_key":     c.appKey,
		"machine_id":  c.machineID,
		"app_version": c.appVersion,
	}

	result, err := c.request("POST", "/subscription/heartbeat", data)
	if err != nil {
		return false
	}

	signature, _ := result["signature"].(string)
	if err := c.verifyResponseSignature(result, signature); err != nil {
		return false
	}

	c.mu.Lock()
	if c.licenseInfo != nil {
		c.licenseInfo.LastVerifiedAt = time.Now().Unix()
	}
	c.mu.Unlock()
	c.saveCache()

	if valid, ok := result["valid"].(bool); ok {
		return valid
	}
	return false
}

// SubscriptionVerify 订阅模式验证
func (c *Client) SubscriptionVerify() bool {
	data := map[string]interface{}{
		"app_key":    c.appKey,
		"machine_id": c.machineID,
	}

	result, err := c.request("POST", "/subscription/verify", data)
	if err != nil {
		return false
	}

	// 验证签名
	signature, _ := result["signature"].(string)
	if err := c.verifyResponseSignature(result, signature); err != nil {
		return false // 签名验证失败，拒绝响应
	}

	c.mu.Lock()
	if c.licenseInfo != nil {
		c.licenseInfo.LastVerifiedAt = time.Now().Unix()
		if valid, ok := result["valid"].(bool); ok {
			c.licenseInfo.Valid = valid
		}
	}
	c.mu.Unlock()
	c.saveCache()

	if valid, ok := result["valid"].(bool); ok {
		return valid
	}
	return false
}

func (c *Client) getSessionSnapshot() (accessToken, refreshToken, authMode, email string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.licenseInfo == nil {
		return "", "", "", ""
	}
	return c.licenseInfo.AccessToken, c.licenseInfo.RefreshToken, c.licenseInfo.AuthMode, c.licenseInfo.Email
}

func (c *Client) isAccessTokenExpiredSoon(skew time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.licenseInfo == nil || c.licenseInfo.AccessToken == "" {
		return true
	}
	return c.licenseInfo.AccessExpiresAt > 0 && time.Now().Add(skew).Unix() >= c.licenseInfo.AccessExpiresAt
}

func (c *Client) getValidAccessToken() (string, error) {
	accessToken, _, _, _ := c.getSessionSnapshot()
	if accessToken == "" || c.isAccessTokenExpiredSoon(clientTokenRefreshSkew) {
		if !c.refreshClientSession() {
			return "", fmt.Errorf("刷新客户端会话失败")
		}
		accessToken, _, _, _ = c.getSessionSnapshot()
	}
	if accessToken == "" {
		return "", fmt.Errorf("缺少客户端访问令牌，请先激活或登录")
	}
	return accessToken, nil
}

func (c *Client) refreshClientSession() bool {
	_, refreshToken, _, _ := c.getSessionSnapshot()
	if refreshToken == "" {
		return false
	}

	result, err := c.request("POST", "/auth/refresh", map[string]interface{}{
		"refresh_token": refreshToken,
	})
	if err != nil {
		return false
	}

	accessToken, _ := result["access_token"].(string)
	if accessToken == "" {
		return false
	}

	c.mu.Lock()
	if c.licenseInfo == nil {
		c.mu.Unlock()
		return false
	}
	c.licenseInfo.AccessToken = accessToken
	if v, ok := result["refresh_token"].(string); ok && v != "" {
		c.licenseInfo.RefreshToken = v
	}
	if v, ok := result["token_type"].(string); ok && v != "" {
		c.licenseInfo.TokenType = v
	}
	if v, ok := result["session_id"].(string); ok && v != "" {
		c.licenseInfo.SessionID = v
	}
	if v, ok := result["auth_mode"].(string); ok && v != "" {
		c.licenseInfo.AuthMode = v
	}
	if v, ok := result["expires_in"].(float64); ok {
		c.licenseInfo.ExpiresIn = int(v)
	}
	if v, ok := result["refresh_expires_in"].(float64); ok {
		c.licenseInfo.RefreshExpiresIn = int(v)
	}
	if v, ok := result["access_expires_at"].(float64); ok {
		c.licenseInfo.AccessExpiresAt = int64(v)
	}
	if v, ok := result["refresh_expires_at"].(float64); ok {
		c.licenseInfo.RefreshExpiresAt = int64(v)
	}
	c.mu.Unlock()

	c.saveCache()
	return true
}

func shouldRetryWithRefresh(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "客户端令牌") ||
		strings.Contains(msg, "会话") ||
		strings.Contains(msg, "认证")
}

func (c *Client) requestWithClientSession(method, endpoint string, data interface{}) (map[string]interface{}, error) {
	doRequest := func() (map[string]interface{}, error) {
		accessToken, _, _, _ := c.getSessionSnapshot()
		if accessToken == "" {
			return nil, fmt.Errorf("请先激活或登录")
		}
		return c.requestWithHeaders(method, endpoint, data, map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
	}

	result, err := doRequest()
	if err != nil && shouldRetryWithRefresh(err) && c.refreshClientSession() {
		return doRequest()
	}
	return result, err
}

func (c *Client) requestRawWithClientSession(method, rawURL string, data interface{}) (*http.Response, error) {
	doRequest := func() (*http.Response, error) {
		accessToken, err := c.getValidAccessToken()
		if err != nil {
			return nil, err
		}

		var body io.Reader
		if data != nil {
			jsonData, err := json.Marshal(data)
			if err != nil {
				return nil, err
			}
			body = bytes.NewBuffer(jsonData)
		}

		req, err := http.NewRequest(method, rawURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		if data != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return c.httpClient.Do(req)
	}

	resp, err := doRequest()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if c.refreshClientSession() {
			return doRequest()
		}
		return nil, fmt.Errorf("API error: 认证失败")
	}
	return resp, nil
}

// Deactivate 解绑设备。
// 订阅模式请传入密码：Deactivate("your_password")。
func (c *Client) Deactivate(password ...string) bool {
	accessToken, _, authMode, email := c.getSessionSnapshot()
	if accessToken != "" {
		mode := strings.ToLower(strings.TrimSpace(authMode))
		body := map[string]interface{}{}
		if mode == "subscription" {
			pass := ""
			if len(password) > 0 {
				pass = strings.TrimSpace(password[0])
			}
			if pass == "" {
				return false
			}
			if email != "" {
				body["password"] = c.hashPassword(pass, email)
				body["password_hashed"] = true
			} else {
				body["password"] = pass
			}
		}

		headers := map[string]string{"Authorization": "Bearer " + accessToken}
		_, err := c.requestWithHeaders("DELETE", "/devices/self", body, headers)
		if err != nil && shouldRetryWithRefresh(err) && c.refreshClientSession() {
			accessToken, _, _, _ = c.getSessionSnapshot()
			if accessToken != "" {
				headers["Authorization"] = "Bearer " + accessToken
				_, err = c.requestWithHeaders("DELETE", "/devices/self", body, headers)
			}
		}
		if err == nil {
			c.clearCache()
			c.StopHeartbeat()
			return true
		}
		if mode == "subscription" {
			return false
		}
	}

	data := map[string]interface{}{
		"app_key":    c.appKey,
		"machine_id": c.machineID,
	}

	_, err := c.request("POST", "/auth/deactivate", data)
	if err != nil {
		return false
	}

	c.clearCache()
	c.StopHeartbeat()
	return true
}

// IsValid 检查授权是否有效（支持离线）
// 使用多点验证增加破解难度
func (c *Client) IsValid() bool {
	// 验证点1: 基础检查
	if !c.checkBasicValidity() {
		return false
	}

	// 验证点2: 过期时间检查
	if !c.checkExpiration() {
		return false
	}

	// 验证点3: 离线宽限期检查
	if !c.checkOfflineGrace() {
		return false
	}

	// 验证点4: 签名完整性检查（如果启用）
	if !c.checkSignatureIntegrity() {
		return false
	}

	return true
}

// checkBasicValidity 基础有效性检查
func (c *Client) checkBasicValidity() bool {
	c.mu.RLock()
	info := c.licenseInfo
	c.mu.RUnlock()

	if info == nil {
		return false
	}

	// 分散检查 Valid 字段
	validFlag := info.Valid
	return validFlag
}

// checkExpiration 过期时间检查
func (c *Client) checkExpiration() bool {
	c.mu.RLock()
	info := c.licenseInfo
	c.mu.RUnlock()

	if info == nil {
		return false
	}

	// 检查过期时间
	if info.ExpireAt != nil && *info.ExpireAt != "" {
		expireTime, err := time.Parse(time.RFC3339, *info.ExpireAt)
		if err == nil && time.Now().After(expireTime) {
			return false
		}
	}

	return true
}

// checkOfflineGrace 离线宽限期检查
func (c *Client) checkOfflineGrace() bool {
	c.mu.RLock()
	info := c.licenseInfo
	c.mu.RUnlock()

	if info == nil {
		return false
	}

	// 检查离线宽限期
	offlineDays := float64(time.Now().Unix()-info.LastVerifiedAt) / 86400
	if offlineDays > float64(c.offlineGraceDays) {
		return c.Verify()
	}

	return true
}

// checkSignatureIntegrity 签名完整性检查
func (c *Client) checkSignatureIntegrity() bool {
	// 如果没有启用签名验证，跳过
	if !c.requireSignature || c.publicKeyPEM == "" {
		return true
	}

	c.mu.RLock()
	info := c.licenseInfo
	c.mu.RUnlock()

	if info == nil {
		return false
	}

	// 验证缓存的签名
	if info.Signature == "" {
		return false
	}

	if err := c.verifyCachedLicenseSignature(info); err != nil {
		return false
	}

	return true
}

// IsValidStrict 严格模式验证（每次都联网验证）
func (c *Client) IsValidStrict() bool {
	// 先进行本地检查
	if !c.checkBasicValidity() {
		return false
	}

	// 强制联网验证
	return c.Verify()
}

// GetFeatures 获取功能权限列表
func (c *Client) GetFeatures() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.licenseInfo == nil {
		return nil
	}
	return c.licenseInfo.Features
}

// HasFeature 检查是否有某个功能权限
func (c *Client) HasFeature(feature string) bool {
	features := c.GetFeatures()
	for _, f := range features {
		if f == feature {
			return true
		}
	}
	return false
}

// GetRemainingDays 获取剩余天数
func (c *Client) GetRemainingDays() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.licenseInfo == nil {
		return 0
	}
	return c.licenseInfo.RemainingDays
}

// GetLicenseInfo 获取完整的授权信息
func (c *Client) GetLicenseInfo() *LicenseInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.licenseInfo
}

// CheckUpdate 检查版本更新
func (c *Client) CheckUpdate() (*UpdateInfo, error) {
	result, err := c.requestWithClientSession(http.MethodGet, "/releases/latest", nil)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("序列化响应失败: %w", err)
	}

	var update UpdateInfo
	if err := json.Unmarshal(data, &update); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return &update, nil
}

// startHeartbeat 启动心跳
func (c *Client) startHeartbeat() {
	c.heartbeatOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(c.heartbeatInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					_, _, authMode, _ := c.getSessionSnapshot()
					if strings.ToLower(strings.TrimSpace(authMode)) == "subscription" {
						c.SubscriptionHeartbeat()
					} else {
						c.Heartbeat()
					}
				case <-c.stopHeartbeat:
					return
				}
			}
		}()
	})
}

// StopHeartbeat 停止心跳
func (c *Client) StopHeartbeat() {
	select {
	case c.stopHeartbeat <- struct{}{}:
	default:
	}
}

// parseResult 解析结果
func (c *Client) parseResult(result map[string]interface{}) *LicenseInfo {
	info := &LicenseInfo{}

	if v, ok := result["valid"].(bool); ok {
		info.Valid = v
	}
	if v, ok := result["license_id"].(string); ok {
		info.LicenseID = v
	}
	if v, ok := result["subscription_id"].(string); ok {
		info.SubscriptionID = v
	}
	if v, ok := result["customer_id"].(string); ok {
		info.CustomerID = v
	}
	if v, ok := result["device_id"].(string); ok {
		info.DeviceID = v
	}
	if v, ok := result["type"].(string); ok {
		info.Type = v
	}
	if v, ok := result["plan_type"].(string); ok {
		info.PlanType = v
	}
	if raw, exists := result["expire_at"]; exists {
		if raw == nil {
			info.ExpireAt = nil
		} else if v, ok := raw.(string); ok {
			info.ExpireAt = &v
		}
	}
	if v, ok := result["remaining_days"].(float64); ok {
		info.RemainingDays = int(v)
	}
	if v, ok := result["features"].([]interface{}); ok {
		for _, f := range v {
			if s, ok := f.(string); ok {
				info.Features = append(info.Features, s)
			}
		}
	}
	if v, ok := result["signature"].(string); ok {
		info.Signature = v
	}
	if v, ok := result["access_token"].(string); ok {
		info.AccessToken = v
	}
	if v, ok := result["refresh_token"].(string); ok {
		info.RefreshToken = v
	}
	if v, ok := result["token_type"].(string); ok {
		info.TokenType = v
	}
	if v, ok := result["session_id"].(string); ok {
		info.SessionID = v
	}
	if v, ok := result["auth_mode"].(string); ok {
		info.AuthMode = v
	}
	if v, ok := result["expires_in"].(float64); ok {
		info.ExpiresIn = int(v)
	}
	if v, ok := result["refresh_expires_in"].(float64); ok {
		info.RefreshExpiresIn = int(v)
	}
	if v, ok := result["access_expires_at"].(float64); ok {
		info.AccessExpiresAt = int64(v)
	}
	if v, ok := result["refresh_expires_at"].(float64); ok {
		info.RefreshExpiresAt = int64(v)
	}

	return info
}

// Close 关闭客户端
func (c *Client) Close() {
	c.StopHeartbeat()
}

// ==================== 签名验证相关方法 ====================

// verifyResponseSignature 验证服务端响应签名
// 这是防止数据篡改的核心安全机制
func (c *Client) verifyResponseSignature(data map[string]interface{}, signature string) error {
	// 如果没有配置公钥
	if c.publicKeyPEM == "" {
		if c.requireSignature {
			return ErrInvalidPublicKey
		}
		return nil // 未配置公钥且不强制验证，跳过
	}

	// 如果没有签名
	if signature == "" {
		if c.requireSignature {
			return ErrSignatureMissing
		}
		return nil // 没有签名且不强制验证，跳过
	}

	// 检查时间戳（防止重放攻击）
	if c.signatureTimeWindow > 0 {
		if ts, ok := data["timestamp"].(float64); ok {
			serverTime := int64(ts)
			currentTime := time.Now().Unix()
			if currentTime-serverTime > c.signatureTimeWindow || serverTime-currentTime > c.signatureTimeWindow {
				return ErrSignatureExpired
			}
		}
	}

	// 构建待验证的数据（排除签名字段）
	dataToVerify := make(map[string]interface{})
	for k, v := range data {
		if k != "signature" {
			dataToVerify[k] = v
		}
	}

	// 序列化数据（按键排序以确保一致性）
	dataBytes, err := c.canonicalJSON(dataToVerify)
	if err != nil {
		return fmt.Errorf("序列化数据失败: %w", err)
	}

	// 验证签名
	return c.verifySignature(dataBytes, signature)
}

// canonicalJSON 生成规范化的 JSON（键按字母排序）
func (c *Client) canonicalJSON(data map[string]interface{}) ([]byte, error) {
	// 获取所有键并排序
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 按排序后的键构建有序 map
	orderedData := make(map[string]interface{})
	for _, k := range keys {
		orderedData[k] = data[k]
	}

	return json.Marshal(orderedData)
}

// verifySignature 使用公钥验证签名
func (c *Client) verifySignature(data []byte, signatureBase64 string) error {
	// 解析公钥
	block, _ := pem.Decode([]byte(c.publicKeyPEM))
	if block == nil {
		return ErrInvalidPublicKey
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return ErrInvalidPublicKey
	}

	// 解码签名
	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return fmt.Errorf("签名解码失败: %w", err)
	}

	// 计算数据哈希
	hashed := sha256.Sum256(data)

	// 验证签名
	if err := rsa.VerifyPKCS1v15(rsaPubKey, crypto.SHA256, hashed[:], signature); err != nil {
		return ErrSignatureVerification
	}

	return nil
}

// parseResultWithVerification 解析结果并验证签名
func (c *Client) parseResultWithVerification(result map[string]interface{}) (*LicenseInfo, error) {
	// 获取签名
	signature, _ := result["signature"].(string)

	// 验证签名
	if err := c.verifyResponseSignature(result, signature); err != nil {
		return nil, err
	}

	// 解析数据
	return c.parseResult(result), nil
}

// IsSignatureEnabled 检查是否启用了签名验证
func (c *Client) IsSignatureEnabled() bool {
	return c.publicKeyPEM != "" && c.requireSignature
}

// SetPublicKey 动态设置公钥（用于从服务器获取公钥的场景）
func (c *Client) SetPublicKey(publicKeyPEM string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publicKeyPEM = publicKeyPEM
	c.requireSignature = true
}
