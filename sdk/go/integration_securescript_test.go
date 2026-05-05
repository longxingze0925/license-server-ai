//go:build integration
// +build integration

package license

import (
	"fmt"
	"testing"
	"time"
)

func requireAvailableSecureScript(t *testing.T, client *Client) (*SecureScriptManager, string) {
	t.Helper()
	scriptManager := NewSecureScriptManager(client,
		WithAppSecret("test_app_secret"),
	)
	versions, err := scriptManager.GetScriptVersions()
	if err != nil {
		t.Fatalf("获取脚本版本列表失败: %v", err)
	}
	if len(versions) == 0 {
		t.Skip("当前测试环境没有已发布的安全脚本，跳过需要真实脚本的集成测试")
	}
	return scriptManager, versions[0].ScriptID
}

// TestIntegration_SecureScript_GetVersions 测试获取脚本版本列表
func TestIntegration_SecureScript_GetVersions(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 获取版本列表 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	fmt.Println("登录中...")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 创建安全脚本管理器
	scriptManager := NewSecureScriptManager(client,
		WithAppSecret("test_app_secret"),
	)

	// 获取脚本版本列表
	fmt.Println("\n获取脚本版本列表...")
	versions, err := scriptManager.GetScriptVersions()
	if err != nil {
		t.Fatalf("获取版本列表失败: %v", err)
	}
	fmt.Printf("脚本版本数: %d\n", len(versions))
	for _, v := range versions {
		fmt.Printf("  脚本ID: %s, 名称: %s, 版本: %s\n",
			v.ScriptID, v.Name, v.Version)
	}

	fmt.Println("\n安全脚本 - 获取版本列表测试通过!")
}

// TestIntegration_SecureScript_FetchScript 测试获取加密脚本
func TestIntegration_SecureScript_FetchScript(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 获取加密脚本 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	fmt.Println("登录中...")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	scriptManager, testScriptID := requireAvailableSecureScript(t, client)

	fmt.Printf("\n获取脚本 %s...\n", testScriptID)
	script, err := scriptManager.FetchScript(testScriptID)
	if err != nil {
		t.Fatalf("获取脚本失败: %v", err)
	}
	fmt.Printf("获取成功!\n")
	fmt.Printf("  脚本ID: %s\n", script.ScriptID)
	fmt.Printf("  版本: %s\n", script.Version)
	fmt.Printf("  内容长度: %d 字节\n", len(script.Content))
	fmt.Printf("  内容哈希: %s\n", script.ContentHash)
	fmt.Printf("  获取时间: %s\n", script.FetchedAt.Format(time.RFC3339))
	fmt.Printf("  过期时间: %s\n", script.ExpiresAt.Format(time.RFC3339))

	fmt.Println("\n安全脚本 - 获取加密脚本测试通过!")
}

// TestIntegration_SecureScript_CacheManagement 测试缓存管理
func TestIntegration_SecureScript_CacheManagement(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 缓存管理 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	scriptManager, testScriptID := requireAvailableSecureScript(t, client)

	// 检查缓存（应该为空）
	fmt.Println("\n检查初始缓存状态...")
	cached := scriptManager.GetCachedScript(testScriptID)
	if cached == nil {
		fmt.Println("缓存为空 (预期)")
	} else {
		t.Fatalf("初始缓存应该为空，实际发现: %s", cached.ScriptID)
	}

	// 尝试获取脚本（会缓存）
	fmt.Printf("\n尝试获取脚本 %s...\n", testScriptID)
	script, err := scriptManager.FetchScript(testScriptID)
	if err != nil {
		t.Fatalf("获取脚本失败: %v", err)
	}

	// 再次检查缓存
	fmt.Println("\n再次检查缓存...")
	cached = scriptManager.GetCachedScript(testScriptID)
	if cached == nil {
		t.Fatalf("脚本获取成功后缓存仍为空: %s", testScriptID)
	}
	fmt.Printf("缓存命中! 脚本ID: %s, 版本: %s\n", cached.ScriptID, cached.Version)

	// 验证从缓存获取
	fmt.Println("\n验证从缓存获取...")
	script2, err := scriptManager.FetchScript(testScriptID)
	if err != nil {
		t.Fatalf("从缓存获取失败: %v", err)
	}
	if script2.ScriptID != script.ScriptID {
		t.Fatalf("缓存脚本ID不匹配: 期望 %s 实际 %s", script.ScriptID, script2.ScriptID)
	}
	fmt.Println("从缓存获取成功!")

	// 清除缓存
	fmt.Println("\n清除缓存...")
	scriptManager.ClearCache()

	// 验证缓存已清除
	cached = scriptManager.GetCachedScript(testScriptID)
	if cached == nil {
		fmt.Println("缓存已清除 (预期)")
	} else {
		t.Error("缓存应该已被清除")
	}

	fmt.Println("\n安全脚本 - 缓存管理测试通过!")
}

// TestIntegration_SecureScript_ExecuteCallback 测试执行回调
func TestIntegration_SecureScript_ExecuteCallback(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 执行回调 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	_, testScriptID := requireAvailableSecureScript(t, client)

	// 回调计数
	callbackCount := 0
	var lastStatus string
	var lastError error

	// 创建带回调的安全脚本管理器
	scriptManager := NewSecureScriptManager(client,
		WithAppSecret("test_app_secret"),
		WithExecuteCallback(func(scriptID string, status string, err error) {
			callbackCount++
			lastStatus = status
			lastError = err
			fmt.Printf("  回调 #%d: 脚本=%s, 状态=%s, 错误=%v\n",
				callbackCount, scriptID, status, err)
		}),
	)

	// 尝试执行脚本
	fmt.Printf("\n尝试执行脚本 %s...\n", testScriptID)

	// 定义一个简单的执行器
	executor := func(content []byte, args map[string]interface{}) (string, error) {
		fmt.Printf("  执行器被调用: 内容长度=%d, 参数=%v\n", len(content), args)
		return "执行成功", nil
	}

	result, err := scriptManager.ExecuteScript(testScriptID, map[string]interface{}{"test": true}, executor)
	if err != nil {
		t.Fatalf("执行脚本失败: %v", err)
	}
	fmt.Printf("执行结果: %s\n", result)

	fmt.Printf("\n回调统计:\n")
	fmt.Printf("  总回调次数: %d\n", callbackCount)
	fmt.Printf("  最后状态: %s\n", lastStatus)
	fmt.Printf("  最后错误: %v\n", lastError)
	if callbackCount == 0 {
		t.Fatal("执行回调未触发")
	}
	if lastStatus != "success" || lastError != nil {
		t.Fatalf("执行回调状态异常: status=%s err=%v", lastStatus, lastError)
	}

	fmt.Println("\n安全脚本 - 执行回调测试通过!")
}

// TestIntegration_SecureScript_ExecuteWithMockScript 测试使用模拟脚本执行
func TestIntegration_SecureScript_ExecuteWithMockScript(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 模拟脚本执行 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	scriptManager, testScriptID := requireAvailableSecureScript(t, client)

	// 尝试执行脚本（使用不同的执行器）
	// 执行器1: 返回成功
	fmt.Println("\n测试成功执行器...")
	successExecutor := func(content []byte, args map[string]interface{}) (string, error) {
		return fmt.Sprintf("处理了 %d 字节的内容", len(content)), nil
	}

	result, err := scriptManager.ExecuteScript(testScriptID, nil, successExecutor)
	if err != nil {
		t.Fatalf("成功执行器执行失败: %v", err)
	}
	fmt.Printf("执行结果: %s\n", result)

	// 执行器2: 返回错误
	fmt.Println("\n测试失败执行器...")
	failExecutor := func(content []byte, args map[string]interface{}) (string, error) {
		return "", fmt.Errorf("模拟执行错误")
	}

	result, err = scriptManager.ExecuteScript(testScriptID, nil, failExecutor)
	if err != nil {
		fmt.Printf("执行结果: 错误=%v (预期)\n", err)
	} else {
		t.Fatalf("失败执行器应该返回错误，实际结果: %s", result)
	}

	fmt.Println("\n安全脚本 - 模拟脚本执行测试通过!")
}

// TestIntegration_SecureScript_ManagerOptions 测试管理器选项
func TestIntegration_SecureScript_ManagerOptions(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 管理器选项 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
	)
	defer client.Close()

	// 测试不同的选项组合
	fmt.Println("\n测试基本创建...")
	manager1 := NewSecureScriptManager(client)
	if manager1 == nil {
		t.Error("创建基本管理器失败")
	}
	fmt.Println("基本管理器创建成功!")

	// 测试带 AppSecret
	fmt.Println("\n测试带 AppSecret...")
	manager2 := NewSecureScriptManager(client,
		WithAppSecret("my_secret_key"),
	)
	if manager2 == nil {
		t.Error("创建带密钥的管理器失败")
	}
	fmt.Println("带密钥的管理器创建成功!")

	// 测试带回调
	fmt.Println("\n测试带回调...")
	manager3 := NewSecureScriptManager(client,
		WithAppSecret("my_secret_key"),
		WithExecuteCallback(func(scriptID string, status string, err error) {
			fmt.Printf("  回调触发: 脚本=%s, 状态=%s\n", scriptID, status)
		}),
	)
	if manager3 == nil {
		t.Error("创建带回调的管理器失败")
	}
	fmt.Println("带回调的管理器创建成功!")

	// 注意: WithPublicKey 需要一个 *rsa.PublicKey，这里跳过

	fmt.Println("\n安全脚本 - 管理器选项测试通过!")
}

// TestIntegration_SecureScript_FullWorkflow 测试完整安全脚本工作流
func TestIntegration_SecureScript_FullWorkflow(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 完整工作流 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 步骤1: 登录
	fmt.Println("\n--- 步骤1: 登录 ---")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	fmt.Println("登录成功!")

	// 步骤2: 创建安全脚本管理器
	fmt.Println("\n--- 步骤2: 创建安全脚本管理器 ---")
	executionLog := []string{}
	scriptManager := NewSecureScriptManager(client,
		WithAppSecret("test_app_secret"),
		WithExecuteCallback(func(scriptID string, status string, err error) {
			log := fmt.Sprintf("脚本 %s: %s", scriptID, status)
			if err != nil {
				log += fmt.Sprintf(" (错误: %v)", err)
			}
			executionLog = append(executionLog, log)
		}),
	)
	fmt.Println("管理器创建成功!")

	// 步骤3: 获取可用脚本列表
	fmt.Println("\n--- 步骤3: 获取可用脚本列表 ---")
	versions, err := scriptManager.GetScriptVersions()
	if err != nil {
		t.Fatalf("获取脚本列表失败: %v", err)
	}
	fmt.Printf("可用脚本数: %d\n", len(versions))
	for _, v := range versions {
		fmt.Printf("  - %s (v%s)\n", v.Name, v.Version)
	}

	// 步骤4: 尝试获取和执行脚本
	fmt.Println("\n--- 步骤4: 获取和执行脚本 ---")
	if len(versions) > 0 {
		scriptID := versions[0].ScriptID
		fmt.Printf("尝试执行脚本: %s\n", scriptID)

		// 获取脚本
		script, err := scriptManager.FetchScript(scriptID)
		if err != nil {
			t.Fatalf("获取脚本失败: %v", err)
		} else {
			fmt.Printf("获取成功! 版本: %s\n", script.Version)

			// 执行脚本
			result, err := scriptManager.ExecuteScript(scriptID, nil,
				func(content []byte, args map[string]interface{}) (string, error) {
					// 模拟执行脚本内容
					return fmt.Sprintf("执行完成，处理了 %d 字节", len(content)), nil
				})
			if err != nil {
				t.Fatalf("执行失败: %v", err)
			} else {
				fmt.Printf("执行结果: %s\n", result)
			}
		}
	} else {
		fmt.Println("没有可用的脚本，跳过执行测试")

		// 测试获取不存在的脚本
		testScriptID := "non_existent_script"
		_, err := scriptManager.FetchScript(testScriptID)
		if err != nil {
			fmt.Printf("预期的错误: %v\n", err)
		} else {
			t.Fatalf("获取不存在的脚本应该失败: %s", testScriptID)
		}
	}

	// 步骤5: 检查执行日志
	fmt.Println("\n--- 步骤5: 执行日志 ---")
	fmt.Printf("执行日志条数: %d\n", len(executionLog))
	for i, log := range executionLog {
		fmt.Printf("  %d. %s\n", i+1, log)
	}

	// 步骤6: 清理
	fmt.Println("\n--- 步骤6: 清理 ---")
	scriptManager.ClearCache()
	fmt.Println("缓存已清理!")

	fmt.Println("\n========== 安全脚本完整工作流测试通过! ==========")
}

// TestIntegration_SecureScript_ConcurrentAccess 测试并发访问
func TestIntegration_SecureScript_ConcurrentAccess(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 并发访问 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	// 创建安全脚本管理器
	scriptManager := NewSecureScriptManager(client,
		WithAppSecret("test_app_secret"),
	)

	// 并发获取脚本版本
	fmt.Println("\n并发获取脚本版本...")
	done := make(chan bool, 5)
	errCh := make(chan error, 5)

	for i := 0; i < 5; i++ {
		go func(id int) {
			_, err := scriptManager.GetScriptVersions()
			if err != nil {
				fmt.Printf("  协程 %d: 错误 - %v\n", id, err)
				errCh <- err
			} else {
				fmt.Printf("  协程 %d: 成功\n", id)
				errCh <- nil
			}
			done <- true
		}(i)
	}

	// 等待所有协程完成
	for i := 0; i < 5; i++ {
		<-done
		if err := <-errCh; err != nil {
			t.Fatalf("并发获取脚本版本失败: %v", err)
		}
	}

	// 并发缓存操作
	fmt.Println("\n并发缓存操作...")
	for i := 0; i < 3; i++ {
		go func(id int) {
			scriptID := fmt.Sprintf("concurrent_script_%d", id)
			scriptManager.GetCachedScript(scriptID)
			fmt.Printf("  协程 %d: 缓存检查完成\n", id)
			done <- true
		}(i)
	}

	for i := 0; i < 3; i++ {
		<-done
	}

	// 并发清除缓存
	fmt.Println("\n并发清除缓存...")
	go func() {
		scriptManager.ClearCache()
		fmt.Println("  清除缓存完成")
		done <- true
	}()
	<-done

	fmt.Println("\n安全脚本 - 并发访问测试通过!")
}

// TestIntegration_SecureScript_ErrorHandling 测试错误处理
func TestIntegration_SecureScript_ErrorHandling(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 安全脚本 - 错误处理 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	// 先登录
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	// 测试1: 没有设置 AppSecret
	fmt.Println("\n测试1: 没有设置 AppSecret...")
	managerNoSecret := NewSecureScriptManager(client)
	_, err = managerNoSecret.FetchScript("test_script")
	if err != nil {
		fmt.Printf("预期的错误: %v\n", err)
	} else {
		t.Fatal("未设置 AppSecret 获取脚本应该失败")
	}

	// 测试2: 获取不存在的脚本
	fmt.Println("\n测试2: 获取不存在的脚本...")
	managerWithSecret := NewSecureScriptManager(client,
		WithAppSecret("test_secret"),
	)
	_, err = managerWithSecret.FetchScript("non_existent_script_12345")
	if err != nil {
		fmt.Printf("预期的错误: %v\n", err)
	} else {
		t.Fatal("获取不存在的脚本应该失败")
	}

	// 测试3: 执行不存在的脚本
	fmt.Println("\n测试3: 执行不存在的脚本...")
	_, err = managerWithSecret.ExecuteScript("non_existent_script", nil,
		func(content []byte, args map[string]interface{}) (string, error) {
			return "ok", nil
		})
	if err != nil {
		fmt.Printf("预期的错误: %v\n", err)
	} else {
		t.Fatal("执行不存在的脚本应该失败")
	}

	// 测试4: 获取空脚本ID的缓存
	fmt.Println("\n测试4: 获取空脚本ID的缓存...")
	cached := managerWithSecret.GetCachedScript("")
	if cached == nil {
		fmt.Println("返回 nil (预期)")
	}

	fmt.Println("\n安全脚本 - 错误处理测试通过!")
}
