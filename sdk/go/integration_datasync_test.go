//go:build integration
// +build integration

package license

import (
	"fmt"
	"testing"
	"time"
)

func requireSyncResultSuccess(t *testing.T, result *SyncResult) {
	t.Helper()
	if result == nil {
		t.Fatal("同步结果为空")
	}
	if result.Status != "success" {
		t.Fatalf("同步结果状态异常: record=%s status=%s error=%s", result.RecordID, result.Status, result.Error)
	}
}

func requireBatchSyncResultSuccess(t *testing.T, result SyncResult) {
	t.Helper()
	if result.Status != "success" {
		t.Fatalf("批量同步结果状态异常: record=%s status=%s error=%s", result.RecordID, result.Status, result.Error)
	}
}

func containsSyncRecord(records []SyncRecord, recordID string) bool {
	for _, record := range records {
		if record.ID == recordID {
			return true
		}
	}
	return false
}

// TestIntegration_DataSync_GetTableList 测试获取表列表
func TestIntegration_DataSync_GetTableList(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 获取表列表 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 获取表列表
	fmt.Println("\n获取表列表...")
	tables, err := syncClient.GetTableList()
	if err != nil {
		t.Fatalf("获取表列表失败: %v", err)
	}
	fmt.Printf("表数量: %d\n", len(tables))
	for _, table := range tables {
		fmt.Printf("  表名: %s, 记录数: %d, 最后更新: %s\n",
			table.TableName, table.RecordCount, table.LastUpdated)
	}

	fmt.Println("\n数据同步 - 获取表列表测试通过!")
}

// TestIntegration_DataSync_PushAndPullRecord 测试推送和拉取记录
func TestIntegration_DataSync_PushAndPullRecord(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 推送和拉取记录 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 测试数据
	tableName := "test_configs"
	recordID := fmt.Sprintf("test_record_%d", time.Now().UnixNano())
	testData := map[string]interface{}{
		"key":   "test_key",
		"value": "test_value",
		"count": 42,
	}

	// 推送记录
	fmt.Printf("\n推送记录到表 %s...\n", tableName)
	result, err := syncClient.PushRecord(tableName, recordID, testData, 0)
	if err != nil {
		t.Fatalf("推送记录失败: %v", err)
	}
	requireSyncResultSuccess(t, result)
	fmt.Printf("推送成功! 状态: %s, 版本: %d\n", result.Status, result.Version)

	// 拉取记录
	fmt.Printf("\n从表 %s 拉取记录...\n", tableName)
	records, serverTime, err := syncClient.PullTable(tableName, 0)
	if err != nil {
		t.Fatalf("拉取记录失败: %v", err)
	}
	if !containsSyncRecord(records, recordID) {
		t.Fatalf("未拉取到刚推送的记录: %s", recordID)
	}
	fmt.Printf("拉取成功! 记录数: %d, 服务器时间: %d\n", len(records), serverTime)
	for i, record := range records {
		fmt.Printf("  记录 %d: ID=%s, 版本=%d\n", i+1, record.ID, record.Version)
	}

	fmt.Println("\n数据同步 - 推送和拉取记录测试通过!")
}

// TestIntegration_DataSync_BackupPushPull 测试备份推送和拉取
func TestIntegration_DataSync_BackupPushPull(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 备份推送和拉取 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
	)
	defer client.Close()

	fmt.Println("登录中...")
	_, err := client.Login(TestEmail, TestPassword)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	syncClient := client.NewDataSyncClient()
	dataJSON := fmt.Sprintf(`[{"id":"backup_%d","text":"hello"}]`, time.Now().UnixNano())
	if err := syncClient.PushBackup(DataTypeScripts, dataJSON, "Go Integration Device", 1); err != nil {
		t.Fatalf("推送备份失败: %v", err)
	}

	backups, err := syncClient.PullBackup(DataTypeScripts)
	if err != nil {
		t.Fatalf("拉取备份失败: %v", err)
	}
	if len(backups) == 0 {
		t.Fatal("备份列表为空")
	}
	if backups[0].DataType != DataTypeScripts {
		t.Fatalf("备份类型异常: got %s want %s", backups[0].DataType, DataTypeScripts)
	}

	fmt.Printf("备份拉取成功! 数量: %d, 最新版本: %d\n", len(backups), backups[0].Version)
	fmt.Println("\n数据同步 - 备份推送和拉取测试通过!")
}

// TestIntegration_DataSync_BatchPush 测试批量推送
func TestIntegration_DataSync_BatchPush(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 批量推送 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 准备批量数据
	tableName := "test_batch"
	records := []PushRecordItem{
		{
			RecordID: fmt.Sprintf("batch_1_%d", time.Now().UnixNano()),
			Data:     map[string]interface{}{"name": "Item 1", "value": 100},
			Version:  0,
		},
		{
			RecordID: fmt.Sprintf("batch_2_%d", time.Now().UnixNano()),
			Data:     map[string]interface{}{"name": "Item 2", "value": 200},
			Version:  0,
		},
		{
			RecordID: fmt.Sprintf("batch_3_%d", time.Now().UnixNano()),
			Data:     map[string]interface{}{"name": "Item 3", "value": 300},
			Version:  0,
		},
	}

	// 批量推送
	fmt.Printf("\n批量推送 %d 条记录到表 %s...\n", len(records), tableName)
	results, err := syncClient.PushRecordBatch(tableName, records)
	if err != nil {
		t.Fatalf("批量推送失败: %v", err)
	}
	if len(results) != len(records) {
		t.Fatalf("批量推送结果数不匹配: 期望 %d 实际 %d", len(records), len(results))
	}
	fmt.Printf("批量推送成功! 结果数: %d\n", len(results))
	for _, r := range results {
		requireBatchSyncResultSuccess(t, r)
		fmt.Printf("  记录 %s: 状态=%s, 版本=%d\n", r.RecordID, r.Status, r.Version)
	}

	fmt.Println("\n数据同步 - 批量推送测试通过!")
}

// TestIntegration_DataSync_DeleteRecord 测试删除记录
func TestIntegration_DataSync_DeleteRecord(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 删除记录 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 先创建一条记录
	tableName := "test_delete"
	recordID := fmt.Sprintf("delete_test_%d", time.Now().UnixNano())
	testData := map[string]interface{}{"name": "to_be_deleted"}

	fmt.Printf("\n先创建记录 %s...\n", recordID)
	result, err := syncClient.PushRecord(tableName, recordID, testData, 0)
	if err != nil {
		t.Fatalf("创建记录失败: %v", err)
	}
	requireSyncResultSuccess(t, result)
	fmt.Println("记录创建成功!")

	// 删除记录
	fmt.Printf("\n删除记录 %s...\n", recordID)
	err = syncClient.DeleteRecord(tableName, recordID)
	if err != nil {
		t.Fatalf("删除记录失败: %v", err)
	}
	fmt.Println("删除成功!")

	fmt.Println("\n数据同步 - 删除记录测试通过!")
}

// TestIntegration_DataSync_SyncTime 测试同步时间管理
func TestIntegration_DataSync_SyncTime(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 同步时间管理 ==========")

	client := NewClient(IntegrationServerURL, IntegrationAppKey,
		WithAppVersion("1.0.0"),
		WithSkipVerify(true),
	)
	defer client.Close()

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 测试设置和获取同步时间
	tableName := "test_table"
	testTime := time.Now().Unix()

	fmt.Printf("\n设置表 %s 的同步时间为 %d...\n", tableName, testTime)
	syncClient.SetLastSyncTime(tableName, testTime)

	gotTime := syncClient.GetLastSyncTime(tableName)
	fmt.Printf("获取到的同步时间: %d\n", gotTime)

	if gotTime != testTime {
		t.Errorf("同步时间不匹配: 期望 %d, 实际 %d", testTime, gotTime)
	}

	fmt.Println("\n数据同步 - 同步时间管理测试通过!")
}

// TestIntegration_DataSync_IncrementalSync 测试增量同步
func TestIntegration_DataSync_IncrementalSync(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 增量同步 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	tableName := "test_incremental"

	// 第一次全量拉取
	fmt.Println("\n第一次全量拉取...")
	records1, serverTime1, err := syncClient.PullTable(tableName, 0)
	if err != nil {
		t.Fatalf("全量拉取失败: %v", err)
	}
	fmt.Printf("全量拉取成功! 记录数: %d, 服务器时间: %d\n", len(records1), serverTime1)

	// 推送新记录
	recordID := fmt.Sprintf("incr_%d", time.Now().UnixNano())
	fmt.Printf("\n推送新记录 %s...\n", recordID)
	result, err := syncClient.PushRecord(tableName, recordID, map[string]interface{}{"data": "new"}, 0)
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	requireSyncResultSuccess(t, result)

	// 增量拉取
	fmt.Printf("\n增量拉取 (since=%d)...\n", serverTime1)
	records2, serverTime2, err := syncClient.PullTable(tableName, serverTime1)
	if err != nil {
		t.Fatalf("增量拉取失败: %v", err)
	}
	if !containsSyncRecord(records2, recordID) {
		t.Fatalf("增量拉取未包含刚推送的记录: %s", recordID)
	}
	fmt.Printf("增量拉取成功! 新记录数: %d, 服务器时间: %d\n", len(records2), serverTime2)

	fmt.Println("\n数据同步 - 增量同步测试通过!")
}

// TestIntegration_DataSync_SyncTableFromServer 测试便捷同步方法
func TestIntegration_DataSync_SyncTableFromServer(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 便捷同步方法 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	tableName := "test_convenient"

	// 使用便捷方法同步
	fmt.Printf("\n使用 SyncTableFromServer 同步表 %s...\n", tableName)
	updates, deletes, serverTime, err := syncClient.SyncTableFromServer(tableName, 0)
	if err != nil {
		t.Fatalf("同步表失败: %v", err)
	}
	fmt.Printf("同步成功!\n")
	fmt.Printf("  更新记录数: %d\n", len(updates))
	fmt.Printf("  删除记录数: %d\n", len(deletes))
	fmt.Printf("  服务器时间: %d\n", serverTime)

	fmt.Println("\n数据同步 - 便捷同步方法测试通过!")
}

// TestIntegration_DataSync_GetSyncStatus 测试获取同步状态
func TestIntegration_DataSync_GetSyncStatus(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 获取同步状态 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 获取同步状态
	fmt.Println("\n获取同步状态...")
	status, err := syncClient.GetSyncStatus()
	if err != nil {
		t.Fatalf("获取同步状态失败: %v", err)
	}
	fmt.Printf("同步状态:\n")
	fmt.Printf("  最后同步时间: %d\n", status.LastSyncTime)
	fmt.Printf("  待处理变更: %d\n", status.PendingChanges)
	fmt.Printf("  服务器时间: %d\n", status.ServerTime)
	fmt.Printf("  表状态: %v\n", status.TableStatus)

	fmt.Println("\n数据同步 - 获取同步状态测试通过!")
}

// TestIntegration_DataSync_PushAndGetChanges 测试推送和获取变更
func TestIntegration_DataSync_PushAndGetChanges(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 推送和获取变更 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 创建变更
	changeKey := fmt.Sprintf("change_%d", time.Now().UnixNano())
	changes := []SyncChange{
		{
			DataType:   DataTypeConfig,
			DataKey:    changeKey,
			Action:     "update",
			Table:      DataTypeConfig,
			RecordID:   changeKey,
			Operation:  "update",
			Data:       map[string]interface{}{"name": "test change"},
			ChangeTime: time.Now().Unix(),
		},
	}

	// 推送变更
	fmt.Printf("\n推送 %d 个变更...\n", len(changes))
	results, err := syncClient.PushChanges(changes)
	if err != nil {
		t.Fatalf("推送变更失败: %v", err)
	} else {
		fmt.Printf("推送成功! 结果数: %d\n", len(results))
	}
	if len(results) != 1 || results[0].Status != "success" {
		t.Fatalf("推送变更结果异常: %+v", results)
	}

	// 获取变更
	fmt.Println("\n获取变更...")
	gotChanges, serverTime, err := syncClient.GetChanges(0, []string{DataTypeConfig})
	if err != nil {
		t.Fatalf("获取变更失败: %v", err)
	} else {
		fmt.Printf("获取成功! 变更数: %d, 服务器时间: %d\n", len(gotChanges), serverTime)
	}
	foundChange := false
	for _, change := range gotChanges {
		if change.DataKey == changeKey {
			foundChange = true
			break
		}
	}
	if !foundChange {
		t.Fatalf("未拉取到刚推送的变更: %s", changeKey)
	}

	fmt.Println("\n数据同步 - 推送和获取变更测试通过!")
}

// TestIntegration_DataSync_AutoSyncManager 测试自动同步管理器
func TestIntegration_DataSync_AutoSyncManager(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 自动同步管理器 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 创建自动同步管理器
	tables := []string{"test_auto_1", "test_auto_2"}
	manager := syncClient.NewAutoSyncManager(tables, 5*time.Second)

	// 设置回调
	pullCount := 0
	manager.OnPull(func(tableName string, records []SyncRecord, deletes []string) error {
		pullCount++
		fmt.Printf("  拉取回调: 表=%s, 记录数=%d, 删除数=%d\n", tableName, len(records), len(deletes))
		return nil
	})

	conflictCount := 0
	manager.OnConflict(func(tableName string, result SyncResult) error {
		conflictCount++
		fmt.Printf("  冲突回调: 表=%s, 记录=%s\n", tableName, result.RecordID)
		return nil
	})

	// 启动自动同步
	fmt.Println("\n启动自动同步...")
	manager.Start()

	// 等待一次同步完成
	time.Sleep(2 * time.Second)

	// 手动触发同步
	fmt.Println("\n手动触发同步...")
	manager.SyncNow()

	// 停止自动同步
	fmt.Println("\n停止自动同步...")
	manager.Stop()

	fmt.Printf("\n拉取回调次数: %d\n", pullCount)
	fmt.Printf("冲突回调次数: %d\n", conflictCount)

	fmt.Println("\n数据同步 - 自动同步管理器测试通过!")
}

// TestIntegration_DataSync_Configs 测试配置数据同步
func TestIntegration_DataSync_Configs(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 配置数据 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 保存配置
	keySuffix := time.Now().UnixNano()
	configs := []ConfigData{
		{Key: fmt.Sprintf("theme_%d", keySuffix), Value: "dark", UpdatedAt: time.Now().Unix()},
		{Key: fmt.Sprintf("language_%d", keySuffix), Value: "zh-CN", UpdatedAt: time.Now().Unix()},
	}

	fmt.Printf("\n保存 %d 个配置...\n", len(configs))
	err = syncClient.SaveConfigs(configs)
	if err != nil {
		t.Fatalf("保存配置失败: %v", err)
	} else {
		fmt.Println("保存成功!")
	}

	// 获取配置
	fmt.Println("\n获取配置...")
	gotConfigs, serverTime, err := syncClient.GetConfigs(0)
	if err != nil {
		t.Fatalf("获取配置失败: %v", err)
	} else {
		fmt.Printf("获取成功! 配置数: %d, 服务器时间: %d\n", len(gotConfigs), serverTime)
		for _, cfg := range gotConfigs {
			fmt.Printf("  %s = %v\n", cfg.Key, cfg.Value)
		}
	}

	gotByKey := make(map[string]ConfigData)
	for _, cfg := range gotConfigs {
		gotByKey[cfg.Key] = cfg
	}
	for _, cfg := range configs {
		got, ok := gotByKey[cfg.Key]
		if !ok {
			t.Fatalf("未拉取到刚保存的配置: %s", cfg.Key)
		}
		if got.Value != cfg.Value {
			t.Fatalf("配置值不匹配: %s 期望 %v 实际 %v", cfg.Key, cfg.Value, got.Value)
		}
	}

	fmt.Println("\n数据同步 - 配置数据测试通过!")
}

// TestIntegration_DataSync_Workflows 测试工作流数据同步
func TestIntegration_DataSync_Workflows(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 工作流数据 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 保存工作流
	workflowID := fmt.Sprintf("wf_%d", time.Now().UnixNano())
	workflows := []WorkflowData{
		{
			ID:        workflowID,
			Name:      "测试工作流",
			Config:    map[string]interface{}{"steps": 3, "auto": true},
			Enabled:   true,
			UpdatedAt: time.Now().Unix(),
		},
	}

	fmt.Printf("\n保存工作流 %s...\n", workflowID)
	err = syncClient.SaveWorkflows(workflows)
	if err != nil {
		t.Fatalf("保存工作流失败: %v", err)
	} else {
		fmt.Println("保存成功!")
	}

	// 获取工作流
	fmt.Println("\n获取工作流...")
	gotWorkflows, serverTime, err := syncClient.GetWorkflows(0)
	if err != nil {
		t.Fatalf("获取工作流失败: %v", err)
	} else {
		fmt.Printf("获取成功! 工作流数: %d, 服务器时间: %d\n", len(gotWorkflows), serverTime)
	}

	foundWorkflow := false
	for _, workflow := range gotWorkflows {
		if workflow.ID == workflowID {
			foundWorkflow = true
			if workflow.Name != workflows[0].Name {
				t.Fatalf("工作流名称不匹配: 期望 %s 实际 %s", workflows[0].Name, workflow.Name)
			}
			break
		}
	}
	if !foundWorkflow {
		t.Fatalf("未拉取到刚保存的工作流: %s", workflowID)
	}

	// 删除工作流
	fmt.Printf("\n删除工作流 %s...\n", workflowID)
	err = syncClient.DeleteWorkflow(workflowID)
	if err != nil {
		t.Fatalf("删除工作流失败: %v", err)
	} else {
		fmt.Println("删除成功!")
	}

	fmt.Println("\n数据同步 - 工作流数据测试通过!")
}

// TestIntegration_DataSync_Materials 测试素材数据同步
func TestIntegration_DataSync_Materials(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 素材数据 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 保存素材
	materialID := time.Now().UnixNano()
	materials := []MaterialData{
		{
			ID:         fmt.Sprintf("%d", materialID),
			MaterialID: materialID,
			Type:       "text",
			Content:    "这是测试素材内容",
			Tags:       "test,demo",
			UpdatedAt:  time.Now().Unix(),
		},
	}

	fmt.Printf("\n保存 %d 个素材...\n", len(materials))
	err = syncClient.SaveMaterials(materials)
	if err != nil {
		t.Fatalf("保存素材失败: %v", err)
	} else {
		fmt.Println("保存成功!")
	}

	// 获取素材
	fmt.Println("\n获取素材...")
	gotMaterials, serverTime, err := syncClient.GetMaterials(0)
	if err != nil {
		t.Fatalf("获取素材失败: %v", err)
	} else {
		fmt.Printf("获取成功! 素材数: %d, 服务器时间: %d\n", len(gotMaterials), serverTime)
	}

	foundMaterial := false
	for _, material := range gotMaterials {
		if material.MaterialID == materialID || material.ID == materials[0].ID {
			foundMaterial = true
			if material.Content != materials[0].Content {
				t.Fatalf("素材内容不匹配: 期望 %s 实际 %s", materials[0].Content, material.Content)
			}
			break
		}
	}
	if !foundMaterial {
		t.Fatalf("未拉取到刚保存的素材: %d", materialID)
	}

	fmt.Println("\n数据同步 - 素材数据测试通过!")
}

// TestIntegration_DataSync_Posts 测试帖子数据同步
func TestIntegration_DataSync_Posts(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 帖子数据 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 保存帖子
	postID := fmt.Sprintf("post_%d", time.Now().UnixNano())
	posts := []PostData{
		{
			ID:        postID,
			Content:   "这是测试帖子内容",
			Status:    "unused",
			GroupID:   "default",
			UpdatedAt: time.Now().Unix(),
		},
	}

	fmt.Printf("\n保存帖子 %s...\n", postID)
	err = syncClient.SavePosts(posts)
	if err != nil {
		t.Fatalf("保存帖子失败: %v", err)
	} else {
		fmt.Println("保存成功!")
	}

	// 获取帖子
	fmt.Println("\n获取帖子...")
	gotPosts, serverTime, err := syncClient.GetPosts(0, "")
	if err != nil {
		t.Fatalf("获取帖子失败: %v", err)
	} else {
		fmt.Printf("获取成功! 帖子数: %d, 服务器时间: %d\n", len(gotPosts), serverTime)
	}

	foundPost := false
	for _, post := range gotPosts {
		if post.ID == postID {
			foundPost = true
			if post.Content != posts[0].Content {
				t.Fatalf("帖子内容不匹配: 期望 %s 实际 %s", posts[0].Content, post.Content)
			}
			break
		}
	}
	if !foundPost {
		t.Fatalf("未拉取到刚保存的帖子: %s", postID)
	}

	posts[0].Content = "这是更新后的测试帖子内容"
	fmt.Printf("\n重复保存帖子 %s 并更新内容...\n", postID)
	err = syncClient.SavePosts(posts)
	if err != nil {
		t.Fatalf("更新帖子内容失败: %v", err)
	}
	gotPosts, _, err = syncClient.GetPosts(0, "")
	if err != nil {
		t.Fatalf("获取更新后的帖子失败: %v", err)
	}

	foundUpdatedPost := false
	for _, post := range gotPosts {
		if post.ID == postID {
			foundUpdatedPost = true
			if post.Content != posts[0].Content {
				t.Fatalf("重复保存后帖子内容未更新: 期望 %s 实际 %s", posts[0].Content, post.Content)
			}
			break
		}
	}
	if !foundUpdatedPost {
		t.Fatalf("未拉取到更新后的帖子: %s", postID)
	}

	// 更新帖子状态
	fmt.Printf("\n更新帖子 %s 状态为 used...\n", postID)
	err = syncClient.UpdatePostStatus(postID, "used")
	if err != nil {
		t.Fatalf("更新帖子状态失败: %v", err)
	} else {
		fmt.Println("更新成功!")
	}

	// 获取帖子分组
	fmt.Println("\n获取帖子分组...")
	groups, err := syncClient.GetPostGroups()
	if err != nil {
		t.Fatalf("获取帖子分组失败: %v", err)
	} else {
		fmt.Printf("获取成功! 分组数: %d\n", len(groups))
		for _, g := range groups {
			fmt.Printf("  分组: %s (%s), 数量: %d\n", g.Name, g.ID, g.Count)
		}
	}

	foundGroup := false
	for _, group := range groups {
		if group.ID == posts[0].GroupID || group.Name == posts[0].GroupID {
			foundGroup = true
			break
		}
	}
	if !foundGroup {
		t.Fatalf("未拉取到刚保存帖子的分组: %s", posts[0].GroupID)
	}

	fmt.Println("\n数据同步 - 帖子数据测试通过!")
}

// TestIntegration_DataSync_CommentScripts 测试评论话术同步
func TestIntegration_DataSync_CommentScripts(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 评论话术 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 保存评论话术
	scripts := []CommentScriptData{
		{
			ID:        fmt.Sprintf("script_%d", time.Now().UnixNano()),
			Content:   "这是一条测试评论话术",
			Category:  "general",
			UpdatedAt: time.Now().Unix(),
		},
	}

	fmt.Printf("\n保存 %d 条评论话术...\n", len(scripts))
	err = syncClient.SaveCommentScripts(scripts)
	if err != nil {
		t.Fatalf("保存评论话术失败: %v", err)
	} else {
		fmt.Println("保存成功!")
	}

	// 获取评论话术
	fmt.Println("\n获取评论话术...")
	gotScripts, serverTime, err := syncClient.GetCommentScripts(0, "")
	if err != nil {
		t.Fatalf("获取评论话术失败: %v", err)
	} else {
		fmt.Printf("获取成功! 话术数: %d, 服务器时间: %d\n", len(gotScripts), serverTime)
	}

	foundScript := false
	for _, script := range gotScripts {
		if script.ID == scripts[0].ID {
			foundScript = true
			if script.Content != scripts[0].Content {
				t.Fatalf("评论话术内容不匹配: 期望 %s 实际 %s", scripts[0].Content, script.Content)
			}
			break
		}
	}
	if !foundScript {
		t.Fatalf("未拉取到刚保存的评论话术: %s", scripts[0].ID)
	}

	fmt.Println("\n数据同步 - 评论话术测试通过!")
}

// TestIntegration_DataSync_FullWorkflow 测试完整数据同步工作流
func TestIntegration_DataSync_FullWorkflow(t *testing.T) {
	requireIntegrationServer(t)
	fmt.Println("\n========== 集成测试: 数据同步 - 完整工作流 ==========")

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

	// 创建数据同步客户端
	syncClient := client.NewDataSyncClient()

	// 步骤2: 获取同步状态
	fmt.Println("\n--- 步骤2: 获取同步状态 ---")
	status, err := syncClient.GetSyncStatus()
	if err != nil {
		t.Fatalf("获取状态失败: %v", err)
	}
	fmt.Printf("服务器时间: %d\n", status.ServerTime)

	// 步骤3: 推送本地数据
	fmt.Println("\n--- 步骤3: 推送本地数据 ---")
	tableName := "test_workflow"
	localRecords := []map[string]interface{}{
		{"id": "local_1", "name": "Local Item 1"},
		{"id": "local_2", "name": "Local Item 2"},
	}
	results, err := syncClient.SyncTableToServer(tableName, localRecords, "id")
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if len(results) != len(localRecords) {
		t.Fatalf("推送结果数不匹配: 期望 %d 实际 %d", len(localRecords), len(results))
	}
	for _, result := range results {
		requireBatchSyncResultSuccess(t, result)
	}
	fmt.Printf("推送成功! 结果数: %d\n", len(results))

	// 步骤4: 从服务器拉取数据
	fmt.Println("\n--- 步骤4: 从服务器拉取数据 ---")
	updates, deletes, serverTime, err := syncClient.SyncTableFromServer(tableName, 0)
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	fmt.Printf("拉取成功! 更新: %d, 删除: %d\n", len(updates), len(deletes))
	syncClient.SetLastSyncTime(tableName, serverTime)

	// 步骤5: 验证同步时间
	fmt.Println("\n--- 步骤5: 验证同步时间 ---")
	lastSync := syncClient.GetLastSyncTime(tableName)
	fmt.Printf("最后同步时间: %d\n", lastSync)

	fmt.Println("\n========== 数据同步完整工作流测试通过! ==========")
}
