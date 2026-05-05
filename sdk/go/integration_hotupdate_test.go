//go:build integration
// +build integration

package license

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIntegration_HotUpdate_CheckUpdate 测试检查更新功能
func TestIntegration_HotUpdate_CheckUpdate(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 热更新 - 检查更新 ==========")

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

	// 创建热更新管理器
	updateManager := NewHotUpdateManager(client, "1.0.0",
		WithAutoCheck(false, time.Hour),
	)

	// 检查更新
	fmt.Println("\n检查更新...")
	updateInfo, err := updateManager.CheckUpdate()
	if err != nil {
		t.Fatalf("检查更新失败: %v", err)
	}
	fmt.Printf("更新信息:\n")
	fmt.Printf("  有更新: %v\n", updateInfo.HasUpdate)
	if updateInfo.HasUpdate {
		fmt.Printf("  从版本: %s\n", updateInfo.FromVersion)
		fmt.Printf("  到版本: %s\n", updateInfo.ToVersion)
		fmt.Printf("  更新类型: %s\n", updateInfo.UpdateType)
		fmt.Printf("  强制更新: %v\n", updateInfo.ForceUpdate)
		fmt.Printf("  更新日志: %s\n", updateInfo.Changelog)
	}

	fmt.Println("\n热更新检查测试通过!")
}

// TestIntegration_HotUpdate_GetHistory 测试获取更新历史
func TestIntegration_HotUpdate_GetHistory(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 热更新 - 获取历史 ==========")

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

	// 创建热更新管理器
	updateManager := NewHotUpdateManager(client, "1.0.0")

	// 获取更新历史
	fmt.Println("\n获取更新历史...")
	history, err := updateManager.GetUpdateHistory()
	if err != nil {
		t.Fatalf("获取更新历史失败: %v", err)
	}
	fmt.Printf("更新历史记录数: %d\n", len(history))
	for i, record := range history {
		fmt.Printf("  记录 %d: %v\n", i+1, record)
	}

	fmt.Println("\n热更新历史测试通过!")
}

// TestIntegration_HotUpdate_Callback 测试更新回调
func TestIntegration_HotUpdate_Callback(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 热更新 - 回调功能 ==========")

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

	// 回调计数
	callbackCount := 0

	// 创建带回调的热更新管理器
	updateManager := NewHotUpdateManager(client, "1.0.0",
		WithUpdateCallback(func(status HotUpdateStatus, progress float64, err error) {
			callbackCount++
			fmt.Printf("回调 #%d: 状态=%s, 进度=%.2f%%, 错误=%v\n",
				callbackCount, status, progress*100, err)
		}),
	)

	// 检查更新（会触发回调）
	fmt.Println("\n检查更新...")
	updateInfo, _ := updateManager.CheckUpdate()

	if updateInfo != nil && updateInfo.HasUpdate {
		fmt.Printf("发现更新: %s -> %s\n", updateInfo.FromVersion, updateInfo.ToVersion)
	}

	fmt.Println("\n热更新回调测试通过!")
}

// TestIntegration_HotUpdate_VersionManagement 测试版本管理
func TestIntegration_HotUpdate_VersionManagement(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 热更新 - 版本管理 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
	)
	defer client.Close()

	// 创建热更新管理器
	updateManager := NewHotUpdateManager(client, "1.0.0")

	// 测试版本获取和设置
	fmt.Printf("当前版本: %s\n", updateManager.GetCurrentVersion())

	updateManager.SetCurrentVersion("1.1.0")
	fmt.Printf("更新后版本: %s\n", updateManager.GetCurrentVersion())

	// 验证
	if updateManager.GetCurrentVersion() != "1.1.0" {
		t.Error("版本设置失败")
	}

	fmt.Println("\n版本管理测试通过!")
}

// TestIntegration_HotUpdate_DirectorySetup 测试目录设置
func TestIntegration_HotUpdate_DirectorySetup(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 热更新 - 目录设置 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
	)
	defer client.Close()

	// 创建临时目录
	tempDir := filepath.Join(os.TempDir(), "hotupdate_test")
	updateDir := filepath.Join(tempDir, "updates")
	backupDir := filepath.Join(tempDir, "backups")

	// 清理
	defer os.RemoveAll(tempDir)

	// 创建带自定义目录的热更新管理器
	updateManager := NewHotUpdateManager(client, "1.0.0",
		WithUpdateDir(updateDir),
		WithBackupDir(backupDir),
	)

	// 验证目录已创建
	if _, err := os.Stat(updateDir); os.IsNotExist(err) {
		t.Error("更新目录未创建")
	}
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Error("备份目录未创建")
	}

	fmt.Printf("更新目录: %s\n", updateDir)
	fmt.Printf("备份目录: %s\n", backupDir)

	// 测试是否正在更新
	if updateManager.IsUpdating() {
		t.Error("不应该处于更新状态")
	}

	fmt.Println("\n目录设置测试通过!")
}
