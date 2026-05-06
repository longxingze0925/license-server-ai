package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"license-server/internal/adapter"
	"license-server/internal/config"
	"license-server/internal/handler"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/utils"
	"license-server/internal/service"
	"license-server/internal/worker"

	"github.com/gin-gonic/gin"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	migrate := flag.Bool("migrate", false, "执行数据库 AutoMigrate 后退出")
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

	if *migrate {
		log.Println("执行数据库 AutoMigrate...")
		if err := model.AutoMigrate(); err != nil {
			log.Fatalf("数据库迁移失败: %v", err)
		}
		log.Println("数据库迁移完成")
		os.Exit(0)
	}

	// 初始化管理员账号
	if *initAdmin {
		log.Println("初始化管理员账号...")

		adminEmail := os.Getenv("INIT_ADMIN_EMAIL")
		if adminEmail == "" {
			adminEmail = "admin@example.com"
		}
		adminPassword := os.Getenv("INIT_ADMIN_PASSWORD")
		generatedPassword := false
		if adminPassword == "" {
			adminPassword = "Admin@" + utils.GenerateRandomString(20) + "1"
			generatedPassword = true
		}

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
		log.Printf("邮箱: %s", adminEmail)
		if generatedPassword {
			log.Printf("一次性初始密码: %s", adminPassword)
			log.Println("【重要提示】请登录后立即修改初始密码！")
		} else {
			log.Println("初始密码来自 INIT_ADMIN_PASSWORD，不在日志中输出")
		}
		os.Exit(0)
	}

	if err := service.NewPricingService().EnsureDefaultRules(); err != nil {
		log.Printf("[warn] 初始化默认计价规则失败: %v", err)
	} else {
		log.Println("默认计价规则检查完成")
	}

	// 创建存储目录
	os.MkdirAll(cfg.Storage.ScriptsDir, 0755)
	os.MkdirAll(cfg.Storage.ReleasesDir, 0755)
	os.MkdirAll(cfg.Storage.ReleasesDir+"/hotupdate", 0755) // 热更新目录
	os.MkdirAll("logs", 0755)

	// 初始化 AI 生成结果文件存储（默认本机磁盘 storage/generations）
	fileStorage, err := service.NewLocalFSProvider(cfg.Storage.GenerationsRoot)
	if err != nil {
		log.Fatalf("初始化文件存储失败: %v", err)
	}
	service.InitFileStorage(fileStorage)
	log.Printf("文件存储就绪：%s（保留 %d 天）", fileStorage.Root(), cfg.Storage.GenerationKeepDays)

	// 初始化 AI Provider 凭证服务（信封加密主密钥）
	// 启动失败立即 panic：防止用错主密钥把数据库写废。
	// 部署须知：在环境变量 LICENSE_MASTER_KEY 设置 base64 编码的 32 字节主密钥。
	//   生成命令：openssl rand -base64 32
	//   或调用 crypto.GenerateMasterKeyBase64()
	masterKeyProvider := crypto.NewEnvMasterKeyProvider()
	credSvc, err := service.InitProviderCredentialService(masterKeyProvider)
	if err != nil {
		log.Fatalf("初始化 Provider 凭证服务失败: %v", err)
	}
	if err := credSvc.SelfCheck(); err != nil {
		log.Fatalf("Provider 凭证服务自检失败: %v", err)
	}
	log.Println("Provider 凭证服务初始化完成（主密钥已加载）")

	// 余额完整性自检：balance 必须等于 sum(transactions.amount)。
	// 不阻塞启动；发现问题日志告警，让运维介入。
	if issues, err := service.NewCreditService().CheckIntegrity(); err != nil {
		log.Printf("[warn] 余额完整性自检失败: %v", err)
	} else if len(issues) > 0 {
		log.Printf("[warn] 余额完整性自检发现 %d 个用户不一致：", len(issues))
		for _, iss := range issues {
			log.Printf("  user_id=%s stored=%d sum_tx=%d (差额=%d)",
				iss.UserID, iss.StoredBalance, iss.SumOfTxAmount, iss.StoredBalance-iss.SumOfTxAmount)
		}
	} else {
		log.Println("余额完整性自检通过（balance == sum(tx) 全部一致）")
	}

	// 启动异步任务轮询 worker
	asyncRunner := service.NewAsyncRunnerService(
		service.NewPricingService(),
		service.NewCreditService(),
		credSvc,
		service.NewGenerationFileService(),
		adapter.NewAsyncRegistry(),
	)
	asyncPoller := worker.NewAsyncPoller(asyncRunner, time.Second)
	asyncPoller.Start()
	log.Println("异步任务轮询 worker 已启动（1s 扫描，到点任务最长 5s 查询上游）")
	defer asyncPoller.Stop()

	// 启动过期文件清理 worker（每小时一次）
	fileCleaner := worker.NewFileCleaner(service.NewGenerationFileService(), time.Hour, 200)
	fileCleaner.Start()
	log.Println("过期文件清理 worker 已启动（1h 间隔）")
	defer fileCleaner.Stop()

	// 创建 Gin 引擎
	r := gin.New()
	if err := configureTrustedProxies(r, cfg.Security.TrustedProxies); err != nil {
		log.Fatalf("配置可信代理失败: %v", err)
	}
	if cfg.Security.MultipartMemoryMB > 0 {
		r.MaxMultipartMemory = int64(cfg.Security.MultipartMemoryMB) << 20
	}

	// 设置路由
	handler.SetupRouter(r, asyncRunner)

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("服务器启动在 http://%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

func configureTrustedProxies(r *gin.Engine, proxies []string) error {
	if len(proxies) == 0 {
		return r.SetTrustedProxies(nil)
	}
	return r.SetTrustedProxies(proxies)
}
