package handler

import (
	"testing"

	"license-server/internal/model"
)

func TestNormalizeHotUpdateLogStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   model.HotUpdateLogStatus
		wantOK bool
	}{
		{name: "pending", input: "pending", want: model.HotUpdateLogStatusPending, wantOK: true},
		{name: "downloading trims and lowercases", input: " Downloading ", want: model.HotUpdateLogStatusDownloading, wantOK: true},
		{name: "installing", input: "installing", want: model.HotUpdateLogStatusInstalling, wantOK: true},
		{name: "success", input: "success", want: model.HotUpdateLogStatusSuccess, wantOK: true},
		{name: "failed", input: "failed", want: model.HotUpdateLogStatusFailed, wantOK: true},
		{name: "rollback", input: "rollback", want: model.HotUpdateLogStatusRollback, wantOK: true},
		{name: "unknown", input: "done", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeHotUpdateLogStatus(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsTerminalHotUpdateLogStatus(t *testing.T) {
	tests := []struct {
		status model.HotUpdateLogStatus
		want   bool
	}{
		{model.HotUpdateLogStatusPending, false},
		{model.HotUpdateLogStatusDownloading, false},
		{model.HotUpdateLogStatusInstalling, false},
		{model.HotUpdateLogStatusSuccess, true},
		{model.HotUpdateLogStatusFailed, true},
		{model.HotUpdateLogStatusRollback, true},
	}

	for _, tt := range tests {
		if got := isTerminalHotUpdateLogStatus(tt.status); got != tt.want {
			t.Fatalf("isTerminalHotUpdateLogStatus(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestShouldApplyHotUpdateLogStatusTransition(t *testing.T) {
	tests := []struct {
		name string
		from model.HotUpdateLogStatus
		to   model.HotUpdateLogStatus
		want bool
	}{
		{name: "new log", to: model.HotUpdateLogStatusDownloading, want: true},
		{name: "running to failed", from: model.HotUpdateLogStatusInstalling, to: model.HotUpdateLogStatusFailed, want: true},
		{name: "failed can retry downloading", from: model.HotUpdateLogStatusFailed, to: model.HotUpdateLogStatusDownloading, want: true},
		{name: "failed can become success", from: model.HotUpdateLogStatusFailed, to: model.HotUpdateLogStatusSuccess, want: true},
		{name: "success ignores late failed", from: model.HotUpdateLogStatusSuccess, to: model.HotUpdateLogStatusFailed, want: false},
		{name: "rollback ignores late success", from: model.HotUpdateLogStatusRollback, to: model.HotUpdateLogStatusSuccess, want: false},
		{name: "same terminal is idempotent", from: model.HotUpdateLogStatusSuccess, to: model.HotUpdateLogStatusSuccess, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldApplyHotUpdateLogStatusTransition(tt.from, tt.to); got != tt.want {
				t.Fatalf("shouldApplyHotUpdateLogStatusTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestHotUpdateStatusCounterDelta(t *testing.T) {
	tests := []struct {
		name        string
		from        model.HotUpdateLogStatus
		to          model.HotUpdateLogStatus
		wantSuccess int
		wantFail    int
	}{
		{name: "running to success", from: model.HotUpdateLogStatusInstalling, to: model.HotUpdateLogStatusSuccess, wantSuccess: 1},
		{name: "running to failed", from: model.HotUpdateLogStatusInstalling, to: model.HotUpdateLogStatusFailed, wantFail: 1},
		{name: "failed retry success", from: model.HotUpdateLogStatusFailed, to: model.HotUpdateLogStatusSuccess, wantSuccess: 1, wantFail: -1},
		{name: "failed retry running", from: model.HotUpdateLogStatusFailed, to: model.HotUpdateLogStatusDownloading, wantFail: -1},
		{name: "same failed no delta", from: model.HotUpdateLogStatusFailed, to: model.HotUpdateLogStatusFailed},
		{name: "success to success no delta", from: model.HotUpdateLogStatusSuccess, to: model.HotUpdateLogStatusSuccess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSuccess, gotFail := hotUpdateStatusCounterDelta(tt.from, tt.to)
			if gotSuccess != tt.wantSuccess || gotFail != tt.wantFail {
				t.Fatalf("hotUpdateStatusCounterDelta(%q, %q) = (%d, %d), want (%d, %d)",
					tt.from, tt.to, gotSuccess, gotFail, tt.wantSuccess, tt.wantFail)
			}
		})
	}
}

func TestCompareVersionStringsUsesNumericSegments(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.10.0", right: "1.2.0", want: 1},
		{left: "1.2", right: "1.2.0", want: 0},
		{left: "2.0", right: "10.0", want: -1},
		{left: "1.0.1", right: "1.0.1", want: 0},
	}

	for _, tt := range tests {
		got := compareVersionStrings(tt.left, tt.right)
		if got < 0 {
			got = -1
		} else if got > 0 {
			got = 1
		}
		if got != tt.want {
			t.Fatalf("compareVersionStrings(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}

func TestScriptVersionCode(t *testing.T) {
	tests := []struct {
		version string
		want    int
	}{
		{version: "1.2.30", want: 1002030},
		{version: "20260502", want: 20260502},
		{version: "v1.2.3-beta.4", want: 1002003004},
		{version: "abc", want: 0},
		{version: "", want: 0},
	}

	for _, tt := range tests {
		if got := scriptVersionCode(tt.version); got != tt.want {
			t.Fatalf("scriptVersionCode(%q) = %d, want %d", tt.version, got, tt.want)
		}
	}
}

func TestNormalizePackageVersion(t *testing.T) {
	valid := []string{"1.2.3", "v1.2.3-beta+4", "20260504", "build_1"}
	for _, version := range valid {
		if got, err := normalizePackageVersion(" " + version + " "); err != nil || got != version {
			t.Fatalf("normalizePackageVersion(%q) = %q, %v; want %q, nil", version, got, err, version)
		}
	}

	invalid := []string{"", "../1.0.0", "1/2/3", "1.2.3.4.5.6.7.8.9.10.11", "-1.0"}
	for _, version := range invalid {
		if _, err := normalizePackageVersion(version); err == nil {
			t.Fatalf("normalizePackageVersion(%q) should fail", version)
		}
	}
}

func TestNormalizeHotUpdateUploadType(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "", want: "full", ok: true},
		{input: " PATCH ", want: "patch", ok: true},
		{input: "full", want: "full", ok: true},
		{input: "../patch", ok: false},
	}

	for _, tt := range tests {
		got, ok := normalizeHotUpdateUploadType(tt.input)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("normalizeHotUpdateUploadType(%q) = (%q, %v), want (%q, %v)",
				tt.input, got, ok, tt.want, tt.ok)
		}
	}
}
