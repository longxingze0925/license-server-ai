package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	JWT      JWTConfig      `yaml:"jwt"`
	RSA      RSAConfig      `yaml:"rsa"`
	Storage  StorageConfig  `yaml:"storage"`
	Log      LogConfig      `yaml:"log"`
	Email    EmailConfig    `yaml:"email"`
	Security SecurityConfig `yaml:"security"`
}

type ServerConfig struct {
	Host string    `yaml:"host"`
	Port int       `yaml:"port"`
	Mode string    `yaml:"mode"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type DatabaseConfig struct {
	Driver       string `yaml:"driver"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	Database     string `yaml:"database"`
	Charset      string `yaml:"charset"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
	MaxOpenConns int    `yaml:"max_open_conns"`
}

func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		d.Username, d.Password, d.Host, d.Port, d.Database, d.Charset)
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (r *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type RSAConfig struct {
	KeySize int `yaml:"key_size"`
}

type StorageConfig struct {
	ScriptsDir  string `yaml:"scripts_dir"`
	ReleasesDir string `yaml:"releases_dir"`
}

type LogConfig struct {
	Level      string `yaml:"level"`
	File       string `yaml:"file"`
	MaxSize    int    `yaml:"max_size"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAge     int    `yaml:"max_age"`
}

type EmailConfig struct {
	Enabled  bool   `yaml:"enabled"`
	SMTPHost string `yaml:"smtp_host"`
	SMTPPort int    `yaml:"smtp_port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
}

type SecurityConfig struct {
	// 登录安全
	MaxLoginAttempts int `yaml:"max_login_attempts"` // 最大登录尝试次数
	LoginLockMinutes int `yaml:"login_lock_minutes"` // 登录锁定时间（分钟）
	IPMaxAttempts    int `yaml:"ip_max_attempts"`    // IP 最大尝试次数
	IPLockMinutes    int `yaml:"ip_lock_minutes"`    // IP 锁定时间（分钟）

	// 密码策略
	PasswordMinLength  int  `yaml:"password_min_length"`  // 密码最小长度
	PasswordRequireNum bool `yaml:"password_require_num"` // 密码需要数字
	PasswordRequireSym bool `yaml:"password_require_sym"` // 密码需要特殊字符

	// CSRF 保护
	CSRFEnabled     bool   `yaml:"csrf_enabled"`      // 是否启用 CSRF 保护
	CSRFTokenExpiry int    `yaml:"csrf_token_expiry"` // CSRF Token 过期时间（分钟）
	CSRFCookieName  string `yaml:"csrf_cookie_name"`  // CSRF Cookie 名称

	// 安全头
	EnableSecurityHeaders bool `yaml:"enable_security_headers"` // 是否启用安全响应头

	// 允许的来源（CORS）
	AllowedOrigins []string `yaml:"allowed_origins"`

	// 上传与请求限制
	MaxReleaseUploadMB      int `yaml:"max_release_upload_mb"`       // 发布版本上传大小上限（MB）
	MaxRequestBodyMB        int `yaml:"max_request_body_mb"`         // 全局请求体大小上限（MB）
	MultipartMemoryMB       int `yaml:"multipart_memory_mb"`         // multipart 解析内存上限（MB）
	MaxScriptUploadMB       int `yaml:"max_script_upload_mb"`        // 普通脚本上传大小上限（MB）
	MaxSecureScriptUploadMB int `yaml:"max_secure_script_upload_mb"` // 安全脚本上传大小上限（MB）

	// 客户端下载令牌
	DownloadTokenSecret        string `yaml:"download_token_secret"`         // 下载 token 密钥（留空复用 JWT Secret）
	DownloadTokenExpireSeconds int    `yaml:"download_token_expire_seconds"` // 下载 token 过期时间（秒）
}

var globalConfig *Config

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 设置默认值
	setDefaults(&cfg)

	// 安全检查
	if err := validateSecurity(&cfg); err != nil {
		return nil, err
	}

	globalConfig = &cfg
	return &cfg, nil
}

func Get() *Config {
	return globalConfig
}

// setDefaults 设置默认值
func setDefaults(cfg *Config) {
	// 安全配置默认值
	if cfg.Security.MaxLoginAttempts == 0 {
		cfg.Security.MaxLoginAttempts = 5
	}
	if cfg.Security.LoginLockMinutes == 0 {
		cfg.Security.LoginLockMinutes = 15
	}
	if cfg.Security.IPMaxAttempts == 0 {
		cfg.Security.IPMaxAttempts = 20
	}
	if cfg.Security.IPLockMinutes == 0 {
		cfg.Security.IPLockMinutes = 30
	}
	if cfg.Security.PasswordMinLength == 0 {
		cfg.Security.PasswordMinLength = 6
	}
	if cfg.Security.CSRFTokenExpiry == 0 {
		cfg.Security.CSRFTokenExpiry = 60
	}
	if cfg.Security.CSRFCookieName == "" {
		cfg.Security.CSRFCookieName = "csrf_token"
	}
	if cfg.Security.MaxReleaseUploadMB == 0 {
		cfg.Security.MaxReleaseUploadMB = 500
	}
	if cfg.Security.MaxRequestBodyMB == 0 {
		cfg.Security.MaxRequestBodyMB = 1024
	}
	if cfg.Security.MultipartMemoryMB == 0 {
		cfg.Security.MultipartMemoryMB = 32
	}
	if cfg.Security.MaxScriptUploadMB == 0 {
		cfg.Security.MaxScriptUploadMB = 20
	}
	if cfg.Security.MaxSecureScriptUploadMB == 0 {
		cfg.Security.MaxSecureScriptUploadMB = 20
	}
	if cfg.Security.DownloadTokenExpireSeconds == 0 {
		cfg.Security.DownloadTokenExpireSeconds = 300
	}
}

// validateSecurity 验证安全配置
func validateSecurity(cfg *Config) error {
	// 检查 JWT Secret
	if cfg.JWT.Secret == "" || cfg.JWT.Secret == "your-jwt-secret-key-change-in-production" {
		if cfg.Server.Mode == "release" {
			return fmt.Errorf("生产环境必须设置安全的 JWT Secret")
		}
		// 开发环境自动生成随机密钥
		cfg.JWT.Secret = generateRandomSecret(32)
		fmt.Println("[WARNING] 使用自动生成的 JWT Secret，请在生产环境配置安全的密钥")
	}

	// 检查 JWT Secret 长度
	if len(cfg.JWT.Secret) < 32 {
		if cfg.Server.Mode == "release" {
			return fmt.Errorf("JWT Secret 长度至少需要 32 个字符")
		}
		fmt.Println("[WARNING] JWT Secret 长度建议至少 32 个字符")
	}

	return nil
}

// generateRandomSecret 生成随机密钥
func generateRandomSecret(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
