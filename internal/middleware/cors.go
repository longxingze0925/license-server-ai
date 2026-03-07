package middleware

import (
	"license-server/internal/config"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware 跨域中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		allowedOrigins := []string{}
		if cfg := config.Get(); cfg != nil {
			allowedOrigins = cfg.Security.AllowedOrigins
		}

		allowOrigin, allowCredentials := resolveCORSOrigin(origin, allowedOrigins)
		if allowOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowOrigin)
			c.Header("Vary", "Origin")
		}
		if allowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Type")

		if c.Request.Method == "OPTIONS" {
			if origin != "" && allowOrigin == "" {
				c.AbortWithStatus(403)
				return
			}
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func resolveCORSOrigin(origin string, allowed []string) (string, bool) {
	normalizedOrigin := normalizeOrigin(origin)

	// 未配置时，默认放行，兼容旧行为。
	if len(allowed) == 0 {
		if normalizedOrigin == "" {
			return "*", false
		}
		return origin, true
	}

	hasWildcard := false
	for _, item := range allowed {
		normalizedAllowed := normalizeOrigin(item)
		if normalizedAllowed == "*" {
			hasWildcard = true
			continue
		}
		if normalizedOrigin != "" && normalizedAllowed == normalizedOrigin {
			return origin, true
		}
	}

	if hasWildcard {
		if normalizedOrigin == "" {
			return "*", false
		}
		return origin, true
	}

	return "", false
}

func normalizeOrigin(origin string) string {
	trimmed := strings.TrimSpace(strings.ToLower(origin))
	return strings.TrimRight(trimmed, "/")
}
