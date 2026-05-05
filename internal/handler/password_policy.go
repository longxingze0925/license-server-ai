package handler

import (
	"fmt"
	"license-server/internal/config"
	"unicode"
)

func validatePasswordPolicy(password string) error {
	cfg := config.Get()
	minLength := 6
	requireNumber := false
	requireSymbol := false
	if cfg != nil {
		if cfg.Security.PasswordMinLength > 0 {
			minLength = cfg.Security.PasswordMinLength
		}
		requireNumber = cfg.Security.PasswordRequireNum
		requireSymbol = cfg.Security.PasswordRequireSym
	}

	if len([]rune(password)) < minLength {
		return fmt.Errorf("密码至少%d位", minLength)
	}
	if requireNumber && !containsNumber(password) {
		return fmt.Errorf("密码必须包含数字")
	}
	if requireSymbol && !containsSymbol(password) {
		return fmt.Errorf("密码必须包含特殊字符")
	}
	return nil
}

func containsNumber(value string) bool {
	for _, r := range value {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsSymbol(value string) bool {
	for _, r := range value {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return true
		}
	}
	return false
}
