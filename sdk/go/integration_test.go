//go:build integration
// +build integration

package license

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// 集成测试配置 - 使用实际服务器
var (
	IntegrationServerURL = getEnvOrDefault("LS_INTEGRATION_SERVER_URL", "http://localhost:8081")
	IntegrationAppKey    = getEnvOrDefault("LS_INTEGRATION_APP_KEY", "18563102289d9bab79daa5e77479eb9a")
	TestLicenseKey       = getEnvOrDefault("LS_TEST_LICENSE_KEY", "F92B-73DA-C489-15C2")
	TestEmail            = getEnvOrDefault("LS_TEST_EMAIL", "test@test.com")
	TestPassword         = getEnvOrDefault("LS_TEST_PASSWORD", "Test123456")
)

func getEnvOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func requireIntegrationServer(t *testing.T) {
	t.Helper()

	healthURL := strings.TrimRight(IntegrationServerURL, "/") + "/api/health"
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Get(healthURL)
	if err != nil {
		t.Skipf("集成测试跳过: 无法访问 %s (%v)。请启动 license-server 或设置 LS_INTEGRATION_SERVER_URL。", healthURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("集成测试跳过: %s 返回 HTTP %d。请确认地址指向 license-server。", healthURL, resp.StatusCode)
	}
}

func requireIntegrationLicenseKey(t *testing.T) {
	t.Helper()

	if strings.TrimSpace(os.Getenv("LS_TEST_LICENSE_KEY")) == "" {
		t.Skip("集成测试跳过: 未设置 LS_TEST_LICENSE_KEY，授权码激活测试需要一个有效授权码。")
	}
}

// TestIntegration_Activate 测试授权码激活功能
func TestIntegration_Activate(t *testing.T) {
	requireIntegrationServer(t)
	requireIntegrationLicenseKey(t)
	fmt.Println("\n========== 集成测试: 授权码激活 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	fmt.Printf("服务器地址: %s\n", client.GetServerURL())
	fmt.Printf("应用密钥: %s\n", client.GetAppKey())
	fmt.Printf("机器码: %s\n", client.GetMachineID())

	// 执行激活
	fmt.Printf("\n正在使用授权码激活: %s\n", TestLicenseKey)
	result, err := client.Activate(TestLicenseKey)
	if err != nil {
		t.Fatalf("激活失败: %v", err)
	}

	fmt.Println("\n激活成功!")
	fmt.Printf("  授权有效: %v\n", result.Valid)
	fmt.Printf("  授权ID: %s\n", result.LicenseID)
	fmt.Printf("  设备ID: %s\n", result.DeviceID)
	fmt.Printf("  类型: %s\n", result.Type)
	fmt.Printf("  剩余天数: %d\n", result.RemainingDays)
	fmt.Printf("  功能列表: %v\n", result.Features)
	if result.ExpireAt != nil {
		fmt.Printf("  过期时间: %s\n", *result.ExpireAt)
	}

	// 验证授权状态
	if !client.IsValid() {
		t.Error("激活后授权状态应该有效")
	}

	fmt.Println("\n授权码激活测试通过!")
}

// TestIntegration_Login 测试账号密码登录功能
func TestIntegration_Login(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 账号密码登录 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	fmt.Printf("服务器地址: %s\n", client.GetServerURL())
	fmt.Printf("应用密钥: %s\n", client.GetAppKey())
	fmt.Printf("机器码: %s\n", client.GetMachineID())

	// 执行登录
	fmt.Printf("\n正在使用账号登录: %s\n", TestEmail)
	result, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	fmt.Println("\n登录成功!")
	fmt.Printf("  授权有效: %v\n", result.Valid)
	fmt.Printf("  订阅ID: %s\n", result.SubscriptionID)
	fmt.Printf("  设备ID: %s\n", result.DeviceID)
	fmt.Printf("  套餐类型: %s\n", result.PlanType)
	fmt.Printf("  剩余天数: %d\n", result.RemainingDays)
	fmt.Printf("  功能列表: %v\n", result.Features)
	fmt.Printf("  邮箱: %s\n", result.Email)
	if result.ExpireAt != nil {
		fmt.Printf("  过期时间: %s\n", *result.ExpireAt)
	}

	// 验证授权状态
	if !client.IsValid() {
		t.Error("登录后授权状态应该有效")
	}

	fmt.Println("\n账号密码登录测试通过!")
}

// TestIntegration_Heartbeat 测试心跳功能（订阅模式）
func TestIntegration_Heartbeat(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 心跳功能（订阅模式） ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	fmt.Printf("先进行登录...\n")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 发送订阅模式心跳
	fmt.Println("\n发送订阅模式心跳...")
	heartbeatResult := client.SubscriptionHeartbeat()
	fmt.Printf("心跳结果: %v\n", heartbeatResult)

	if !heartbeatResult {
		t.Error("心跳应该返回成功")
	}

	// 验证订阅状态
	fmt.Println("\n验证订阅状态...")
	verifyResult := client.SubscriptionVerify()
	fmt.Printf("验证结果: %v\n", verifyResult)

	if !verifyResult {
		t.Error("验证应该返回成功")
	}

	fmt.Println("\n心跳功能测试通过!")
}

// TestIntegration_Verify 测试验证功能（订阅模式）
func TestIntegration_Verify(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 验证功能（订阅模式） ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	fmt.Printf("先进行登录...\n")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 验证订阅
	fmt.Println("\n验证订阅状态...")
	verifyResult := client.SubscriptionVerify()
	fmt.Printf("验证结果: %v\n", verifyResult)

	if !verifyResult {
		t.Error("验证应该返回成功")
	}

	// 检查本地状态
	fmt.Println("\n检查本地授权状态...")
	isValid := client.IsValid()
	fmt.Printf("本地授权有效: %v\n", isValid)

	if !isValid {
		t.Error("本地授权状态应该有效")
	}

	// 获取授权信息
	info := client.GetLicenseInfo()
	if info != nil {
		fmt.Printf("\n授权信息:\n")
		fmt.Printf("  有效: %v\n", info.Valid)
		fmt.Printf("  剩余天数: %d\n", info.RemainingDays)
		fmt.Printf("  功能: %v\n", info.Features)
	}

	fmt.Println("\n验证功能测试通过!")
}

// TestIntegration_Features 测试功能权限检查
func TestIntegration_Features(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 功能权限检查 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	fmt.Printf("先进行登录...\n")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 获取功能列表
	features := client.GetFeatures()
	fmt.Printf("\n功能列表: %v\n", features)

	// 检查特定功能
	fmt.Println("\n检查功能权限:")
	testFeatures := []string{"basic", "export", "print", "advanced"}
	for _, f := range testFeatures {
		hasFeature := client.HasFeature(f)
		fmt.Printf("  %s: %v\n", f, hasFeature)
	}

	// 获取剩余天数
	remainingDays := client.GetRemainingDays()
	fmt.Printf("\n剩余天数: %d\n", remainingDays)

	fmt.Println("\n功能权限检查测试通过!")
}

// TestIntegration_FullWorkflow 测试完整工作流程
func TestIntegration_FullWorkflow(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 完整工作流程 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
		WithEncryptCache(true),
	)
	defer client.Close()

	fmt.Printf("服务器地址: %s\n", client.GetServerURL())
	fmt.Printf("应用密钥: %s\n", client.GetAppKey())
	fmt.Printf("机器码: %s\n", client.GetMachineID())

	// 步骤1: 登录
	fmt.Println("\n--- 步骤1: 登录 ---")
	loginResult, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Printf("登录成功! 订阅ID: %s\n", loginResult.SubscriptionID)

	// 步骤2: 验证授权
	fmt.Println("\n--- 步骤2: 验证授权 ---")
	if !client.IsValid() {
		t.Error("授权应该有效")
	}
	fmt.Println("授权验证通过!")

	// 步骤3: 发送心跳
	fmt.Println("\n--- 步骤3: 发送心跳 ---")
	// 使用订阅模式的心跳方法（因为是通过Login登录的）
	if !client.SubscriptionHeartbeat() {
		t.Error("心跳应该成功")
	} else {
		fmt.Println("心跳发送成功!")
	}

	// 步骤4: 检查功能
	fmt.Println("\n--- 步骤4: 检查功能 ---")
	features := client.GetFeatures()
	fmt.Printf("可用功能: %v\n", features)

	// 步骤5: 获取授权信息
	fmt.Println("\n--- 步骤5: 获取授权信息 ---")
	info := client.GetLicenseInfo()
	if info != nil {
		fmt.Printf("授权有效: %v\n", info.Valid)
		fmt.Printf("剩余天数: %d\n", info.RemainingDays)
		fmt.Printf("套餐类型: %s\n", info.PlanType)
	}

	// 步骤6: 严格模式验证
	fmt.Println("\n--- 步骤6: 严格模式验证 ---")
	// 使用订阅模式的验证方法（因为是通过Login登录的）
	if !client.SubscriptionVerify() {
		t.Error("严格模式验证应该通过")
	} else {
		fmt.Println("严格模式验证通过!")
	}

	fmt.Println("\n========== 完整工作流程测试通过! ==========")
}

// TestIntegration_CacheAndOffline 测试缓存和离线功能
func TestIntegration_CacheAndOffline(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 缓存和离线功能 ==========")

	// 创建第一个客户端并登录
	client1 := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithEncryptCache(true),
	)

	fmt.Println("客户端1: 登录并缓存...")
	_, err := client1.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 验证授权有效
	if !client1.IsValid() {
		t.Error("客户端1授权应该有效")
	}
	fmt.Println("客户端1授权有效!")

	// 关闭第一个客户端
	client1.Close()
	fmt.Println("客户端1已关闭")

	// 创建第二个客户端，应该从缓存加载
	fmt.Println("\n客户端2: 从缓存加载...")
	client2 := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithEncryptCache(true),
	)
	defer client2.Close()

	// 检查是否从缓存加载了授权信息
	info := client2.GetLicenseInfo()
	if info == nil {
		t.Error("应该从缓存加载授权信息")
	} else {
		fmt.Printf("从缓存加载成功!\n")
		fmt.Printf("  授权有效: %v\n", info.Valid)
		fmt.Printf("  剩余天数: %d\n", info.RemainingDays)
	}

	// 验证离线状态下授权仍然有效
	if !client2.IsValid() {
		t.Error("离线状态下授权应该有效（在宽限期内）")
	}
	fmt.Println("离线状态下授权有效!")

	fmt.Println("\n缓存和离线功能测试通过!")
}

// TestIntegration_SecureClient 测试安全客户端
func TestIntegration_SecureClient(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全客户端 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
	)
	defer client.Close()

	// 创建安全客户端
	secureClient := NewSecureClient(client)
	defer secureClient.Close()

	fmt.Printf("安全令牌: %s\n", secureClient.GetSecurityToken())

	// 先登录
	fmt.Println("\n登录中...")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 使用安全客户端验证
	fmt.Println("\n使用安全客户端验证...")
	isValid := secureClient.IsValid()
	fmt.Printf("安全验证结果: %v\n", isValid)

	if !isValid {
		t.Error("安全客户端验证应该通过")
	}

	fmt.Println("\n安全客户端测试通过!")
}

// TestIntegration_HardenedClient 测试强化安全客户端
func TestIntegration_HardenedClient(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 强化安全客户端 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
	)
	defer client.Close()

	// 创建强化安全客户端
	hardenedClient := NewHardenedSecureClient(client)
	defer hardenedClient.Close()

	// 先登录
	fmt.Println("登录中...")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 获取安全状态
	fmt.Println("\n获取安全状态...")
	status := hardenedClient.GetSecurityStatus()
	fmt.Printf("安全状态: %v\n", status)

	// 分布式验证
	fmt.Println("\n执行分布式验证...")
	result := hardenedClient.IsValidDistributed()
	if result != nil {
		fmt.Println("分布式验证结果已生成")
		for i := 0; i < 4; i++ {
			verified := hardenedClient.VerifyDistributedToken(result, i)
			fmt.Printf("  Token %d 验证: %v\n", i, verified)
		}
	}

	fmt.Println("\n强化安全客户端测试通过!")
}
