package handler

import (
	"errors"
	"license-server/internal/config"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	downloadTokenKindRelease   = "release"
	downloadTokenKindHotUpdate = "hotupdate"
)

type downloadTokenClaims struct {
	TenantID  string `json:"tenant_id,omitempty"`
	AppID     string `json:"app_id"`
	MachineID string `json:"machine_id"`
	Filename  string `json:"filename"`
	Kind      string `json:"kind"`
	jwt.RegisteredClaims
}

func buildClientDownloadURLWithToken(rawURL, tenantID, appID, machineID, kind string) (string, error) {
	filename := getFilenameFromDownloadURL(rawURL)
	if filename == "" {
		return "", errors.New("invalid download url")
	}

	token, err := generateDownloadToken(tenantID, appID, machineID, filename, kind)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("token", token)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func getFilenameFromDownloadURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Path != "" {
		return path.Base(parsed.Path)
	}

	return path.Base(rawURL)
}

func generateDownloadToken(tenantID, appID, machineID, filename, kind string) (string, error) {
	if tenantID == "" || appID == "" || machineID == "" || filename == "" || kind == "" {
		return "", errors.New("missing token claims")
	}

	secret := getDownloadTokenSecret()
	if secret == "" {
		return "", errors.New("missing download token secret")
	}

	now := time.Now()
	claims := downloadTokenClaims{
		TenantID:  tenantID,
		AppID:     appID,
		MachineID: machineID,
		Filename:  filename,
		Kind:      kind,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(getDownloadTokenTTL())),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func parseDownloadToken(tokenString string) (*downloadTokenClaims, error) {
	secret := getDownloadTokenSecret()
	if secret == "" {
		return nil, errors.New("missing download token secret")
	}

	token, err := jwt.ParseWithClaims(tokenString, &downloadTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("invalid signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*downloadTokenClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.AppID == "" || claims.MachineID == "" || claims.Filename == "" || claims.Kind == "" {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

func getDownloadTokenTTL() time.Duration {
	cfg := config.Get()
	if cfg == nil || cfg.Security.DownloadTokenExpireSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(cfg.Security.DownloadTokenExpireSeconds) * time.Second
}

func getDownloadTokenSecret() string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}

	if secret := strings.TrimSpace(cfg.Security.DownloadTokenSecret); secret != "" {
		return secret
	}

	return cfg.JWT.Secret
}
