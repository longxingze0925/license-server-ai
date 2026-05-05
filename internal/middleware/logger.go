package middleware

import (
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// LoggerMiddleware 日志中间件
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		// 计算耗时
		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()

		path = sanitizeLogPath(path, query)

		log.Printf("[API] %d | %13v | %15s | %-7s %s",
			statusCode,
			latency,
			clientIP,
			method,
			path,
		)
	}
}

var sensitiveQueryKeys = map[string]struct{}{
	"access_token":  {},
	"api_key":       {},
	"api_token":     {},
	"client_secret": {},
	"key":           {},
	"refresh_token": {},
	"secret":        {},
	"token":         {},
}

func sanitizeLogPath(path, rawQuery string) string {
	if rawQuery == "" {
		return path
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return path + "?" + redactRawQuery(rawQuery)
	}

	for key := range values {
		if _, ok := sensitiveQueryKeys[strings.ToLower(key)]; ok {
			values.Set(key, "***")
		}
	}
	return path + "?" + values.Encode()
}

func redactRawQuery(rawQuery string) string {
	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		key, _, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		decodedKey, err := url.QueryUnescape(key)
		if err != nil {
			decodedKey = key
		}
		if _, ok := sensitiveQueryKeys[strings.ToLower(decodedKey)]; ok {
			parts[i] = key + "=***"
		}
	}
	return strings.Join(parts, "&")
}
