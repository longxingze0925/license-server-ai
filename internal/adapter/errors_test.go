package adapter

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestCreateErrorNeedsCredentialRetry(t *testing.T) {
	if CreateErrorNeedsCredentialRetry(errors.New("Grok 参考图模式最长支持 10 秒")) {
		t.Fatal("local validation errors should not retry another credential")
	}
	if !CreateErrorNeedsCredentialRetry(newUpstreamHTTPError("create", 401, []byte("bad key"), 200)) {
		t.Fatal("401 should retry another credential")
	}
	if !CreateErrorNeedsCredentialRetry(newUpstreamHTTPError("create", 503, []byte("busy"), 200)) {
		t.Fatal("503 should retry another credential")
	}
	if CreateErrorNeedsCredentialRetry(newUpstreamHTTPError("create", 400, []byte("bad request"), 200)) {
		t.Fatal("400 should not retry another credential")
	}
}

func TestUpstreamHTTPErrorMessage(t *testing.T) {
	err := newUpstreamHTTPError("create", 400, []byte(strings.Repeat("x", 220)), 20)
	if !strings.Contains(err.Error(), "create 上游 HTTP 400") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyCustomHeadersSkipsReservedHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer system")
	req.Header.Set("Content-Type", "application/json")

	applyCustomHeaders(req, `{"Authorization":"Bearer custom","Content-Type":"text/plain","X-Test":"ok"}`)

	if req.Header.Get("Authorization") != "Bearer system" {
		t.Fatalf("Authorization was overwritten: %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type was overwritten: %q", req.Header.Get("Content-Type"))
	}
	if req.Header.Get("X-Test") != "ok" {
		t.Fatalf("X-Test = %q", req.Header.Get("X-Test"))
	}
}
