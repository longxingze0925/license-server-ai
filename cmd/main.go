package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"license-server/internal/config"
	"license-server/internal/handler"
	"license-server/internal/model"

	"github.com/gin-gonic/gin"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	migrate := flag.Bool("migrate", false, "是否执行数据库迁移")
	initAdmin := flag.Bool("init-admin", false, "初始化管理员账号")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 设置 Gin 模式
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化数据库
	if err := model.InitDB(&cfg.Database); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	log.Println("数据库连接成功")

	// 自动执行数据库迁移（确保表结构是最新的）
	log.Println("检查数据库表结构...")
	if err := model.AutoMigrate(); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	// 数据库迁移（仅迁移模式）
	if *migrate {
		log.Println("数据库迁移完成")
		os.Exit(0)
	}

	// 初始化管理员账号
	if *initAdmin {
		log.Println("初始化管理员账号...")

		adminEmail := "admin@example.com"
		adminPassword := "admin123"

		// 检查是否已存在（在 TeamMember 表中）
		var existingMember model.TeamMember
		if err := model.DB.Where("email = ?", adminEmail).First(&existingMember).Error; err == nil {
			log.Println("管理员账号已存在")
			os.Exit(0)
		}

		// 开始事务
		tx := model.DB.Begin()

		// 创建默认租户
		tenant := model.Tenant{
			Name:   "管理后台",
			Slug:   "default",
			Status: model.TenantStatusActive,
			Plan:   model.TenantPlanPro, // 使用 Pro 套餐，有更多配额
		}
		if err := tx.Create(&tenant).Error; err != nil {
			tx.Rollback()
			log.Fatalf("创建默认租户失败: %v", err)
		}

		// 创建管理员（作为 Owner）
		admin := model.TeamMember{
			TenantID: tenant.ID,
			Email:    adminEmail,
			Name:     "管理员",
			Role:     model.RoleOwner, // 设置为 Owner，拥有最高权限
			Status:   model.MemberStatusActive,
		}
		if err := admin.SetPassword(adminPassword); err != nil {
			tx.Rollback()
			log.Fatalf("密码加密失败: %v", err)
		}

		if err := tx.Create(&admin).Error; err != nil {
			tx.Rollback()
			log.Fatalf("创建管理员失败: %v", err)
		}

		tx.Commit()

		log.Println("管理员账号创建成功!")
		log.Println("邮箱: admin@example.com")
		log.Println("密码: admin123")
		log.Println("")
		log.Println("【重要提示】请登录后立即修改默认密码！")
		os.Exit(0)
	}

	// 创建存储目录
	os.MkdirAll(cfg.Storage.ScriptsDir, 0755)
	os.MkdirAll(cfg.Storage.ReleasesDir, 0755)
	os.MkdirAll(cfg.Storage.ReleasesDir+"/hotupdate", 0755) // 热更新目录
	os.MkdirAll("logs", 0755)

	// 创建 Gin 引擎
	r := gin.New()
	if cfg.Security.MultipartMemoryMB > 0 {
		r.MaxMultipartMemory = int64(cfg.Security.MultipartMemoryMB) << 20
	}

	// 设置路由
	handler.SetupRouter(r)

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("服务器启动在 http://%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
