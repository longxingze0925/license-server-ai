package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestClientRegisterRejectsPreHashedPasswordBeforeDatabaseLookup(t *testing.T) {
	w := performClientPasswordContractRequest(
		t,
		http.MethodPost,
		"/api/client/auth/register",
		`{"app_key":"app-test","email":"user@example.com","password":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","password_hashed":true}`,
		func(h *ClientHandler, c *gin.Context) {
			h.ClientRegister(c)
		},
	)

	assertPasswordContractError(t, w, http.StatusBadRequest, "注册必须提交原始密码")
}

func TestClientRegisterRejectsWeakRawPasswordBeforeDatabaseLookup(t *testing.T) {
	w := performClientPasswordContractRequest(
		t,
		http.MethodPost,
		"/api/client/auth/register",
		`{"app_key":"app-test","email":"user@example.com","password":"123","password_hashed":false}`,
		func(h *ClientHandler, c *gin.Context) {
			h.ClientRegister(c)
		},
	)

	assertPasswordContractError(t, w, http.StatusBadRequest, "密码至少")
}

func TestClientChangePasswordRejectsPreHashedPasswordBeforeSessionLookup(t *testing.T) {
	w := performClientPasswordContractRequest(
		t,
		http.MethodPut,
		"/api/client/auth/password",
		`{"old_password":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","new_password":"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","password_hashed":true}`,
		func(h *ClientHandler, c *gin.Context) {
			h.ClientChangePassword(c)
		},
	)

	assertPasswordContractError(t, w, http.StatusBadRequest, "修改密码必须提交原始密码")
}

func TestClientChangePasswordRejectsWeakRawPasswordBeforeSessionLookup(t *testing.T) {
	w := performClientPasswordContractRequest(
		t,
		http.MethodPut,
		"/api/client/auth/password",
		`{"old_password":"Valid123!","new_password":"123","password_hashed":false}`,
		func(h *ClientHandler, c *gin.Context) {
			h.ClientChangePassword(c)
		},
	)

	assertPasswordContractError(t, w, http.StatusBadRequest, "密码至少")
}

func performClientPasswordContractRequest(
	t *testing.T,
	method string,
	path string,
	body string,
	handler func(*ClientHandler, *gin.Context),
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	handler(NewClientHandler(), c)
	return w
}

func assertPasswordContractError(t *testing.T, w *httptest.ResponseRecorder, status int, messagePart string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, status, w.Body.String())
	}

	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response should be JSON, got %q: %v", w.Body.String(), err)
	}
	if !strings.Contains(payload.Message, messagePart) {
		t.Fatalf("message = %q, want containing %q", payload.Message, messagePart)
	}
}
