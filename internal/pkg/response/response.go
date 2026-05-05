package response

import (
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
)

// Response 统一响应结构
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// PageData 分页数据
type PageData struct {
	List     interface{} `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

// Success 成功响应
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    normalizeJSONValue(data),
	})
}

// SuccessWithMessage 成功响应带消息
func SuccessWithMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: message,
		Data:    normalizeJSONValue(data),
	})
}

// SuccessPage 分页成功响应
func SuccessPage(c *gin.Context, list interface{}, total int64, page, pageSize int) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data: PageData{
			List:     normalizeJSONValue(list),
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		},
	})
}

func normalizeJSONValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	normalized := normalizeJSONReflect(reflect.ValueOf(value))
	if !normalized.IsValid() {
		return nil
	}
	return normalized.Interface()
}

func normalizeJSONReflect(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}

	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return value
		}
		return normalizeJSONReflect(value.Elem())
	}

	switch value.Kind() {
	case reflect.Slice:
		if value.IsNil() {
			return reflect.MakeSlice(value.Type(), 0, 0)
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(normalizeAssignableValue(normalizeJSONReflect(value.Index(i)), value.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(normalizeAssignableValue(normalizeJSONReflect(value.Index(i)), value.Index(i)))
		}
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.MakeMap(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		for _, key := range value.MapKeys() {
			item := value.MapIndex(key)
			out.SetMapIndex(key, normalizeAssignableValue(normalizeJSONReflect(item), item))
		}
		return out
	default:
		return value
	}
}

func normalizeAssignableValue(value reflect.Value, fallback reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Zero(fallback.Type())
	}
	if value.Type().AssignableTo(fallback.Type()) {
		return value
	}
	if value.Type().ConvertibleTo(fallback.Type()) {
		return value.Convert(fallback.Type())
	}
	return fallback
}

// Error 错误响应
func Error(c *gin.Context, code int, message string) {
	c.JSON(httpStatusFromCode(code), Response{
		Code:    code,
		Message: message,
	})
}

func httpStatusFromCode(code int) int {
	if code >= 100 && code <= 599 {
		return code
	}
	return http.StatusInternalServerError
}

// BadRequest 参数错误
func BadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, Response{
		Code:    400,
		Message: message,
	})
}

// Unauthorized 未授权
func Unauthorized(c *gin.Context, message string) {
	c.JSON(http.StatusUnauthorized, Response{
		Code:    401,
		Message: message,
	})
}

// Forbidden 禁止访问
func Forbidden(c *gin.Context, message string) {
	c.JSON(http.StatusForbidden, Response{
		Code:    403,
		Message: message,
	})
}

// NotFound 资源不存在
func NotFound(c *gin.Context, message string) {
	c.JSON(http.StatusNotFound, Response{
		Code:    404,
		Message: message,
	})
}

// ServerError 服务器错误
func ServerError(c *gin.Context, message string) {
	c.JSON(http.StatusInternalServerError, Response{
		Code:    500,
		Message: message,
	})
}
