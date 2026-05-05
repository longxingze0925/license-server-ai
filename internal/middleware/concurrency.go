package middleware

import (
	"net/http"

	"license-server/internal/model"
	"license-server/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// ConcurrencyMiddleware 限制单用户的同时跑中任务数。
//
// 仅作用于"会建 generation_tasks 行的入口"——即 POST /api/proxy/:provider/generate。
// chat 这种同步且即返的入口豁免（不进 generation_tasks running 队列）。
//
// 计数语义：当前用户 status IN (pending, running) 的任务数；
//
//	user_credits.concurrent_limit == 0 视为不限。
//
// 数据库直接 COUNT；任务量起来后可以换 Redis。
func ConcurrencyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 仅拦 POST .../generate
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		path := c.FullPath()
		// FullPath() 在路由注册时是 "/api/proxy/:provider/generate"
		if !endsWithGenerate(path) {
			c.Next()
			return
		}

		userID := GetUserID(c)
		if userID == "" {
			c.Next() // 未登录由后续 auth 中间件拦
			return
		}

		var credit model.UserCredit
		if err := model.DB.First(&credit, "user_id = ?", userID).Error; err != nil {
			// 余额行不存在 → 视为 limit=1（首次使用），后面 reserve 会自动建行
			credit.ConcurrentLimit = 1
		}

		if credit.ConcurrentLimit <= 0 {
			c.Next() // 0 = 不限
			return
		}

		var running int64
		if err := model.DB.Model(&model.GenerationTask{}).
			Where("user_id = ? AND status IN ?", userID, []model.GenerationStatus{
				model.GenerationPending,
				model.GenerationRunning,
			}).
			Count(&running).Error; err != nil {
			response.ServerError(c, "并发计数失败: "+err.Error())
			c.Abort()
			return
		}

		if running >= int64(credit.ConcurrentLimit) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":             429,
				"message":          "超过并发上限",
				"running":          running,
				"concurrent_limit": credit.ConcurrentLimit,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func endsWithGenerate(path string) bool {
	const suffix = "/generate"
	if len(path) < len(suffix) {
		return false
	}
	return path[len(path)-len(suffix):] == suffix
}
