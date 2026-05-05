package handler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var packageVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,19}$`)

func normalizePackageVersion(version string) (string, error) {
	normalized := strings.TrimSpace(version)
	if normalized == "" {
		return "", fmt.Errorf("版本号不能为空")
	}
	if !packageVersionPattern.MatchString(normalized) {
		return "", fmt.Errorf("版本号只能包含字母、数字、点、下划线、中划线和加号，且最长 20 个字符")
	}
	return normalized, nil
}

func packageVersionFilenamePart(version string) string {
	normalized := strings.TrimSpace(version)
	if normalized == "" || normalized == "*" {
		return "any"
	}
	if packageVersionPattern.MatchString(normalized) {
		return normalized
	}
	return "invalid"
}

func scriptVersionCode(version string) int {
	normalized := strings.TrimSpace(version)
	if normalized == "" {
		return 0
	}
	if n, err := strconv.Atoi(normalized); err == nil {
		return n
	}

	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r < '0' || r > '9'
	})
	code := 0
	for i := 0; i < len(parts) && i < 4; i++ {
		if parts[i] == "" {
			continue
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}
		if n > 999 {
			n = 999
		}
		code = code*1000 + n
	}
	return code
}
