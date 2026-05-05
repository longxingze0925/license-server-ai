package service

import (
	"testing"

	"license-server/internal/model"
)

func TestIsTerminalScriptDeliveryStatus(t *testing.T) {
	tests := []struct {
		status model.ScriptDeliveryStatus
		want   bool
	}{
		{model.ScriptDeliveryStatusPending, false},
		{model.ScriptDeliveryStatusExecuting, false},
		{model.ScriptDeliveryStatusSuccess, true},
		{model.ScriptDeliveryStatusFailed, true},
		{model.ScriptDeliveryStatusExpired, true},
	}

	for _, tt := range tests {
		if got := isTerminalScriptDeliveryStatus(tt.status); got != tt.want {
			t.Fatalf("isTerminalScriptDeliveryStatus(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
