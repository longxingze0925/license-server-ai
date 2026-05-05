package handler

import (
	"encoding/json"
	"testing"

	"license-server/internal/model"
)

func TestNormalizeInstructionResultStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   model.InstructionStatus
		wantOK bool
	}{
		{name: "legacy success", input: "success", want: model.InstructionStatusExecuted, wantOK: true},
		{name: "acked", input: "acked", want: model.InstructionStatusAcked, wantOK: true},
		{name: "executed", input: " executed ", want: model.InstructionStatusExecuted, wantOK: true},
		{name: "failed", input: "failed", want: model.InstructionStatusFailed, wantOK: true},
		{name: "pending is not client result", input: "pending", wantOK: false},
		{name: "unknown", input: "done", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeInstructionResultStatus(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeInstructionResultText(t *testing.T) {
	tests := []struct {
		name      string
		result    string
		errorText string
		want      string
	}{
		{name: "string result", result: `"ok"`, want: "ok"},
		{name: "object result", result: `{"x":1}`, want: `{"x":1}`},
		{name: "null falls back to error", result: `null`, errorText: "failed", want: "failed"},
		{name: "empty falls back to error", errorText: "failed", want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeInstructionResultText(json.RawMessage(tt.result), tt.errorText)
			if got != tt.want {
				t.Fatalf("result = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeClientScriptDeliveryStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   model.ScriptDeliveryStatus
		wantOK bool
	}{
		{name: "executing", input: "executing", want: model.ScriptDeliveryStatusExecuting, wantOK: true},
		{name: "success", input: " success ", want: model.ScriptDeliveryStatusSuccess, wantOK: true},
		{name: "failed", input: "failed", want: model.ScriptDeliveryStatusFailed, wantOK: true},
		{name: "pending is not reportable", input: "pending", wantOK: false},
		{name: "expired is not reportable", input: "expired", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeClientScriptDeliveryStatus(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsValidInstructionType(t *testing.T) {
	if !isValidInstructionType(model.InstructionTypeCustom) {
		t.Fatal("custom should be accepted")
	}
	if !isValidInstructionType(model.InstructionTypeExecScript) {
		t.Fatal("exec_script should be accepted")
	}
	if isValidInstructionType(model.InstructionType("reload_config")) {
		t.Fatal("reload_config should be rejected")
	}
}

func TestBroadcastToAppReturnsDeliveredCount(t *testing.T) {
	h := NewWebSocketHub()
	h.clients["app-1"] = map[string]*DeviceClient{
		"machine-1": {send: make(chan []byte, 1)},
		"machine-2": {send: make(chan []byte, 1)},
	}
	h.clients["app-1"]["machine-2"].send <- []byte("buffer-full")

	got := h.BroadcastToApp("app-1", []byte(`{"type":"instruction"}`))
	if got != 1 {
		t.Fatalf("BroadcastToApp delivered %d messages, want 1", got)
	}

	delivered := <-h.clients["app-1"]["machine-1"].send
	if string(delivered) != `{"type":"instruction"}` {
		t.Fatalf("delivered message = %s", delivered)
	}
	if got := h.BroadcastToApp("missing-app", []byte(`{}`)); got != 0 {
		t.Fatalf("BroadcastToApp missing app delivered %d messages, want 0", got)
	}
}
