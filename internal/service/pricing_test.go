package service

import (
	"testing"

	"license-server/internal/model"
)

func TestEvalFormula(t *testing.T) {
	tests := []struct {
		formula string
		params  map[string]any
		want    int
		wantErr bool
	}{
		{"1+2", nil, 3, false},
		{"2*3+4", nil, 10, false},
		{"2+3*4", nil, 14, false},
		{"(2+3)*4", nil, 20, false},
		{"duration_seconds * 2", map[string]any{"duration_seconds": 5}, 10, false},
		{"duration_seconds * 2 + 1", map[string]any{"duration_seconds": 5}, 11, false},
		{"duration_seconds", map[string]any{"duration_seconds": "8"}, 8, false},        // string -> int
		{"duration_seconds", map[string]any{"duration_seconds": float64(8)}, 8, false}, // JSON 解析后是 float64
		{"-5+10", nil, 5, false},
		{"10/3", nil, 3, false}, // 整数除法
		{"10/0", nil, 0, true},  // 除零
		{"unknown_var", nil, 0, true},
		{"1 + ", nil, 0, true},                           // 残缺
		{"a b", map[string]any{"a": 1, "b": 2}, 0, true}, // 多余 ident
	}
	for _, tt := range tests {
		got, err := evalFormula(tt.formula, tt.params)
		if tt.wantErr {
			if err == nil {
				t.Errorf("formula=%q params=%v: 期望错误，得到 %d", tt.formula, tt.params, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("formula=%q params=%v: 期望 %d，错误 %v", tt.formula, tt.params, tt.want, err)
			continue
		}
		if got != tt.want {
			t.Errorf("formula=%q params=%v: 期望 %d，得到 %d", tt.formula, tt.params, tt.want, got)
		}
	}
}

func TestMatchAll(t *testing.T) {
	tests := []struct {
		matchJSON string
		params    map[string]any
		want      bool
	}{
		{"", map[string]any{"x": 1}, true},   // 空匹配 = 任意
		{"{}", map[string]any{"x": 1}, true}, // 空对象 = 任意
		{"null", map[string]any{"x": 1}, true},
		{`{"model":"gpt-4o-mini"}`, map[string]any{"model": "gpt-4o-mini"}, true},
		{`{"model":"gpt-4o-mini"}`, map[string]any{"model": "gpt-3.5"}, false},
		{`{"duration_seconds":5}`, map[string]any{"duration_seconds": 5}, true},   // 5 == 5
		{`{"duration_seconds":5}`, map[string]any{"duration_seconds": "5"}, true}, // "5" == 5（loose）
		{`{"duration_seconds":5}`, map[string]any{"duration_seconds": float64(5)}, true},
		{`{"duration_seconds":5}`, map[string]any{}, false}, // 缺少字段
		{`{"a":1,"b":2}`, map[string]any{"a": 1, "b": 2}, true},
		{`{"a":1,"b":2}`, map[string]any{"a": 1}, false},         // 缺一个
		{`{"a":1,"b":2}`, map[string]any{"a": 1, "b": 3}, false}, // 错一个
		{"not-json", map[string]any{}, false},                    // 规则配错 → 不命中
	}
	for _, tt := range tests {
		got := matchAll(tt.matchJSON, tt.params)
		if got != tt.want {
			t.Errorf("matchAll(%q, %v) = %v, want %v", tt.matchJSON, tt.params, got, tt.want)
		}
	}
}

func TestLooseEqual(t *testing.T) {
	if !looseEqual(5, "5") {
		t.Error("5 应该 loose equal '5'")
	}
	if !looseEqual(float64(8), 8) {
		t.Error("float64(8) 应该 loose equal int(8)")
	}
	if looseEqual("a", "b") {
		t.Error("a 不应 loose equal b")
	}
}

func TestExtractParams_NormalizesDurationAndReferenceImageCount(t *testing.T) {
	params := extractParams([]byte(`{"model":"grok-imagine-video","duration":8,"reference_image_count":3,"ignored":"x"}`))

	if params["duration"] != float64(8) {
		t.Fatalf("duration = %#v", params["duration"])
	}
	if params["duration_seconds"] != float64(8) {
		t.Fatalf("duration_seconds = %#v", params["duration_seconds"])
	}
	if params["reference_image_count"] != float64(3) {
		t.Fatalf("reference_image_count = %#v", params["reference_image_count"])
	}
	if _, ok := params["ignored"]; ok {
		t.Fatal("unexpected ignored param")
	}
}

func TestExtractParams_NormalizesVeoGoogleParameters(t *testing.T) {
	params := extractParams([]byte(`{"model":"veo-3","parameters":{"durationSeconds":8,"aspectRatio":"16:9","resolution":"720p"}}`))

	if params["duration_seconds"] != float64(8) {
		t.Fatalf("duration_seconds = %#v", params["duration_seconds"])
	}
	if params["duration"] != float64(8) {
		t.Fatalf("duration = %#v", params["duration"])
	}
	if params["aspect_ratio"] != "16:9" {
		t.Fatalf("aspect_ratio = %#v", params["aspect_ratio"])
	}
	if params["resolution"] != "720p" {
		t.Fatalf("resolution = %#v", params["resolution"])
	}
}

func TestExtractParams_UsesClientModelForPricing(t *testing.T) {
	params := extractParams([]byte(`{"model":"grok-video-3","client_model":"grok-imagine","duration_seconds":8}`))

	if params["model"] != "grok-imagine" {
		t.Fatalf("model = %#v, want client model", params["model"])
	}
	if params["client_model"] != "grok-imagine" {
		t.Fatalf("client_model = %#v", params["client_model"])
	}
}

func TestNormalizePricingParamsAddsDurationAliases(t *testing.T) {
	params := NormalizePricingParams(map[string]any{"duration_seconds": float64(10)})
	if params["duration"] != float64(10) {
		t.Fatalf("duration = %#v", params["duration"])
	}

	params = NormalizePricingParams(map[string]any{"duration": float64(8)})
	if params["duration_seconds"] != float64(8) {
		t.Fatalf("duration_seconds = %#v", params["duration_seconds"])
	}
}

func TestDefaultGrokVideoMatchCoversAllGrokVideoModels(t *testing.T) {
	params := NormalizePricingParams(map[string]any{
		"model":            "grok-video",
		"mode":             "suchuang",
		"duration_seconds": float64(30),
	})

	if !matchAll(defaultGrokVideoMatchJSON, params) {
		t.Fatalf("default Grok match should cover third-party params: %#v", params)
	}
}

func TestIsLegacyDefaultGrokVideoRule(t *testing.T) {
	rule := &model.PricingRule{
		Provider:  string(model.ProviderGrok),
		Scope:     model.PricingScopeVideo,
		MatchJSON: legacyDefaultGrokVideoMatchJSON,
		Credits:   defaultGrokVideoCredits,
		Enabled:   true,
		Note:      "默认 Grok 视频计价规则：所有 grok-imagine-video 任务扣 10 点",
	}

	if !isLegacyDefaultGrokVideoRule(rule) {
		t.Fatal("system-created legacy default rule should be detected")
	}

	rule.Formula = "duration * 2"
	if isLegacyDefaultGrokVideoRule(rule) {
		t.Fatal("customized formula should not be treated as legacy default")
	}
}
