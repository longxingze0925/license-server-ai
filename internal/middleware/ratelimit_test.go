package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimitMiddlewareAddsRetryAfterHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewRateLimiter(1, time.Minute)
	router := gin.New()
	router.Use(RateLimitMiddleware(limiter))
	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if got := recorder.Header().Get("Retry-After"); got != "10" {
		t.Fatalf("Retry-After = %q, want 10", got)
	}
}

func TestRateLimitByMethodMiddlewareUsesReadLimiterForGets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	readLimiter := NewRateLimiter(2, time.Minute)
	writeLimiter := NewRateLimiter(1, time.Minute)
	router := gin.New()
	router.Use(RateLimitByMethodMiddleware(readLimiter, writeLimiter))
	router.GET("/resource", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.POST("/resource", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/resource", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET #%d status = %d, want 200", i+1, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/resource", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST #1 status = %d, want 200", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/resource", nil))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("POST #2 status = %d, want 429", recorder.Code)
	}
}

func TestRateLimitExceptPathsMiddlewareKeepsClientGroupOnDedicatedLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	globalLimiter := NewRateLimiter(1, time.Minute)
	clientReadLimiter := NewRateLimiter(2, time.Minute)
	clientWriteLimiter := NewRateLimiter(1, time.Minute)
	router := gin.New()
	router.Use(RateLimitExceptPathsMiddleware(globalLimiter, "/api/client"))

	client := router.Group("/api/client")
	client.Use(RateLimitByMethodMiddleware(clientReadLimiter, clientWriteLimiter))
	client.GET("/proxy/tasks/task-1", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	router.GET("/api/admin/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second admin request status = %d, want 429", recorder.Code)
	}

	for i := 0; i < 2; i++ {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/client/proxy/tasks/task-1", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("client GET #%d status = %d, want 200", i+1, recorder.Code)
		}
	}
}

func TestHasExcludedPathPrefixMatchesOnlyRouteBoundary(t *testing.T) {
	if !hasExcludedPathPrefix("/api/client/proxy/tasks", "/api/client") {
		t.Fatal("expected /api/client child route to match")
	}
	if hasExcludedPathPrefix("/api/clientevil", "/api/client") {
		t.Fatal("prefix should not match without route boundary")
	}
}
