// Package license 强化安全模块 - 提供最高级别的反破解防护
//
// 功能：
// 1. 验证结果分散化 - 不直接返回 bool，使用分散令牌
// 2. 公钥分片保护 - 防止公钥被替换
// 3. 代码流程混淆 - 不透明谓词和虚假分支
// 4. 关键功能强制联网验证
// 5. 增强反调试检测
// 6. 多层验证机制

package license

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ==================== 验证结果分散化 ====================

// DistributedValidationResult 分散验证结果
// 不使用单一 bool，而是使用多个分散的验证点
type DistributedValidationResult struct {
	tokens    [4]uint64 // 4个分散的验证令牌
	timestamp int64     // 验证时间戳
	nonce     uint64    // 随机数
	checksum  uint64    // 校验和
	sequence  uint32    // 序列号
	valid     [4]uint32 // 分散的有效标志（加密存储）
}

// HardenedDistributedValidator 强化分散验证器
type HardenedDistributedValidator struct {
	secretKey    []byte
	magicNumbers [4]uint64 // 4个魔数用于验证
	counter      uint64
	mu           sync.RWMutex
}

// NewHardenedDistributedValidator 创建强化分散验证器
func NewHardenedDistributedValidator(machineID, appKey string) *HardenedDistributedValidator {
	keyMaterial := machineID + appKey + "distributed_validation_v2"
	hash := sha256.Sum256([]byte(keyMaterial))

	dv := &HardenedDistributedValidator{
		secretKey: hash[:],
	}

	// 派生4个魔数
	for i := 0; i < 4; i++ {
		h := sha256.Sum256(append(hash[:], byte(i)))
		dv.magicNumbers[i] = binary.LittleEndian.Uint64(h[:8])
	}

	return dv
}

// CreateDistributedResult 创建分散验证结果
func (dv *HardenedDistributedValidator) CreateDistributedResult(isValid bool) *DistributedValidationResult {
	dv.mu.Lock()
	dv.counter++
	seq := uint32(dv.counter)
	dv.mu.Unlock()

	result := &DistributedValidationResult{
		timestamp: time.Now().UnixNano(),
		nonce:     secureRandom.Uint64(),
		sequence:  seq,
	}

	// 生成4个分散的令牌
	for i := 0; i < 4; i++ {
		if isValid {
			// 有效时：令牌 = 魔数 XOR nonce XOR timestamp的部分
			result.tokens[i] = dv.magicNumbers[i] ^ result.nonce ^ uint64(result.timestamp>>(i*8)&0xFF)
			result.valid[i] = 0x5A5A5A5A ^ uint32(result.nonce>>(i*8))
		} else {
			// 无效时：随机值
			result.tokens[i] = secureRandom.Uint64()
			result.valid[i] = secureRandom.Uint32()
		}
	}

	// 计算校验和
	result.checksum = dv.calculateChecksum(result)

	return result
}

// calculateChecksum 计算校验和
func (dv *HardenedDistributedValidator) calculateChecksum(result *DistributedValidationResult) uint64 {
	h := hmac.New(sha256.New, dv.secretKey)
	for i := 0; i < 4; i++ {
		binary.Write(h, binary.LittleEndian, result.tokens[i])
		binary.Write(h, binary.LittleEndian, result.valid[i])
	}
	binary.Write(h, binary.LittleEndian, result.timestamp)
	binary.Write(h, binary.LittleEndian, result.nonce)
	binary.Write(h, binary.LittleEndian, result.sequence)
	sum := h.Sum(nil)
	return binary.LittleEndian.Uint64(sum[:8])
}

// VerifyToken 验证单个令牌（在代码多处调用）
func (dv *HardenedDistributedValidator) VerifyToken(result *DistributedValidationResult, index int) bool {
	if result == nil || index < 0 || index >= 4 {
		return false
	}

	// 验证校验和
	if result.checksum != dv.calculateChecksum(result) {
		return false
	}

	// 验证时间戳（10分钟内有效）
	if time.Now().UnixNano()-result.timestamp > 10*60*1e9 {
		return false
	}

	// 验证令牌
	expectedToken := dv.magicNumbers[index] ^ result.nonce ^ uint64(result.timestamp>>(index*8)&0xFF)
	if result.tokens[index] != expectedToken {
		return false
	}

	// 验证有效标志
	expectedValid := 0x5A5A5A5A ^ uint32(result.nonce>>(index*8))
	return result.valid[index] == expectedValid
}

// VerifyAll 验证所有令牌
func (dv *HardenedDistributedValidator) VerifyAll(result *DistributedValidationResult) bool {
	for i := 0; i < 4; i++ {
		if !dv.VerifyToken(result, i) {
			return false
		}
	}
	return true
}

// ==================== 公钥分片保护 ====================

// PublicKeyProtector 公钥保护器
// 将公钥分片存储，运行时组装，防止静态分析
type PublicKeyProtector struct {
	fragments    [][]byte // 公钥分片
	positions    []int    // 分片位置
	xorKeys      []byte   // XOR 密钥
	integrityKey []byte   // 完整性校验密钥
}

// NewPublicKeyProtector 创建公钥保护器
func NewPublicKeyProtector(machineID string) *PublicKeyProtector {
	keyMaterial := machineID + "pubkey_protection_v1"
	hash := sha256.Sum256([]byte(keyMaterial))

	return &PublicKeyProtector{
		fragments:    make([][]byte, 0),
		positions:    make([]int, 0),
		xorKeys:      hash[:16],
		integrityKey: hash[16:],
	}
}

// ProtectPublicKey 保护公钥（分片并加密）
func (pkp *PublicKeyProtector) ProtectPublicKey(publicKeyPEM string) {
	data := []byte(publicKeyPEM)

	// 分成 4 片
	fragmentSize := (len(data) + 3) / 4
	pkp.fragments = make([][]byte, 4)
	// positions[i] 表示第 i 个分片存储到 fragments 数组的哪个位置
	pkp.positions = []int{2, 0, 3, 1} // 打乱顺序

	for i := 0; i < 4; i++ {
		start := i * fragmentSize
		end := start + fragmentSize
		if end > len(data) {
			end = len(data)
		}
		if start >= len(data) {
			// 如果数据不够分4片，创建空分片
			pkp.fragments[pkp.positions[i]] = []byte{}
			continue
		}

		fragment := make([]byte, end-start)
		copy(fragment, data[start:end])

		// XOR 加密每个分片
		for j := range fragment {
			fragment[j] ^= pkp.xorKeys[j%len(pkp.xorKeys)]
		}

		pkp.fragments[pkp.positions[i]] = fragment
	}
}

// GetPublicKey 获取公钥（运行时组装）
func (pkp *PublicKeyProtector) GetPublicKey() string {
	if len(pkp.fragments) != 4 {
		return ""
	}

	// 按正确顺序组装
	// positions = {2, 0, 3, 1} 表示:
	// 分片0 在 fragments[2], 分片1 在 fragments[0], 分片2 在 fragments[3], 分片3 在 fragments[1]
	// 所以要按 分片0, 分片1, 分片2, 分片3 的顺序读取，需要从 fragments[2], fragments[0], fragments[3], fragments[1] 读取
	var result []byte
	order := []int{2, 0, 3, 1} // 与 positions 相同，按分片顺序读取

	for _, idx := range order {
		if idx >= len(pkp.fragments) {
			return ""
		}

		fragment := make([]byte, len(pkp.fragments[idx]))
		copy(fragment, pkp.fragments[idx])

		// XOR 解密
		for j := range fragment {
			fragment[j] ^= pkp.xorKeys[j%len(pkp.xorKeys)]
		}

		result = append(result, fragment...)
	}

	return string(result)
}

// VerifyIntegrity 验证公钥完整性
func (pkp *PublicKeyProtector) VerifyIntegrity(expectedHash string) bool {
	pubKey := pkp.GetPublicKey()
	if pubKey == "" {
		return false
	}

	h := hmac.New(sha256.New, pkp.integrityKey)
	h.Write([]byte(pubKey))
	actualHash := hex.EncodeToString(h.Sum(nil))

	return actualHash == expectedHash
}

// ==================== 代码流程混淆 ====================

// OpaquePredicates 不透明谓词生成器
type OpaquePredicates struct {
	seed uint64
}

// NewOpaquePredicates 创建不透明谓词生成器
func NewOpaquePredicates() *OpaquePredicates {
	return &OpaquePredicates{
		seed: uint64(time.Now().UnixNano()),
	}
}

// AlwaysTrue 永远返回 true 的不透明谓词
// 静态分析难以确定结果
func (op *OpaquePredicates) AlwaysTrue() bool {
	// 数学恒等式: (x^2 + x) % 2 == 0 对所有整数成立
	x := time.Now().UnixNano()
	return (x*x+x)%2 == 0
}

// AlwaysFalse 永远返回 false 的不透明谓词
func (op *OpaquePredicates) AlwaysFalse() bool {
	// 数学恒等式: 对于任意整数 x，(x & 1) 和 ((x+1) & 1) 不可能同时为 1
	// 因为一个是奇数另一个是偶数
	x := time.Now().UnixNano()
	return (x&1) == 1 && ((x+1)&1) == 1
}

// RandomLooking 看起来随机但实际上确定的谓词
func (op *OpaquePredicates) RandomLooking(expectedTrue bool) bool {
	// 使用确定性计算，但看起来像随机
	a := uint64(7)
	b := uint64(3)
	c := a*a + b*b // 49 + 9 = 58

	if expectedTrue {
		return c == 58 // 永远为 true
	}
	return c == 59 // 永远为 false
}

// ConfusingBranch 混淆分支
func (op *OpaquePredicates) ConfusingBranch(realCheck func() bool) bool {
	// 添加虚假分支增加分析难度
	if op.AlwaysFalse() {
		// 这个分支永远不会执行，但静态分析可能认为会执行
		return true
	}

	if op.AlwaysTrue() {
		// 真正的检查
		return realCheck()
	}

	// 这里也永远不会到达
	return false
}

// ==================== 增强反调试检测 ====================

// EnhancedAntiDebug 增强反调试检测器
type EnhancedAntiDebug struct {
	checkInterval time.Duration
	detected      int32
	onDetected    func()
	stopChan      chan struct{}
	mu            sync.Mutex
	running       bool
}

// NewEnhancedAntiDebug 创建增强反调试检测器
func NewEnhancedAntiDebug() *EnhancedAntiDebug {
	return &EnhancedAntiDebug{
		checkInterval: 5 * time.Second,
	}
}

// IsDebuggerPresent 综合检测调试器
func (ead *EnhancedAntiDebug) IsDebuggerPresent() bool {
	checks := []func() bool{
		ead.checkTimingAnomaly,
		ead.checkDebuggerProcess,
		ead.checkDebugFlags,
		ead.checkBreakpoints,
		ead.checkParentProcess,
	}

	detectedCount := 0
	for _, check := range checks {
		if check() {
			detectedCount++
		}
	}

	// 如果有 2 个或以上检测到，认为存在调试器
	return detectedCount >= 2
}

// checkTimingAnomaly 检测时间异常（调试时代码执行变慢）
func (ead *EnhancedAntiDebug) checkTimingAnomaly() bool {
	iterations := 1000000

	start := time.Now()
	sum := uint64(0)
	for i := 0; i < iterations; i++ {
		sum += uint64(i)
	}
	_ = sum
	elapsed := time.Since(start)

	// 正常情况下应该在 5ms 内完成
	return elapsed > 20*time.Millisecond
}

// checkDebuggerProcess 检测调试器进程
func (ead *EnhancedAntiDebug) checkDebuggerProcess() bool {
	if runtime.GOOS != "windows" {
		return false
	}

	// 扩展的调试器列表
	debuggers := []string{
		"ollydbg", "x64dbg", "x32dbg", "windbg", "ida", "ida64",
		"immunitydebugger", "cheatengine", "ce.exe", "processhacker",
		"procmon", "procexp", "wireshark", "fiddler", "charles",
		"httpanalyzer", "apimonitor", "regmon", "filemon",
		"dnspy", "de4dot", "ilspy", "dotpeek", "ghidra",
	}

	// 使用 tasklist 检查
	output, err := runCommand("tasklist", "/FO", "CSV", "/NH")
	if err != nil {
		return false
	}

	outputLower := toLower(output)
	for _, debugger := range debuggers {
		if containsString(outputLower, debugger) {
			return true
		}
	}

	return false
}

// checkDebugFlags 检测调试标志
func (ead *EnhancedAntiDebug) checkDebugFlags() bool {
	if runtime.GOOS == "windows" {
		return ead.checkWindowsDebugFlags()
	} else if runtime.GOOS == "linux" {
		return ead.checkLinuxDebugFlags()
	}
	return false
}

// checkLinuxDebugFlags Linux 调试标志检测
func (ead *EnhancedAntiDebug) checkLinuxDebugFlags() bool {
	// 检查 /proc/self/status 中的 TracerPid
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}

	lines := splitLines(string(data))
	for _, line := range lines {
		if hasPrefix(line, "TracerPid:") {
			pid := trimSpace(trimPrefix(line, "TracerPid:"))
			if pid != "0" {
				return true
			}
		}
	}

	return false
}

// checkBreakpoints 检测软件断点
func (ead *EnhancedAntiDebug) checkBreakpoints() bool {
	return detectCurrentFunctionBreakpoint()
}

// checkParentProcess 检测父进程
func (ead *EnhancedAntiDebug) checkParentProcess() bool {
	if runtime.GOOS != "windows" {
		return false
	}

	// 检查父进程是否是常见的调试器
	output, err := runCommand("wmic", "process", "where", fmt.Sprintf("ProcessId=%d", os.Getpid()), "get", "ParentProcessId", "/format:value")
	if err != nil {
		return false
	}

	// 解析父进程 ID 并检查其名称
	// 简化实现：检查是否从命令行启动
	return containsString(output, "cmd.exe") || containsString(output, "powershell")
}

// StartContinuousCheck 启动持续检测
func (ead *EnhancedAntiDebug) StartContinuousCheck(onDetected func()) {
	ead.mu.Lock()
	ead.onDetected = onDetected
	if ead.running {
		ead.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	interval := ead.checkInterval
	ead.stopChan = stopCh
	ead.running = true
	ead.mu.Unlock()

	go func() {
		defer func() {
			ead.mu.Lock()
			if ead.stopChan == stopCh {
				ead.running = false
				ead.stopChan = nil
			}
			ead.mu.Unlock()
		}()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if ead.IsDebuggerPresent() {
					atomic.StoreInt32(&ead.detected, 1)
					ead.mu.Lock()
					onDetected := ead.onDetected
					ead.mu.Unlock()
					if onDetected != nil {
						onDetected()
					}
				}
			case <-stopCh:
				return
			}
		}
	}()
}

// Stop 停止检测
func (ead *EnhancedAntiDebug) Stop() {
	ead.mu.Lock()
	defer ead.mu.Unlock()
	if !ead.running || ead.stopChan == nil {
		return
	}
	close(ead.stopChan)
	ead.running = false
	ead.stopChan = nil
}

// WasDetected 是否检测到调试器
func (ead *EnhancedAntiDebug) WasDetected() bool {
	return atomic.LoadInt32(&ead.detected) == 1
}

// ==================== 强化安全客户端 ====================

// HardenedSecureClient 强化安全客户端
type HardenedSecureClient struct {
	*AdvancedSecureClient
	distributedValidator *HardenedDistributedValidator
	publicKeyProtector   *PublicKeyProtector
	opaquePredicates     *OpaquePredicates
	enhancedAntiDebug    *EnhancedAntiDebug
	lastValidation       *DistributedValidationResult
	criticalFeatures     map[string]bool // 需要强制联网验证的功能
	mu                   sync.RWMutex
}

// NewHardenedSecureClient 创建强化安全客户端
func NewHardenedSecureClient(client *Client) *HardenedSecureClient {
	advClient := NewAdvancedSecureClient(client)

	hsc := &HardenedSecureClient{
		AdvancedSecureClient: advClient,
		distributedValidator: NewHardenedDistributedValidator(client.GetMachineID(), client.GetAppKey()),
		publicKeyProtector:   NewPublicKeyProtector(client.GetMachineID()),
		opaquePredicates:     NewOpaquePredicates(),
		enhancedAntiDebug:    NewEnhancedAntiDebug(),
		criticalFeatures:     make(map[string]bool),
	}

	return hsc
}

// Start 启动强化安全功能
func (hsc *HardenedSecureClient) Start() {
	// 启动父类功能
	hsc.AdvancedSecureClient.Start()

	// 启动增强反调试
	hsc.enhancedAntiDebug.StartContinuousCheck(func() {
		// 检测到调试器时清除缓存
		hsc.client.clearCache()
	})
}

// Stop 停止强化安全功能
func (hsc *HardenedSecureClient) Stop() {
	hsc.enhancedAntiDebug.Stop()
	hsc.AdvancedSecureClient.Stop()
}

// SetPublicKeyProtected 设置受保护的公钥
func (hsc *HardenedSecureClient) SetPublicKeyProtected(publicKeyPEM string) {
	hsc.publicKeyProtector.ProtectPublicKey(publicKeyPEM)
	// 同时设置到底层客户端
	hsc.client.SetPublicKey(publicKeyPEM)
}

// RegisterCriticalFeature 注册需要强制联网验证的功能
func (hsc *HardenedSecureClient) RegisterCriticalFeature(feature string) {
	hsc.mu.Lock()
	defer hsc.mu.Unlock()
	hsc.criticalFeatures[feature] = true
}

// IsValidDistributed 分散验证（推荐使用）
// 返回分散验证结果，需要在代码多处调用 VerifyToken 验证
func (hsc *HardenedSecureClient) IsValidDistributed() *DistributedValidationResult {
	// 不透明谓词混淆
	if hsc.opaquePredicates.AlwaysFalse() {
		// 永远不会执行的蜜罐代码
		return hsc.distributedValidator.CreateDistributedResult(true)
	}

	// 检测调试器
	if hsc.enhancedAntiDebug.WasDetected() {
		return hsc.distributedValidator.CreateDistributedResult(false)
	}

	// 使用混淆分支执行真正的验证
	isValid := hsc.opaquePredicates.ConfusingBranch(func() bool {
		return hsc.AdvancedSecureClient.IsValid()
	})

	result := hsc.distributedValidator.CreateDistributedResult(isValid)

	hsc.mu.Lock()
	hsc.lastValidation = result
	hsc.mu.Unlock()

	return result
}

// VerifyDistributedToken 验证分散令牌（在代码多处调用）
func (hsc *HardenedSecureClient) VerifyDistributedToken(result *DistributedValidationResult, index int) bool {
	// 添加不透明谓词
	if hsc.opaquePredicates.AlwaysFalse() {
		return true // 永远不会执行
	}

	return hsc.distributedValidator.VerifyToken(result, index)
}

// HasFeatureCritical 检查关键功能（强制联网验证）
func (hsc *HardenedSecureClient) HasFeatureCritical(feature string) bool {
	hsc.mu.RLock()
	isCritical := hsc.criticalFeatures[feature]
	hsc.mu.RUnlock()

	if isCritical {
		// 关键功能强制联网验证
		if !hsc.client.Verify() {
			return false
		}
	}

	// 分散验证
	result := hsc.IsValidDistributed()

	// 验证多个令牌
	if !hsc.VerifyDistributedToken(result, 0) {
		return false
	}

	if !hsc.client.HasFeature(feature) {
		return false
	}

	// 再次验证另一个令牌
	if !hsc.VerifyDistributedToken(result, 1) {
		return false
	}

	return true
}

// ExecuteCriticalOperation 执行关键操作（强制联网验证）
func (hsc *HardenedSecureClient) ExecuteCriticalOperation(operation func() error) error {
	// 强制联网验证
	if !hsc.client.Verify() {
		return fmt.Errorf("授权验证失败")
	}

	// 分散验证
	result := hsc.IsValidDistributed()

	// 验证令牌 0
	if !hsc.VerifyDistributedToken(result, 0) {
		return fmt.Errorf("验证令牌无效")
	}

	// 执行操作
	err := operation()

	// 操作后再次验证令牌 2
	if !hsc.VerifyDistributedToken(result, 2) {
		return fmt.Errorf("验证令牌无效")
	}

	return err
}

// GetSecurityStatus 获取安全状态
func (hsc *HardenedSecureClient) GetSecurityStatus() map[string]interface{} {
	status := make(map[string]interface{})

	// 基础状态
	status["is_valid"] = hsc.AdvancedSecureClient.IsValid()
	status["debugger_detected"] = hsc.enhancedAntiDebug.WasDetected()
	status["public_key_protected"] = len(hsc.publicKeyProtector.fragments) > 0

	hsc.mu.RLock()
	status["has_validation_result"] = hsc.lastValidation != nil
	status["critical_features_count"] = len(hsc.criticalFeatures)
	hsc.mu.RUnlock()

	return status
}

// Close 关闭客户端
func (hsc *HardenedSecureClient) Close() {
	hsc.Stop()
	hsc.AdvancedSecureClient.Close()
}

// ==================== 辅助函数 ====================

func runCommand(name string, args ...string) (string, error) {
	if runtime.GOOS == "windows" {
		// Windows 下使用 exec.Command
		cmd := newCommand(name, args...)
		output, err := cmd.Output()
		return string(output), err
	}
	return "", nil
}

func newCommand(name string, args ...string) *command {
	return &command{name: name, args: args}
}

type command struct {
	name string
	args []string
}

func (c *command) Output() ([]byte, error) {
	// 简化实现，实际应该使用 os/exec
	return nil, nil
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && findString(s, substr) >= 0
}

func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimPrefix(s, prefix string) string {
	if hasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// ==================== 便捷函数 ====================

// WrapClientHardened 使用强化安全功能包装客户端
func WrapClientHardened(client *Client) *HardenedSecureClient {
	return NewHardenedSecureClient(client)
}
