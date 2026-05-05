package handler

import (
	"strings"
	"testing"
)

func TestValidatePricingRuleRequestRejectsZeroCostWithoutFormula(t *testing.T) {
	err := validatePricingRuleRequest(pricingRuleRequest{
		Provider:  "grok",
		Scope:     "video",
		MatchJSON: `{"model":"grok-imagine-video"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "至少一个非空") {
		t.Fatalf("expected cost validation error, got %v", err)
	}
}

func TestValidatePricingRuleRequestRejectsInvalidMatchJSON(t *testing.T) {
	err := validatePricingRuleRequest(pricingRuleRequest{
		Provider:  "grok",
		Scope:     "video",
		Credits:   10,
		MatchJSON: `{"model":`,
	})
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestValidatePricingRuleRequestRejectsUnsupportedProvider(t *testing.T) {
	err := validatePricingRuleRequest(pricingRuleRequest{
		Provider:  "not-a-provider",
		Scope:     "video",
		Credits:   10,
		MatchJSON: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("expected provider validation error, got %v", err)
	}
}

func TestValidatePricingRuleRequestRejectsClaudeUntilAdapterIsConnected(t *testing.T) {
	err := validatePricingRuleRequest(pricingRuleRequest{
		Provider:  "claude",
		Scope:     "chat",
		Credits:   1,
		MatchJSON: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("expected Claude provider validation error, got %v", err)
	}
}

func TestValidatePricingRuleRequestRejectsUnsupportedScope(t *testing.T) {
	err := validatePricingRuleRequest(pricingRuleRequest{
		Provider:  "grok",
		Scope:     "movie",
		Credits:   10,
		MatchJSON: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("expected scope validation error, got %v", err)
	}
}

func TestValidatePricingRuleRequestRejectsInvalidFormula(t *testing.T) {
	err := validatePricingRuleRequest(pricingRuleRequest{
		Provider:  "grok",
		Scope:     "video",
		Formula:   "duration_seconds * unknown_count",
		MatchJSON: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "formula") {
		t.Fatalf("expected formula validation error, got %v", err)
	}
}

func TestNormalizePricingRuleRequestTrimsCanonicalFields(t *testing.T) {
	got := normalizePricingRuleRequest(pricingRuleRequest{
		Provider: " Grok ",
		Scope:    " Video ",
		Formula:  " duration_seconds * 2 ",
		Note:     " note ",
	})
	if got.Provider != "grok" || got.Scope != "video" || got.Formula != "duration_seconds * 2" || got.Note != "note" {
		t.Fatalf("unexpected normalized request: %#v", got)
	}
	if got.MatchJSON != "{}" {
		t.Fatalf("match_json = %q, want {}", got.MatchJSON)
	}
}

func TestValidateCustomHeadersRejectsNestedValue(t *testing.T) {
	err := validateCustomHeaders(`{"X-Test":{"nested":true}}`)
	if err == nil || !strings.Contains(err.Error(), "必须是字符串") {
		t.Fatalf("expected custom header validation error, got %v", err)
	}
}

func TestValidateCustomHeadersRejectsReservedHeader(t *testing.T) {
	err := validateCustomHeaders(`{"Authorization":"Bearer bad"}`)
	if err == nil || !strings.Contains(err.Error(), "系统管理") {
		t.Fatalf("expected reserved header validation error, got %v", err)
	}
}

func TestCredentialUpdateInputAllowsClearingTextFields(t *testing.T) {
	in := credentialUpdateInputFromRequest(credentialRequest{})
	if in.DefaultModel == nil || *in.DefaultModel != "" {
		t.Fatalf("DefaultModel should be an empty string pointer, got %#v", in.DefaultModel)
	}
	if in.CustomHeader == nil || *in.CustomHeader != "" {
		t.Fatalf("CustomHeader should be an empty string pointer, got %#v", in.CustomHeader)
	}
	if in.Note == nil || *in.Note != "" {
		t.Fatalf("Note should be an empty string pointer, got %#v", in.Note)
	}
}
