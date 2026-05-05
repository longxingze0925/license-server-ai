// Package license 高级安全模块 - 提供增强的反破解防护
//
// 功能：
// 1. 运行时完整性校验 - 检测内存修改和 Hook
// 2. 验证结果混淆 - 不直接返回 bool
// 3. 随机验证间隔 - 增加破解难度
// 4. 服务端挑战-响应 - 动态验证
// 5. 蜜罐检测 - 检测异常调用模式
// 6. 调用频率监控 - 检测自动化破解

package license

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ==================== 运行时完整性校验 ====================

// RuntimeIntegrityChecker 运行时完整性校验器
// 检测关键函数是否被 Hook 或内存修改
type RuntimeIntegrityChecker struct {
	functionHashes  map[string]uint64
	functionPtrs    map[string]uintptr // 保存函数指针用于后续验证
	checkInterval   time.Duration
	stopChan        chan struct{}
	onViolation     func(reason string)
	mu              sync.RWMutex
	enabled         bool
	lastCheckResult bool
	running         bool
}

// NewRuntimeIntegrityChecker 创建运行时完整性校验器
func NewRuntimeIntegrityChecker() *RuntimeIntegrityChecker {
	ric := &RuntimeIntegrityChecker{
		functionHashes:  make(map[string]uint64),
		functionPtrs:    make(map[string]uintptr),
		checkInterval:   30 * time.Second,
		enabled:         true,
		lastCheckResult: true,
	}
	return ric
}

// RegisterFunction 注册需要监控的函数
// 记录函数入口点的特征值
func (ric *RuntimeIntegrityChecker) RegisterFunction(name string, fn interface{}) {
	ric.mu.Lock()
	defer ric.mu.Unlock()

	// 获取函数指针
	ptr := reflect.ValueOf(fn).Pointer()

	// 保存函数指针
	ric.functionPtrs[name] = ptr

	// 计算函数入口点的特征哈希（前64字节）
	hash := ric.calculateFunctionHash(ptr)
	ric.functionHashes[name] = hash
}

// calculateFunctionHash 计算函数指纹。
func (ric *RuntimeIntegrityChecker) calculateFunctionHash(ptr uintptr) uint64 {
	return runtimeFunctionHash(ptr)
}

// CheckIntegrity 检查所有注册函数的完整性
func (ric *RuntimeIntegrityChecker) CheckIntegrity() bool {
	if !ric.enabled {
		return true
	}

	ric.mu.RLock()
	defer ric.mu.RUnlock()

	for name, expectedHash := range ric.functionHashes {
		ptr, exists := ric.functionPtrs[name]
		if !exists {
			ric.lastCheckResult = false
			return false
		}

		// 重新计算哈希
		currentHash := ric.calculateFunctionHash(ptr)
		if currentHash != expectedHash {
			ric.lastCheckResult = false
			if ric.onViolation != nil {
				ric.onViolation(fmt.Sprintf("function_modified:%s", name))
			}
			return false
		}

		// 检测常见 Hook 模式
		if ric.DetectCommonHooks(ptr) {
			ric.lastCheckResult = false
			if ric.onViolation != nil {
				ric.onViolation(fmt.Sprintf("hook_detected:%s", name))
			}
			return false
		}
	}

	ric.lastCheckResult = true
	return true
}

// DetectCommonHooks 检测常见的 Hook 特征
func (ric *RuntimeIntegrityChecker) DetectCommonHooks(ptr uintptr) bool {
	return detectCommonHooks(ptr)
}

// StartPeriodicCheck 启动定期检查
func (ric *RuntimeIntegrityChecker) StartPeriodicCheck() {
	ric.mu.Lock()
	if ric.running {
		ric.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	interval := ric.checkInterval
	ric.stopChan = stopCh
	ric.running = true
	ric.mu.Unlock()

	go func() {
		defer func() {
			ric.mu.Lock()
			if ric.stopChan == stopCh {
				ric.running = false
				ric.stopChan = nil
			}
			ric.mu.Unlock()
		}()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !ric.CheckIntegrity() {
					if ric.onViolation != nil {
						ric.onViolation("runtime_integrity_violation")
					}
				}
			case <-stopCh:
				return
			}
		}
	}()
}

// Stop 停止检查
func (ric *RuntimeIntegrityChecker) Stop() {
	ric.mu.Lock()
	defer ric.mu.Unlock()
	if !ric.running || ric.stopChan == nil {
		return
	}
	close(ric.stopChan)
	ric.running = false
	ric.stopChan = nil
}

// SetViolationHandler 设置违规处理函数
func (ric *RuntimeIntegrityChecker) SetViolationHandler(handler func(reason string)) {
	ric.onViolation = handler
}

// ==================== 验证结果混淆 ====================

// ObfuscatedResult 混淆的验证结果
// 不直接使用 bool，增加逆向难度
type ObfuscatedResult struct {
	value     uint64
	timestamp int64
	nonce     uint32
	checksum  uint32
}

// ValidationToken 验证令牌
type ValidationToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Nonce     string `json:"nonce"`
}

// ObfuscatedValidator 混淆验证器
type ObfuscatedValidator struct {
	secretKey  []byte
	validMagic uint64
	mu         sync.RWMutex
}

// NewObfuscatedValidator 创建混淆验证器
func NewObfuscatedValidator(secretKey []byte) *ObfuscatedValidator {
	ov := &ObfuscatedValidator{
		secretKey: secretKey,
	}
	// 使用密钥派生有效魔数
	h := sha256.Sum256(append(secretKey, []byte("valid_magic_v1")...))
	ov.validMagic = binary.LittleEndian.Uint64(h[:8])
	return ov
}

// CreateResult 创建混淆的验证结果
func (ov *ObfuscatedValidator) CreateResult(isValid bool) *ObfuscatedResult {
	result := &ObfuscatedResult{
		timestamp: time.Now().UnixNano(),
		nonce:     secureRandom.Uint32(),
	}

	if isValid {
		// 有效时使用特定的魔数模式
		result.value = ov.validMagic ^ uint64(result.nonce) ^ uint64(result.timestamp&0xFFFFFFFF)
	} else {
		// 无效时使用随机值
		result.value = secureRandom.Uint64()
	}

	// 计算校验和
	result.checksum = ov.calculateChecksum(result)
	return result
}

// VerifyResult 验证混淆结果
func (ov *ObfuscatedValidator) VerifyResult(result *ObfuscatedResult) bool {
	if result == nil {
		return false
	}

	// 验证校验和
	if result.checksum != ov.calculateChecksum(result) {
		return false
	}

	// 验证时间戳（5分钟内有效）
	if time.Now().UnixNano()-result.timestamp > 5*60*1e9 {
		return false
	}

	// 验证魔数
	expectedMagic := ov.validMagic ^ uint64(result.nonce) ^ uint64(result.timestamp&0xFFFFFFFF)
	return result.value == expectedMagic
}

// calculateChecksum 计算校验和
func (ov *ObfuscatedValidator) calculateChecksum(result *ObfuscatedResult) uint32 {
	data := make([]byte, 20)
	binary.LittleEndian.PutUint64(data[0:8], result.value)
	binary.LittleEndian.PutUint64(data[8:16], uint64(result.timestamp))
	binary.LittleEndian.PutUint32(data[16:20], result.nonce)

	h := hmac.New(sha256.New, ov.secretKey)
	h.Write(data)
	sum := h.Sum(nil)
	return binary.LittleEndian.Uint32(sum[:4])
}

// CreateToken 创建验证令牌
func (ov *ObfuscatedValidator) CreateToken(isValid bool, ttlSeconds int64) *ValidationToken {
	nonce := secureRandom.Bytes(16)

	token := &ValidationToken{
		ExpiresAt: time.Now().Unix() + ttlSeconds,
		Nonce:     hex.EncodeToString(nonce),
	}

	// 生成令牌
	data := fmt.Sprintf("%v:%d:%s", isValid, token.ExpiresAt, token.Nonce)
	h := hmac.New(sha256.New, ov.secretKey)
	h.Write([]byte(data))
	token.Token = hex.EncodeToString(h.Sum(nil))

	return token
}

// VerifyToken 验证令牌
func (ov *ObfuscatedValidator) VerifyToken(token *ValidationToken, expectedValid bool) bool {
	if token == nil {
		return false
	}

	// 检查过期
	if time.Now().Unix() > token.ExpiresAt {
		return false
	}

	// 验证令牌
	data := fmt.Sprintf("%v:%d:%s", expectedValid, token.ExpiresAt, token.Nonce)
	h := hmac.New(sha256.New, ov.secretKey)
	h.Write([]byte(data))
	expectedToken := hex.EncodeToString(h.Sum(nil))

	return token.Token == expectedToken
}

// ==================== 随机验证间隔 ====================

// RandomVerificationScheduler 随机验证调度器
type RandomVerificationScheduler struct {
	minInterval  time.Duration
	maxInterval  time.Duration
	verifyFunc   func() bool
	onFailure    func()
	stopChan     chan struct{}
	lastVerify   int64
	verifyCount  int64
	failureCount int64
	mu           sync.Mutex
	running      bool
}

// NewRandomVerificationScheduler 创建随机验证调度器
func NewRandomVerificationScheduler(minInterval, maxInterval time.Duration) *RandomVerificationScheduler {
	return &RandomVerificationScheduler{
		minInterval: minInterval,
		maxInterval: maxInterval,
	}
}

// SetVerifyFunc 设置验证函数
func (rvs *RandomVerificationScheduler) SetVerifyFunc(fn func() bool) {
	rvs.verifyFunc = fn
}

// SetFailureHandler 设置失败处理函数
func (rvs *RandomVerificationScheduler) SetFailureHandler(fn func()) {
	rvs.onFailure = fn
}

// Start 启动随机验证
func (rvs *RandomVerificationScheduler) Start() {
	rvs.mu.Lock()
	if rvs.running {
		rvs.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	minInterval := rvs.minInterval
	maxInterval := rvs.maxInterval
	rvs.stopChan = stopCh
	rvs.running = true
	rvs.mu.Unlock()

	go func() {
		defer func() {
			rvs.mu.Lock()
			if rvs.stopChan == stopCh {
				rvs.running = false
				rvs.stopChan = nil
			}
			rvs.mu.Unlock()
		}()

		for {
			// 使用安全随机数计算间隔
			interval := secureRandom.Duration(minInterval, maxInterval)
			timer := time.NewTimer(interval)

			select {
			case <-timer.C:
				rvs.doVerify()
			case <-stopCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
		}
	}()
}

// doVerify 执行验证
func (rvs *RandomVerificationScheduler) doVerify() {
	rvs.mu.Lock()
	rvs.lastVerify = time.Now().Unix()
	atomic.AddInt64(&rvs.verifyCount, 1)
	rvs.mu.Unlock()

	if rvs.verifyFunc != nil && !rvs.verifyFunc() {
		atomic.AddInt64(&rvs.failureCount, 1)
		if rvs.onFailure != nil {
			rvs.onFailure()
		}
	}
}

// Stop 停止调度器
func (rvs *RandomVerificationScheduler) Stop() {
	rvs.mu.Lock()
	defer rvs.mu.Unlock()
	if !rvs.running || rvs.stopChan == nil {
		return
	}
	close(rvs.stopChan)
	rvs.running = false
	rvs.stopChan = nil
}

// GetStats 获取统计信息
func (rvs *RandomVerificationScheduler) GetStats() (verifyCount, failureCount int64) {
	return atomic.LoadInt64(&rvs.verifyCount), atomic.LoadInt64(&rvs.failureCount)
}

// ==================== 蜜罐检测 ====================

// HoneypotDetector 蜜罐检测器
// 检测异常的调用模式，识别自动化破解尝试
type HoneypotDetector struct {
	// 调用计数器
	callCounts map[string]*callStats
	// 蜜罐函数被调用的标记
	honeypotTriggered bool
	// 异常模式检测
	suspiciousPatterns int32
	mu                 sync.RWMutex
	onDetection        func(reason string)
}

type callStats struct {
	count       int64
	lastCall    int64
	avgInterval float64
}

// NewHoneypotDetector 创建蜜罐检测器
func NewHoneypotDetector() *HoneypotDetector {
	return &HoneypotDetector{
		callCounts: make(map[string]*callStats),
	}
}

// RecordCall 记录函数调用
func (hd *HoneypotDetector) RecordCall(funcName string) {
	hd.mu.Lock()
	defer hd.mu.Unlock()

	now := time.Now().UnixNano()
	stats, exists := hd.callCounts[funcName]
	if !exists {
		stats = &callStats{}
		hd.callCounts[funcName] = stats
	}

	// 更新统计
	if stats.lastCall > 0 {
		interval := float64(now - stats.lastCall)
		if stats.avgInterval == 0 {
			stats.avgInterval = interval
		} else {
			stats.avgInterval = stats.avgInterval*0.9 + interval*0.1
		}

		// 检测异常快速调用（可能是自动化工具）
		if interval < float64(time.Millisecond*10) {
			atomic.AddInt32(&hd.suspiciousPatterns, 1)
		}
	}

	stats.count++
	stats.lastCall = now
}

// TriggerHoneypot 触发蜜罐（用于检测逆向分析）
// 这个函数看起来像是有用的函数，但实际上是陷阱
func (hd *HoneypotDetector) TriggerHoneypot() {
	hd.mu.Lock()
	hd.honeypotTriggered = true
	hd.mu.Unlock()

	if hd.onDetection != nil {
		hd.onDetection("honeypot_triggered")
	}
}

// IsCompromised 检查是否检测到异常
func (hd *HoneypotDetector) IsCompromised() bool {
	hd.mu.RLock()
	defer hd.mu.RUnlock()

	// 蜜罐被触发
	if hd.honeypotTriggered {
		return true
	}

	// 可疑模式过多
	if atomic.LoadInt32(&hd.suspiciousPatterns) > 10 {
		return true
	}

	return false
}

// SetDetectionHandler 设置检测处理函数
func (hd *HoneypotDetector) SetDetectionHandler(handler func(reason string)) {
	hd.onDetection = handler
}

// CheckCallFrequency 检查调用频率是否异常
func (hd *HoneypotDetector) CheckCallFrequency(funcName string, maxCallsPerSecond float64) bool {
	hd.mu.RLock()
	defer hd.mu.RUnlock()

	stats, exists := hd.callCounts[funcName]
	if !exists {
		return true
	}

	// 计算每秒调用次数
	if stats.avgInterval > 0 {
		callsPerSecond := float64(time.Second) / stats.avgInterval
		if callsPerSecond > maxCallsPerSecond {
			return false // 调用频率过高
		}
	}

	return true
}

// ==================== 服务端挑战-响应 ====================

// ChallengeResponse 挑战-响应结构
type ChallengeResponse struct {
	ChallengeID string `json:"challenge_id"`
	Challenge   string `json:"challenge"`
	Algorithm   string `json:"algorithm"`
	Difficulty  int    `json:"difficulty"`
	ExpiresAt   int64  `json:"expires_at"`
}

// ChallengeAnswer 挑战答案
type ChallengeAnswer struct {
	ChallengeID string `json:"challenge_id"`
	Answer      string `json:"answer"`
	Nonce       string `json:"nonce"`
	Timestamp   int64  `json:"timestamp"`
}

// ChallengeSolver 挑战求解器
type ChallengeSolver struct {
	secretKey []byte
}

// NewChallengeSolver 创建挑战求解器
func NewChallengeSolver(secretKey []byte) *ChallengeSolver {
	return &ChallengeSolver{
		secretKey: secretKey,
	}
}

// SolveChallenge 求解挑战
func (cs *ChallengeSolver) SolveChallenge(challenge *ChallengeResponse) (*ChallengeAnswer, error) {
	if challenge == nil {
		return nil, fmt.Errorf("challenge is nil")
	}

	// 检查是否过期
	if time.Now().Unix() > challenge.ExpiresAt {
		return nil, fmt.Errorf("challenge expired")
	}

	answer := &ChallengeAnswer{
		ChallengeID: challenge.ChallengeID,
		Timestamp:   time.Now().Unix(),
	}

	// 使用安全随机数生成 nonce
	answer.Nonce = secureRandom.Hex(16)

	// 根据算法求解
	switch challenge.Algorithm {
	case "hmac-sha256":
		answer.Answer = cs.solveHMAC(challenge.Challenge, answer.Nonce)
	case "hash-prefix":
		answer.Answer = cs.solveHashPrefix(challenge.Challenge, challenge.Difficulty)
	default:
		answer.Answer = cs.solveHMAC(challenge.Challenge, answer.Nonce)
	}

	return answer, nil
}

// solveHMAC 使用 HMAC 求解
func (cs *ChallengeSolver) solveHMAC(challenge, nonce string) string {
	data := challenge + ":" + nonce
	h := hmac.New(sha256.New, cs.secretKey)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// solveHashPrefix 求解哈希前缀挑战（类似工作量证明）
func (cs *ChallengeSolver) solveHashPrefix(challenge string, difficulty int) string {
	prefix := ""
	for i := 0; i < difficulty; i++ {
		prefix += "0"
	}

	nonce := 0
	for {
		data := fmt.Sprintf("%s:%d", challenge, nonce)
		hash := sha256.Sum256([]byte(data))
		hashHex := hex.EncodeToString(hash[:])

		if len(hashHex) >= difficulty && hashHex[:difficulty] == prefix {
			return fmt.Sprintf("%d", nonce)
		}
		nonce++

		// 防止无限循环
		if nonce > 10000000 {
			break
		}
	}
	return ""
}

// ==================== 增强型安全客户端 ====================

// AdvancedSecureClient 增强型安全客户端
type AdvancedSecureClient struct {
	client              *Client
	secureClient        *SecureClient
	runtimeChecker      *RuntimeIntegrityChecker
	obfuscatedValidator *ObfuscatedValidator
	randomScheduler     *RandomVerificationScheduler
	honeypotDetector    *HoneypotDetector
	challengeSolver     *ChallengeSolver

	// 状态
	lastValidResult *ObfuscatedResult
	compromised     int32
	mu              sync.RWMutex
}

// NewAdvancedSecureClient 创建增强型安全客户端
func NewAdvancedSecureClient(client *Client) *AdvancedSecureClient {
	// 派生密钥
	secretKey := sha256.Sum256([]byte(client.GetMachineID() + client.GetAppKey() + "advanced_security_v1"))

	asc := &AdvancedSecureClient{
		client:              client,
		secureClient:        NewSecureClient(client),
		runtimeChecker:      NewRuntimeIntegrityChecker(),
		obfuscatedValidator: NewObfuscatedValidator(secretKey[:]),
		randomScheduler:     NewRandomVerificationScheduler(time.Minute, 5*time.Minute),
		honeypotDetector:    NewHoneypotDetector(),
		challengeSolver:     NewChallengeSolver(secretKey[:]),
	}

	// 设置处理函数
	asc.setupHandlers()

	return asc
}

// setupHandlers 设置各种处理函数
func (asc *AdvancedSecureClient) setupHandlers() {
	// 运行时完整性违规处理
	asc.runtimeChecker.SetViolationHandler(func(reason string) {
		atomic.StoreInt32(&asc.compromised, 1)
		asc.client.clearCache()
	})

	// 蜜罐检测处理
	asc.honeypotDetector.SetDetectionHandler(func(reason string) {
		atomic.StoreInt32(&asc.compromised, 1)
		asc.client.clearCache()
	})

	// 随机验证
	asc.randomScheduler.SetVerifyFunc(func() bool {
		return asc.secureClient.IsValid()
	})
	asc.randomScheduler.SetFailureHandler(func() {
		asc.client.clearCache()
	})
}

// Start 启动高级安全功能
func (asc *AdvancedSecureClient) Start() {
	asc.runtimeChecker.StartPeriodicCheck()
	asc.randomScheduler.Start()
}

// Stop 停止高级安全功能
func (asc *AdvancedSecureClient) Stop() {
	asc.runtimeChecker.Stop()
	asc.randomScheduler.Stop()
}

// IsValid 验证授权（带高级安全检查）
func (asc *AdvancedSecureClient) IsValid() bool {
	// 记录调用
	asc.honeypotDetector.RecordCall("IsValid")

	// 检查是否已被标记为异常
	if atomic.LoadInt32(&asc.compromised) == 1 {
		return false
	}

	// 检查蜜罐
	if asc.honeypotDetector.IsCompromised() {
		return false
	}

	// 检查调用频率
	if !asc.honeypotDetector.CheckCallFrequency("IsValid", 100) {
		atomic.StoreInt32(&asc.compromised, 1)
		return false
	}

	// 执行基础验证
	result := asc.secureClient.IsValid()

	// 创建混淆结果
	asc.mu.Lock()
	asc.lastValidResult = asc.obfuscatedValidator.CreateResult(result)
	asc.mu.Unlock()

	return result
}

// IsValidObfuscated 返回混淆的验证结果
func (asc *AdvancedSecureClient) IsValidObfuscated() *ObfuscatedResult {
	asc.IsValid() // 先执行验证

	asc.mu.RLock()
	defer asc.mu.RUnlock()
	return asc.lastValidResult
}

// VerifyObfuscatedResult 验证混淆结果
func (asc *AdvancedSecureClient) VerifyObfuscatedResult(result *ObfuscatedResult) bool {
	return asc.obfuscatedValidator.VerifyResult(result)
}

// GetValidationToken 获取验证令牌
func (asc *AdvancedSecureClient) GetValidationToken(ttlSeconds int64) *ValidationToken {
	isValid := asc.IsValid()
	return asc.obfuscatedValidator.CreateToken(isValid, ttlSeconds)
}

// VerifyValidationToken 验证令牌
func (asc *AdvancedSecureClient) VerifyValidationToken(token *ValidationToken) bool {
	return asc.obfuscatedValidator.VerifyToken(token, true)
}

// SolveChallenge 求解服务端挑战
func (asc *AdvancedSecureClient) SolveChallenge(challenge *ChallengeResponse) (*ChallengeAnswer, error) {
	return asc.challengeSolver.SolveChallenge(challenge)
}

// HasFeature 检查功能权限
func (asc *AdvancedSecureClient) HasFeature(feature string) bool {
	if !asc.IsValid() {
		return false
	}
	return asc.client.HasFeature(feature)
}

// GetRemainingDays 获取剩余天数
func (asc *AdvancedSecureClient) GetRemainingDays() int {
	if !asc.IsValid() {
		return 0
	}
	return asc.client.GetRemainingDays()
}

// Activate 激活
func (asc *AdvancedSecureClient) Activate(licenseKey string) (*LicenseInfo, error) {
	return asc.client.Activate(licenseKey)
}

// Login 登录
func (asc *AdvancedSecureClient) Login(email, password string) (*LicenseInfo, error) {
	return asc.client.Login(email, password)
}

// Deactivate 解绑
func (asc *AdvancedSecureClient) Deactivate() bool {
	return asc.client.Deactivate()
}

// Close 关闭
func (asc *AdvancedSecureClient) Close() {
	asc.Stop()
	asc.client.Close()
}

// ==================== 蜜罐函数（陷阱）====================
// 这些函数看起来有用，但调用它们会触发安全警报

// GetLicenseKeyInternal 蜜罐函数 - 看起来像是获取内部密钥
// 实际上调用会触发安全警报
func (asc *AdvancedSecureClient) GetLicenseKeyInternal() string {
	asc.honeypotDetector.TriggerHoneypot()
	runtime.GC() // 混淆
	return ""
}

// BypassValidation 蜜罐函数 - 看起来像是绕过验证
// 实际上调用会触发安全警报
func (asc *AdvancedSecureClient) BypassValidation() bool {
	asc.honeypotDetector.TriggerHoneypot()
	return false
}

// SetValidFlag 蜜罐函数 - 看起来像是设置有效标志
// 实际上调用会触发安全警报
func (asc *AdvancedSecureClient) SetValidFlag(valid bool) {
	asc.honeypotDetector.TriggerHoneypot()
}

// UnlockPremium 蜜罐函数 - 看起来像是解锁高级功能
// 实际上调用会触发安全警报
func (asc *AdvancedSecureClient) UnlockPremium() bool {
	asc.honeypotDetector.TriggerHoneypot()
	return false
}

// DisableLicenseCheck 蜜罐函数 - 看起来像是禁用授权检查
// 实际上调用会触发安全警报
func (asc *AdvancedSecureClient) DisableLicenseCheck() {
	asc.honeypotDetector.TriggerHoneypot()
}

// ==================== 受保护布尔值 ====================

// ProtectedBool 受保护的布尔值
// 使用多个冗余存储和校验来防止内存修改
type ProtectedBool struct {
	value1   uint32 // 主值（加密）
	value2   uint32 // 冗余值（取反后加密）
	checksum uint32 // 校验和
	nonce    uint32 // 随机数
}

// NewProtectedBool 创建受保护的布尔值
func NewProtectedBool(value bool) *ProtectedBool {
	pb := &ProtectedBool{}
	pb.Set(value)
	return pb
}

// Set 设置值
func (pb *ProtectedBool) Set(value bool) {
	// 生成随机 nonce
	nonceBytes := make([]byte, 4)
	crand.Read(nonceBytes)
	pb.nonce = binary.BigEndian.Uint32(nonceBytes)

	if value {
		pb.value1 = 0x5A5A5A5A ^ pb.nonce
		pb.value2 = 0xA5A5A5A5 ^ pb.nonce
	} else {
		pb.value1 = 0xA5A5A5A5 ^ pb.nonce
		pb.value2 = 0x5A5A5A5A ^ pb.nonce
	}

	// 计算校验和
	pb.checksum = pb.value1 ^ pb.value2 ^ pb.nonce ^ 0xDEADBEEF
}

// Get 获取值
func (pb *ProtectedBool) Get() bool {
	// 验证校验和
	expectedChecksum := pb.value1 ^ pb.value2 ^ pb.nonce ^ 0xDEADBEEF
	if pb.checksum != expectedChecksum {
		// 校验失败，可能被篡改
		return false
	}

	// 验证冗余值
	v1 := pb.value1 ^ pb.nonce
	v2 := pb.value2 ^ pb.nonce

	// 检查是否互为取反
	if v1^v2 != 0xFFFFFFFF {
		return false
	}

	return v1 == 0x5A5A5A5A
}

// IsIntact 检查数据完整性
func (pb *ProtectedBool) IsIntact() bool {
	expectedChecksum := pb.value1 ^ pb.value2 ^ pb.nonce ^ 0xDEADBEEF
	if pb.checksum != expectedChecksum {
		return false
	}

	v1 := pb.value1 ^ pb.nonce
	v2 := pb.value2 ^ pb.nonce
	return v1^v2 == 0xFFFFFFFF
}

// ==================== 调用栈检查 ====================

// CallStackChecker 调用栈检查器
// 验证函数调用是否来自合法的调用者
type CallStackChecker struct {
	allowedPackages []string
	blockedPackages []string
	mu              sync.RWMutex
}

// NewCallStackChecker 创建调用栈检查器
func NewCallStackChecker() *CallStackChecker {
	return &CallStackChecker{
		allowedPackages: []string{},
		blockedPackages: []string{
			"github.com/nicholasjackson/grpc-mock",
			"github.com/stretchr/testify/mock",
			"github.com/golang/mock",
		},
	}
}

// AllowPackage 允许特定包调用
func (csc *CallStackChecker) AllowPackage(pkg string) {
	csc.mu.Lock()
	defer csc.mu.Unlock()
	csc.allowedPackages = append(csc.allowedPackages, pkg)
}

// BlockPackage 阻止特定包调用
func (csc *CallStackChecker) BlockPackage(pkg string) {
	csc.mu.Lock()
	defer csc.mu.Unlock()
	csc.blockedPackages = append(csc.blockedPackages, pkg)
}

// CheckCaller 检查调用者是否合法
// skipFrames: 跳过的栈帧数（通常为 2，跳过 CheckCaller 和调用它的函数）
func (csc *CallStackChecker) CheckCaller(skipFrames int) bool {
	csc.mu.RLock()
	defer csc.mu.RUnlock()

	// 获取调用栈
	pc := make([]uintptr, 20)
	n := runtime.Callers(skipFrames+1, pc)
	if n == 0 {
		return false
	}

	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		funcName := frame.Function

		// 检查是否在阻止列表中
		for _, blocked := range csc.blockedPackages {
			if strings.Contains(funcName, blocked) {
				return false
			}
		}

		// 检查是否包含可疑关键词
		suspiciousKeywords := []string{"hook", "patch", "inject", "bypass", "crack", "keygen"}
		funcNameLower := strings.ToLower(funcName)
		for _, keyword := range suspiciousKeywords {
			if strings.Contains(funcNameLower, keyword) {
				return false
			}
		}

		if !more {
			break
		}
	}

	// 如果设置了允许列表，检查是否在允许列表中
	if len(csc.allowedPackages) > 0 {
		frames = runtime.CallersFrames(pc[:n])
		for {
			frame, more := frames.Next()
			funcName := frame.Function

			for _, allowed := range csc.allowedPackages {
				if strings.Contains(funcName, allowed) {
					return true
				}
			}

			if !more {
				break
			}
		}
		return false // 不在允许列表中
	}

	return true
}

// GetCallStack 获取调用栈信息（用于调试）
func (csc *CallStackChecker) GetCallStack(skipFrames int) []string {
	pc := make([]uintptr, 20)
	n := runtime.Callers(skipFrames+1, pc)
	if n == 0 {
		return nil
	}

	var stack []string
	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		stack = append(stack, fmt.Sprintf("%s (%s:%d)", frame.Function, frame.File, frame.Line))
		if !more {
			break
		}
	}
	return stack
}

// ==================== 安全随机数生成 ====================

// SecureRandom 安全随机数生成器
type SecureRandom struct{}

// Uint32 生成安全的 uint32 随机数
func (sr *SecureRandom) Uint32() uint32 {
	var b [4]byte
	crand.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}

// Uint64 生成安全的 uint64 随机数
func (sr *SecureRandom) Uint64() uint64 {
	var b [8]byte
	crand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

// Bytes 生成指定长度的随机字节
func (sr *SecureRandom) Bytes(n int) []byte {
	b := make([]byte, n)
	crand.Read(b)
	return b
}

// Hex 生成指定长度的随机十六进制字符串
func (sr *SecureRandom) Hex(n int) string {
	return hex.EncodeToString(sr.Bytes(n))
}

// IntN 生成 [0, max) 范围内的安全随机整数
func (sr *SecureRandom) IntN(max int64) int64 {
	if max <= 0 {
		return 0
	}
	n, err := crand.Int(crand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return n.Int64()
}

// Duration 生成 [min, max] 范围内的随机时间间隔
func (sr *SecureRandom) Duration(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	diff := int64(max - min)
	return min + time.Duration(sr.IntN(diff))
}

// ==================== 增强型安全客户端扩展 ====================

// 为 AdvancedSecureClient 添加新功能

// protectedValid 受保护的有效状态
var protectedValidMap = make(map[*AdvancedSecureClient]*ProtectedBool)
var protectedValidMu sync.RWMutex

// SetProtectedValid 设置受保护的有效状态
func (asc *AdvancedSecureClient) SetProtectedValid(valid bool) {
	protectedValidMu.Lock()
	defer protectedValidMu.Unlock()

	pb, exists := protectedValidMap[asc]
	if !exists {
		pb = NewProtectedBool(valid)
		protectedValidMap[asc] = pb
	} else {
		pb.Set(valid)
	}
}

// GetProtectedValid 获取受保护的有效状态
func (asc *AdvancedSecureClient) GetProtectedValid() bool {
	protectedValidMu.RLock()
	defer protectedValidMu.RUnlock()

	pb, exists := protectedValidMap[asc]
	if !exists {
		return false
	}
	return pb.Get()
}

// CheckProtectedIntegrity 检查受保护状态的完整性
func (asc *AdvancedSecureClient) CheckProtectedIntegrity() bool {
	protectedValidMu.RLock()
	defer protectedValidMu.RUnlock()

	pb, exists := protectedValidMap[asc]
	if !exists {
		return true // 未设置时默认通过
	}
	return pb.IsIntact()
}

// callStackChecker 调用栈检查器实例
var globalCallStackChecker = NewCallStackChecker()

// secureRandom 安全随机数生成器实例
var secureRandom = &SecureRandom{}

// IsValidWithStackCheck 带调用栈检查的验证
func (asc *AdvancedSecureClient) IsValidWithStackCheck() bool {
	// 检查调用栈
	if !globalCallStackChecker.CheckCaller(2) {
		atomic.StoreInt32(&asc.compromised, 1)
		return false
	}

	return asc.IsValid()
}

// GetSecureRandom 获取安全随机数生成器
func GetSecureRandom() *SecureRandom {
	return secureRandom
}

// GetCallStackChecker 获取调用栈检查器
func GetCallStackChecker() *CallStackChecker {
	return globalCallStackChecker
}
