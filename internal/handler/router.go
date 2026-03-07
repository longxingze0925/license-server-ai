package handler

import (
	"license-server/internal/config"
	"license-server/internal/middleware"
	"time"

	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter(r *gin.Engine) {
	cfg := config.Get()

	// 全局中间件
	if cfg.Security.MaxRequestBodyMB > 0 {
		r.Use(middleware.RequestBodyLimitMiddleware(int64(cfg.Security.MaxRequestBodyMB) << 20))
	}
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.LoggerMiddleware())
	r.Use(gin.Recovery())

	// 安全响应头
	if cfg.Security.EnableSecurityHeaders {
		r.Use(middleware.SecurityHeadersMiddleware())
	}

	// 速率限制器
	limiter := middleware.NewRateLimiter(100, time.Minute)          // 普通接口：每分钟100次
	authLimiter := middleware.NewRateLimiter(10, time.Minute)       // 认证接口：每分钟10次
	clientLimiter := middleware.NewRateLimiter(30, time.Minute)     // 客户端接口：每分钟30次
	clientAuthLimiter := middleware.NewRateLimiter(5, time.Minute)  // 客户端认证：每分钟5次
	heartbeatLimiter := middleware.NewRateLimiter(120, time.Minute) // 心跳接口：每分钟120次

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// API 路由组
	api := r.Group("/api")
	api.Use(middleware.RateLimitMiddleware(limiter))

	// API 健康检查（供 Docker/K8s 使用）
	api.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "license-server"})
	})

	// 初始化 Handler
	authHandler := NewAuthHandler()
	tenantHandler := NewTenantHandler()
	teamMemberHandler := NewTeamMemberHandler()
	customerHandler := NewCustomerHandler()
	appHandler := NewApplicationHandler()
	licenseHandler := NewLicenseHandler()
	clientHandler := NewClientHandler()
	scriptHandler := NewScriptHandler()
	releaseHandler := NewReleaseHandler()
	deviceHandler := NewDeviceHandler()
	statsHandler := NewStatisticsHandler()
	auditHandler := NewAuditHandler()
	exportHandler := NewExportHandler()
	hotUpdateHandler := NewHotUpdateHandler()
	secureScriptHandler := NewSecureScriptHandler()
	wsHandler := NewWebSocketHandler()
	dataSyncHandler := NewDataSyncHandler()
	clientSyncHandler := NewClientSyncHandler()

	// ==================== 公开接口 ====================
	// 用户认证（更严格的速率限制）
	auth := api.Group("/auth")
	auth.Use(middleware.RateLimitMiddleware(authLimiter))
	{
		auth.POST("/register", authHandler.Register)                // 注册新租户
		auth.POST("/login", authHandler.Login)                      // 团队成员登录
		auth.POST("/accept-invite", teamMemberHandler.AcceptInvite) // 接受邀请
	}

	// ==================== 客户端接口 ====================
	client := api.Group("/client")
	client.Use(middleware.RateLimitMiddleware(clientLimiter))
	{
		// 授权码模式（认证接口使用更严格限制）
		clientAuth := client.Group("/auth")
		clientAuth.Use(middleware.RateLimitMiddleware(clientAuthLimiter))
		{
			clientAuth.POST("/activate", clientHandler.Activate)
			clientAuth.POST("/deactivate", clientHandler.Deactivate)
			clientAuth.POST("/register", clientHandler.ClientRegister)
			clientAuth.POST("/login", clientHandler.ClientLogin)
		}

		// 验证接口（中等限制）
		client.POST("/auth/verify", clientHandler.Verify)
		client.POST("/subscription/verify", clientHandler.SubscriptionVerify)

		// 心跳接口（较宽松限制）
		heartbeat := client.Group("")
		heartbeat.Use(middleware.RateLimitMiddleware(heartbeatLimiter))
		{
			heartbeat.POST("/auth/heartbeat", clientHandler.Heartbeat)
			heartbeat.POST("/subscription/heartbeat", clientHandler.SubscriptionHeartbeat)
		}

		// 脚本相关
		client.GET("/scripts/version", clientHandler.GetScriptVersion)
		client.GET("/scripts/:filename", clientHandler.DownloadScript)

		// 版本更新
		client.GET("/releases/latest", clientHandler.GetLatestRelease)
		client.GET("/releases/download/:filename", releaseHandler.DownloadRelease)

		// 热更新
		client.GET("/hotupdate/check", hotUpdateHandler.CheckUpdate)
		client.GET("/hotupdate/download/:filename", hotUpdateHandler.DownloadUpdate)
		client.POST("/hotupdate/report", hotUpdateHandler.ReportUpdateStatus)
		client.GET("/hotupdate/history", hotUpdateHandler.GetUpdateHistory)

		// 安全脚本
		client.GET("/secure-scripts/versions", secureScriptHandler.ClientGetVersions)
		client.POST("/secure-scripts/fetch", secureScriptHandler.ClientFetchScript)
		client.POST("/secure-scripts/report", secureScriptHandler.ClientReportExecution)

		// 客户端数据备份同步
		clientBackup := client.Group("/backup")
		{
			clientBackup.POST("/push", clientSyncHandler.Push)
			clientBackup.GET("/pull", clientSyncHandler.Pull)
		}

		// WebSocket 连接
		client.GET("/ws", wsHandler.HandleWebSocket)

		// 数据同步 API
		sync := client.Group("/sync")
		{
			// 核心同步接口
			sync.GET("/changes", dataSyncHandler.GetChanges)
			sync.POST("/push", dataSyncHandler.PushChanges)
			sync.POST("/conflict/resolve", dataSyncHandler.ResolveConflict)
			sync.GET("/status", dataSyncHandler.GetSyncStatus)

			// 配置数据
			sync.GET("/configs", dataSyncHandler.GetConfigs)
			sync.POST("/configs", dataSyncHandler.SaveConfig)

			// 工作流数据
			sync.GET("/workflows", dataSyncHandler.GetWorkflows)
			sync.POST("/workflows", dataSyncHandler.SaveWorkflow)
			sync.DELETE("/workflows/:id", dataSyncHandler.DeleteWorkflow)

			// 素材数据
			sync.GET("/materials", dataSyncHandler.GetMaterials)
			sync.POST("/materials", dataSyncHandler.SaveMaterial)
			sync.POST("/materials/batch", dataSyncHandler.SaveMaterialsBatch)

			// 帖子数据
			sync.GET("/posts", dataSyncHandler.GetPosts)
			sync.POST("/posts/batch", dataSyncHandler.SavePostsBatch)
			sync.PUT("/posts/:id/status", dataSyncHandler.UpdatePostStatus)
			sync.GET("/posts/groups", dataSyncHandler.GetPostGroups)

			// 评论话术
			sync.GET("/comment-scripts", dataSyncHandler.GetCommentScripts)
			sync.POST("/comment-scripts/batch", dataSyncHandler.SaveCommentScriptsBatch)

			// 通用表数据同步
			sync.GET("/tables", dataSyncHandler.GetTableList)
			sync.GET("/tables/all", dataSyncHandler.SyncAllTables)
			sync.GET("/table", dataSyncHandler.GetTableData)
			sync.POST("/table", dataSyncHandler.SaveTableData)
			sync.POST("/table/batch", dataSyncHandler.SaveTableDataBatch)
			sync.DELETE("/table", dataSyncHandler.DeleteTableData)
		}
	}

	// ==================== 需要认证的接口 ====================
	authenticated := api.Group("")
	authenticated.Use(middleware.AuthMiddleware())
	authenticated.Use(middleware.TenantMiddleware())
	{
		// 用户信息
		authenticated.GET("/auth/profile", authHandler.GetProfile)
		authenticated.PUT("/auth/password", authHandler.ChangePassword)

		// 租户管理
		tenant := authenticated.Group("/tenant")
		{
			tenant.GET("", tenantHandler.Get)
			tenant.PUT("", middleware.PermissionMiddleware("tenant:update"), tenantHandler.Update)
			tenant.DELETE("", middleware.OwnerMiddleware(), tenantHandler.Delete)
		}

		// 团队成员管理
		team := authenticated.Group("/team")
		{
			team.GET("/members", teamMemberHandler.List)
			team.GET("/members/:id", teamMemberHandler.Get)
			team.POST("/members", middleware.PermissionMiddleware("member:invite"), teamMemberHandler.Create)
			team.PUT("/members/:id", teamMemberHandler.Update)
			team.POST("/members/:id/reset-password", teamMemberHandler.ResetPassword)
			team.PUT("/members/:id/role", middleware.PermissionMiddleware("member:update"), teamMemberHandler.UpdateRole)
			team.DELETE("/members/:id", middleware.PermissionMiddleware("member:delete"), teamMemberHandler.Remove)
		}
	}

	// ==================== 管理后台接口 ====================
	admin := api.Group("/admin")
	admin.Use(middleware.AuthMiddleware())
	admin.Use(middleware.TenantMiddleware())
	admin.Use(middleware.AuditMiddleware())
	{
		// 统计数据
		admin.GET("/statistics/dashboard", statsHandler.Dashboard)
		admin.GET("/statistics/apps/:app_id", statsHandler.AppStatistics)
		admin.GET("/statistics/license-trend", statsHandler.LicenseTrend)
		admin.GET("/statistics/device-trend", statsHandler.DeviceTrend)
		admin.GET("/statistics/heartbeat-trend", statsHandler.HeartbeatTrend)
		admin.GET("/statistics/license-type", statsHandler.LicenseTypeDistribution)
		admin.GET("/statistics/device-os", statsHandler.DeviceOSDistribution)

		// 客户管理
		customers := admin.Group("/customers")
		{
			customers.POST("", middleware.PermissionMiddleware("customer:create"), customerHandler.Create)
			customers.GET("", customerHandler.List)
			customers.GET("/:id", customerHandler.Get)
			customers.PUT("/:id", middleware.PermissionMiddleware("customer:update"), customerHandler.Update)
			customers.DELETE("/:id", middleware.PermissionMiddleware("customer:delete"), customerHandler.Delete)
			customers.POST("/:id/disable", middleware.PermissionMiddleware("customer:update"), customerHandler.Disable)
			customers.POST("/:id/enable", middleware.PermissionMiddleware("customer:update"), customerHandler.Enable)
			customers.POST("/:id/reset-password", middleware.PermissionMiddleware("customer:update"), customerHandler.ResetPassword)
			customers.GET("/:id/licenses", customerHandler.GetLicenses)
			customers.GET("/:id/subscriptions", customerHandler.GetSubscriptions)
			customers.GET("/:id/devices", customerHandler.GetDevices)
		}

		// 应用管理
		apps := admin.Group("/apps")
		{
			apps.POST("", middleware.PermissionMiddleware("app:create"), appHandler.Create)
			apps.GET("", appHandler.List)
			apps.GET("/:id", appHandler.Get)
			apps.PUT("/:id", middleware.PermissionMiddleware("app:update"), appHandler.Update)
			apps.DELETE("/:id", middleware.PermissionMiddleware("app:delete"), appHandler.Delete)
			apps.POST("/:id/regenerate-keys", middleware.PermissionMiddleware("app:update"), appHandler.RegenerateKeys)

			// 应用脚本
			apps.POST("/:id/scripts", middleware.PermissionMiddleware("app:update"), scriptHandler.Upload)
			apps.GET("/:id/scripts", scriptHandler.List)

			// 应用版本
			apps.POST("/:id/releases", middleware.PermissionMiddleware("app:update"), releaseHandler.Create)
			apps.POST("/:id/releases/upload", middleware.PermissionMiddleware("app:update"), releaseHandler.Upload)
			apps.GET("/:id/releases", releaseHandler.List)

			// 热更新管理
			apps.POST("/:id/hotupdate", middleware.PermissionMiddleware("app:update"), hotUpdateHandler.Create)
			apps.POST("/:id/hotupdate/:hotupdate_id/upload", middleware.PermissionMiddleware("app:update"), hotUpdateHandler.Upload)
			apps.GET("/:id/hotupdate", hotUpdateHandler.List)
			apps.GET("/:id/hotupdate/stats", hotUpdateHandler.GetStats)

			// 安全脚本管理
			apps.POST("/:id/secure-scripts", middleware.PermissionMiddleware("app:update"), secureScriptHandler.Create)
			apps.GET("/:id/secure-scripts", secureScriptHandler.List)
			apps.GET("/:id/secure-scripts/stats", secureScriptHandler.GetStats)

			// 在线设备 (WebSocket)
			apps.GET("/:id/online-devices", wsHandler.GetOnlineDevices)
		}

		// 脚本管理
		scripts := admin.Group("/scripts")
		{
			scripts.GET("/:id", scriptHandler.Get)
			scripts.PUT("/:id", middleware.PermissionMiddleware("app:update"), scriptHandler.Update)
			scripts.DELETE("/:id", middleware.PermissionMiddleware("app:delete"), scriptHandler.Delete)
			scripts.GET("/:id/download", scriptHandler.Download)
		}

		// 版本管理
		releases := admin.Group("/releases")
		{
			releases.GET("/:id", releaseHandler.Get)
			releases.PUT("/:id", middleware.PermissionMiddleware("app:update"), releaseHandler.Update)
			releases.POST("/:id/publish", middleware.PermissionMiddleware("app:update"), releaseHandler.Publish)
			releases.POST("/:id/deprecate", middleware.PermissionMiddleware("app:update"), releaseHandler.Deprecate)
			releases.DELETE("/:id", middleware.PermissionMiddleware("app:delete"), releaseHandler.Delete)
		}

		// 热更新管理
		hotupdate := admin.Group("/hotupdate")
		{
			hotupdate.GET("/:id", hotUpdateHandler.Get)
			hotupdate.PUT("/:id", middleware.PermissionMiddleware("app:update"), hotUpdateHandler.Update)
			hotupdate.POST("/:id/publish", middleware.PermissionMiddleware("app:update"), hotUpdateHandler.Publish)
			hotupdate.POST("/:id/deprecate", middleware.PermissionMiddleware("app:update"), hotUpdateHandler.Deprecate)
			hotupdate.POST("/:id/rollback", middleware.PermissionMiddleware("app:update"), hotUpdateHandler.Rollback)
			hotupdate.DELETE("/:id", middleware.PermissionMiddleware("app:delete"), hotUpdateHandler.Delete)
			hotupdate.GET("/:id/logs", hotUpdateHandler.GetLogs)
		}

		// 安全脚本管理
		secureScripts := admin.Group("/secure-scripts")
		{
			secureScripts.GET("/:id", secureScriptHandler.Get)
			secureScripts.PUT("/:id", middleware.PermissionMiddleware("app:update"), secureScriptHandler.Update)
			secureScripts.POST("/:id/content", middleware.PermissionMiddleware("app:update"), secureScriptHandler.UpdateContent)
			secureScripts.POST("/:id/publish", middleware.PermissionMiddleware("app:update"), secureScriptHandler.Publish)
			secureScripts.POST("/:id/deprecate", middleware.PermissionMiddleware("app:update"), secureScriptHandler.Deprecate)
			secureScripts.DELETE("/:id", middleware.PermissionMiddleware("app:delete"), secureScriptHandler.Delete)
			secureScripts.GET("/:id/deliveries", secureScriptHandler.GetDeliveries)
		}

		// 实时指令管理
		instructions := admin.Group("/instructions")
		{
			instructions.POST("/send", middleware.PermissionMiddleware("app:update"), wsHandler.SendInstruction)
			instructions.GET("", wsHandler.ListInstructions)
			instructions.GET("/:id", wsHandler.GetInstructionStatus)
		}

		// 授权管理
		licenses := admin.Group("/licenses")
		{
			licenses.POST("", middleware.PermissionMiddleware("license:create"), licenseHandler.Create)
			licenses.GET("", licenseHandler.List)
			licenses.GET("/:id", licenseHandler.Get)
			licenses.PUT("/:id", middleware.PermissionMiddleware("license:update"), licenseHandler.Update)
			licenses.DELETE("/:id", middleware.PermissionMiddleware("license:delete"), licenseHandler.Delete)
			licenses.POST("/:id/renew", middleware.PermissionMiddleware("license:update"), licenseHandler.Renew)
			licenses.POST("/:id/revoke", middleware.PermissionMiddleware("license:update"), licenseHandler.Revoke)
			licenses.POST("/:id/suspend", middleware.PermissionMiddleware("license:update"), licenseHandler.Suspend)
			licenses.POST("/:id/resume", middleware.PermissionMiddleware("license:update"), licenseHandler.Resume)
			licenses.POST("/:id/reset-devices", middleware.PermissionMiddleware("license:update"), licenseHandler.ResetDevices)
		}

		// 订阅管理
		subscriptionHandler := NewSubscriptionHandler()
		subscriptions := admin.Group("/subscriptions")
		{
			subscriptions.POST("", middleware.PermissionMiddleware("subscription:create"), subscriptionHandler.Create)
			subscriptions.GET("", subscriptionHandler.List)
			subscriptions.GET("/:id", subscriptionHandler.Get)
			subscriptions.PUT("/:id", middleware.PermissionMiddleware("subscription:update"), subscriptionHandler.Update)
			subscriptions.DELETE("/:id", middleware.PermissionMiddleware("subscription:delete"), subscriptionHandler.Delete)
			subscriptions.POST("/:id/renew", middleware.PermissionMiddleware("subscription:update"), subscriptionHandler.Renew)
			subscriptions.POST("/:id/cancel", middleware.PermissionMiddleware("subscription:update"), subscriptionHandler.Cancel)
		}

		// 设备管理
		devices := admin.Group("/devices")
		{
			devices.GET("", deviceHandler.List)
			devices.GET("/:id", deviceHandler.Get)
			devices.DELETE("/:id", middleware.PermissionMiddleware("device:delete"), deviceHandler.Unbind)
			devices.POST("/:id/blacklist", middleware.PermissionMiddleware("device:update"), deviceHandler.Blacklist)
			devices.POST("/:id/unblacklist", middleware.PermissionMiddleware("device:update"), deviceHandler.Unblacklist)
		}

		// 黑名单管理
		blacklist := admin.Group("/blacklist")
		{
			blacklist.GET("", deviceHandler.GetBlacklist)
			blacklist.DELETE("/:machine_id", middleware.PermissionMiddleware("device:update"), deviceHandler.RemoveFromBlacklist)
		}

		// 审计日志
		audit := admin.Group("/audit")
		audit.Use(middleware.PermissionMiddleware("audit:read"))
		{
			audit.GET("", auditHandler.List)
			audit.GET("/stats", auditHandler.GetStats)
			audit.GET("/:id", auditHandler.Get)
		}

		// 数据备份管理
		backups := admin.Group("/backups")
		{
			backups.GET("/users", clientSyncHandler.AdminListUsers)
			backups.GET("/users/:user_id", clientSyncHandler.AdminGetUserBackups)
			backups.GET("/:backup_id", clientSyncHandler.AdminGetBackupDetail)
			backups.POST("/:backup_id/set-current", clientSyncHandler.AdminSetCurrentVersion)
		}

		// 数据导出
		export := admin.Group("/export")
		export.Use(middleware.PermissionMiddleware("export:read"))
		{
			export.GET("/formats", exportHandler.GetExportFormats)
			export.GET("/licenses", exportHandler.ExportLicenses)
			export.GET("/devices", exportHandler.ExportDevices)
			export.GET("/customers", exportHandler.ExportCustomers)
			export.GET("/users", exportHandler.ExportCustomers) // 兼容旧前端路径
			export.GET("/audit-logs", exportHandler.ExportAuditLogs)
		}
	}
}
