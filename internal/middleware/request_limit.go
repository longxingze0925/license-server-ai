package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequestBodyLimitMiddleware 全局请求体大小限制。
func RequestBodyLimitMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes <= 0 {
			c.Next()
			return
		}

		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"code":    413,
				"message": "请求体过大",
			})
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
