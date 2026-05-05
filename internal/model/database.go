package model

import (
	"license-server/internal/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDB 初始化数据库连接
func InitDB(cfg *config.DatabaseConfig) error {
	var logLevel logger.LogLevel
	if config.Get().Server.Mode == "debug" {
		logLevel = logger.Info
	} else {
		logLevel = logger.Silent
	}

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logLevel),
		DisableForeignKeyConstraintWhenMigrating: true, // 禁用外键约束检查
	})
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)

	DB = db
	return nil
}

// AutoMigrate 自动迁移数据库表
func AutoMigrate() error {
	return DB.AutoMigrate(
		// 多租户核心模型
		&Tenant{},
		&TeamMember{},
		&TeamInvitation{},
		&Customer{},
		// 应用管理
		&Application{},
		&AppRelease{},
		&Script{},
		// 热更新
		&HotUpdate{},
		&HotUpdateLog{},
		// 安全脚本
		&SecureScript{},
		&ScriptDelivery{},
		&RealtimeInstruction{},
		&RealtimeInstructionResult{},
		&DeviceConnection{},
		// 授权管理
		&License{},
		&LicenseEvent{},
		&Subscription{},
		// 设备管理
		&Device{},
		&DeviceBlacklist{},
		&Heartbeat{},
		// 通知与日志
		&Notification{},
		&Webhook{},
		&WebhookLog{},
		&AuditLog{},
		&PublishTask{},
		&Setting{},
		// 用户数据同步
		&UserConfig{},
		&UserWorkflow{},
		&UserBatchTask{},
		&UserMaterial{},
		&UserPost{},
		&UserComment{},
		&UserCommentScript{},
		&UserVoiceConfig{},
		&UserFile{},
		&UserTableData{},
		&SyncCheckpoint{},
		&SyncConflict{},
		&SyncLog{},
		// 客户端数据同步
		&ClientSyncData{},
		// 客户端会话
		&ClientSession{},
		// AI Provider 转发与计费
		&ProviderCredential{},
		&UserCredit{},
		&CreditTransaction{},
		&PricingRule{},
		&GenerationTask{},
		&GenerationFile{},
	)
}
