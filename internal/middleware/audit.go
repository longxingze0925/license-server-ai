package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"license-server/internal/model"
	"mime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxAuditRequestBodyBytes int64 = 64 << 10

// AuditMiddleware 审计日志中间件
func AuditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过不需要记录的路径
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/client/") ||
			strings.HasPrefix(path, "/health") ||
			strings.Contains(path, "/statistics/") {
			c.Next()
			return
		}

		// 只记录写操作
		method := c.Request.Method
		if method == "GET" {
			c.Next()
			return
		}

		startTime := time.Now()

		requestBody := captureAuditRequestBody(c)

		// 处理请求
		c.Next()

		// 记录日志
		duration := time.Since(startTime).Milliseconds()

		tenantID, _ := c.Get("tenant_id")
		userID, _ := c.Get("user_id")
		userEmail, _ := c.Get("email")

		action, resource, resourceID := parseActionFromPath(method, path)

		log := model.AuditLog{
			TenantID:     toString(tenantID),
			UserID:       toString(userID),
			UserEmail:    toString(userEmail),
			Action:       action,
			Resource:     resource,
			ResourceID:   resourceID,
			Description:  generateDescription(action, resource),
			IPAddress:    c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
			RequestBody:  truncateString(requestBody, 2000),
			ResponseCode: c.Writer.Status(),
			Duration:     duration,
		}

		// 异步写入日志
		go func() {
			model.DB.Create(&log)
		}()
	}
}

// parseActionFromPath 从路径解析操作类型
func parseActionFromPath(method, path string) (action, resource, resourceID string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// 解析资源类型
	for _, part := range parts {
		switch part {
		case "apps":
			resource = model.ResourceApp
		case "licenses":
			resource = model.ResourceLicense
		case "subscriptions":
			resource = model.ResourceSubscription
		case "devices":
			resource = model.ResourceDevice
		case "customers":
			resource = model.ResourceCustomer
		case "team", "members":
			resource = model.ResourceTeamMember
		case "tenant":
			resource = model.ResourceTenant
		case "scripts":
			resource = model.ResourceScript
		case "secure-scripts":
			resource = model.ResourceScript
		case "releases":
			resource = model.ResourceRelease
		case "hotupdate":
			resource = model.ResourceHotUpdate
		case "tasks":
			resource = model.ResourcePublishTask
		case "credentials":
			resource = model.ResourceProviderCred
		case "pricing", "rules":
			resource = model.ResourcePricingRule
		case "credits":
			resource = model.ResourceCredit
		case "instructions":
			resource = model.ResourceInstruction
		case "auth":
			resource = model.ResourceUser
		}
	}

	// 解析操作类型
	switch method {
	case "POST":
		if strings.Contains(path, "/login") {
			action = model.ActionLogin
		} else if strings.Contains(path, "/publish") {
			action = model.ActionPublish
		} else if strings.Contains(path, "/deprecate") {
			action = model.ActionDeprecate
		} else if strings.Contains(path, "/rollback") {
			action = model.ActionRollback
		} else if strings.Contains(path, "/revoke") {
			action = model.ActionRevoke
		} else if strings.Contains(path, "/reset") {
			action = model.ActionReset
		} else {
			action = model.ActionCreate
		}
	case "PUT":
		action = model.ActionUpdate
	case "DELETE":
		action = model.ActionDelete
	default:
		action = method
	}

	// 尝试提取资源ID
	for i, part := range parts {
		if len(part) == 36 && strings.Count(part, "-") == 4 {
			resourceID = part
			break
		}
		// 检查是否是资源类型后面的ID
		if i > 0 && isResourceType(parts[i-1]) && len(part) > 0 {
			resourceID = part
		}
	}

	return
}

func isResourceType(s string) bool {
	types := []string{"apps", "licenses", "subscriptions", "devices", "customers", "members", "scripts", "secure-scripts", "releases", "hotupdate", "tasks", "credentials", "rules", "users", "instructions"}
	for _, t := range types {
		if s == t {
			return true
		}
	}
	return false
}

func generateDescription(action, resource string) string {
	actionMap := map[string]string{
		model.ActionCreate:    "创建",
		model.ActionUpdate:    "更新",
		model.ActionDelete:    "删除",
		model.ActionLogin:     "登录",
		model.ActionRevoke:    "吊销",
		model.ActionReset:     "重置",
		model.ActionPublish:   "发布",
		model.ActionDeprecate: "废弃",
		model.ActionRollback:  "回滚",
	}
	resourceMap := map[string]string{
		model.ResourceUser:         "用户",
		model.ResourceTeamMember:   "团队成员",
		model.ResourceCustomer:     "客户",
		model.ResourceTenant:       "租户",
		model.ResourceApp:          "应用",
		model.ResourceLicense:      "授权",
		model.ResourceSubscription: "订阅",
		model.ResourceDevice:       "设备",
		model.ResourceScript:       "脚本",
		model.ResourceRelease:      "版本",
		model.ResourceHotUpdate:    "热更新",
		model.ResourcePublishTask:  "发布任务",
		model.ResourceProviderCred: "Provider 凭证",
		model.ResourcePricingRule:  "计价规则",
		model.ResourceCredit:       "额度",
		model.ResourceInstruction:  "实时指令",
	}

	a := actionMap[action]
	if a == "" {
		a = action
	}
	r := resourceMap[resource]
	if r == "" {
		r = resource
	}

	return a + r
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func captureAuditRequestBody(c *gin.Context) string {
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return ""
	}

	contentType := c.GetHeader("Content-Type")
	if !isAuditableBodyContentType(contentType) {
		return omittedAuditBody("content_type=" + firstNonEmpty(contentType, "unknown"))
	}

	if c.Request.ContentLength < 0 {
		return omittedAuditBody("unknown_length")
	}
	if c.Request.ContentLength > maxAuditRequestBodyBytes {
		return omittedAuditBody("too_large")
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	if err != nil {
		return omittedAuditBody("read_error")
	}
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return ""
	}
	return maskSensitiveData(string(bodyBytes))
}

func isAuditableBodyContentType(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func omittedAuditBody(reason string) string {
	return "[request body omitted: " + reason + "]"
}

func maskSensitiveData(data string) string {
	var payload any
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return omittedAuditBody("invalid_json")
	}

	masked, err := json.Marshal(maskSensitiveValue(payload))
	if err != nil {
		return omittedAuditBody("mask_error")
	}
	return string(masked)
}

func maskSensitiveValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveAuditKey(key) {
				out[key] = "***"
				continue
			}
			out[key] = maskSensitiveValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = maskSensitiveValue(item)
		}
		return out
	default:
		return value
	}
}

func isSensitiveAuditKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_")) {
	case "password",
		"oldpassword",
		"old_password",
		"newpassword",
		"new_password",
		"api_key",
		"api_token",
		"apikey",
		"authorization",
		"access_token",
		"accesstoken",
		"refresh_token",
		"refreshtoken",
		"token",
		"app_secret",
		"appsecret",
		"license_key",
		"licensekey",
		"private_key",
		"privatekey",
		"secret",
		"x_api_key",
		"custom_header",
		"custom_headers":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
