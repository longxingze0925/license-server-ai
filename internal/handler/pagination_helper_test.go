package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParsePageParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		rawQuery     string
		defaultSize  int
		maxSize      int
		wantPage     int
		wantPageSize int
	}{
		{name: "defaults", wantPage: 1, wantPageSize: 20},
		{name: "valid", rawQuery: "page=3&page_size=50", wantPage: 3, wantPageSize: 50},
		{name: "invalid page", rawQuery: "page=-2&page_size=10", wantPage: 1, wantPageSize: 10},
		{name: "invalid page size", rawQuery: "page=2&page_size=-10", wantPage: 2, wantPageSize: 20},
		{name: "oversized page size", rawQuery: "page=2&page_size=1000", wantPage: 2, wantPageSize: 20},
		{name: "custom defaults", rawQuery: "page=x&page_size=y", defaultSize: 30, maxSize: 120, wantPage: 1, wantPageSize: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultSize := tt.defaultSize
			if defaultSize == 0 {
				defaultSize = 20
			}
			maxSize := tt.maxSize
			if maxSize == 0 {
				maxSize = 100
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest("GET", "/?"+tt.rawQuery, nil)
			c.Request = req

			gotPage, gotPageSize := parsePageParams(c, defaultSize, maxSize)
			if gotPage != tt.wantPage || gotPageSize != tt.wantPageSize {
				t.Fatalf("parsePageParams() = (%d, %d), want (%d, %d)", gotPage, gotPageSize, tt.wantPage, tt.wantPageSize)
			}
		})
	}
}
