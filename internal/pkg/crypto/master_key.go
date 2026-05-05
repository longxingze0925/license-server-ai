package crypto

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"
)

// 主密钥用途：
// 平台统一管理 Provider API Key 时采用信封加密。
// 1) 每条凭证生成一次性 DEK，用 DEK 加密真实 Key   → api_key_enc
// 2) 用主密钥加密 DEK                               → dek_enc
// 主密钥永不入库、永不写日志，由 MasterKeyProvider 在进程启动时加载到内存。
//
// 第一版实现：EnvMasterKeyProvider（从 LICENSE_MASTER_KEY 环境变量读 base64 编码的 32B）。
// 未来扩展：VaultProvider / AliyunKMSProvider / AwsKMSProvider —— 切换只改 main.go 的注入。

const (
	MasterKeyEnvVar = "LICENSE_MASTER_KEY"
	MasterKeyLength = 32 // AES-256
)

// MasterKeyProvider 提供主密钥及其标识。
// 调用方应当在启动后获取一次并缓存，所有加解密操作复用。
type MasterKeyProvider interface {
	GetMasterKey(ctx context.Context) ([]byte, error)
	KeyID() string // 用于 dek_enc 头部，方便未来轮换；当前实现固定返回 "env-v1"
}

// EnvMasterKeyProvider 从环境变量读取主密钥。
type EnvMasterKeyProvider struct {
	once sync.Once
	key  []byte
	err  error
}

// NewEnvMasterKeyProvider 创建从环境变量读取主密钥的 Provider。
func NewEnvMasterKeyProvider() *EnvMasterKeyProvider {
	return &EnvMasterKeyProvider{}
}

func (p *EnvMasterKeyProvider) GetMasterKey(_ context.Context) ([]byte, error) {
	p.once.Do(func() {
		raw := os.Getenv(MasterKeyEnvVar)
		if raw == "" {
			p.err = fmt.Errorf("环境变量 %s 未设置", MasterKeyEnvVar)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			p.err = fmt.Errorf("环境变量 %s 不是合法 base64: %w", MasterKeyEnvVar, err)
			return
		}
		if len(decoded) != MasterKeyLength {
			p.err = fmt.Errorf("环境变量 %s 解码后长度必须为 %d 字节，实际 %d", MasterKeyEnvVar, MasterKeyLength, len(decoded))
			return
		}
		p.key = decoded
	})
	if p.err != nil {
		return nil, p.err
	}
	return p.key, nil
}

func (p *EnvMasterKeyProvider) KeyID() string {
	return "env-v1"
}

// GenerateMasterKeyBase64 生成一个新的 32B 主密钥并以 base64 编码返回。
// 用于运维首次部署时生成 LICENSE_MASTER_KEY 环境变量值。
func GenerateMasterKeyBase64() (string, error) {
	key, err := GenerateAESKey()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// EnvelopeEncrypt 信封加密：返回 (api_key_enc, dek_enc)。
//
//	api_key_enc = AES-256-GCM(DEK, plaintext)
//	dek_enc     = AES-256-GCM(MasterKey, DEK)
//
// 调用方应在使用完 DEK 后立即清零（本函数已自行清零）。
func EnvelopeEncrypt(masterKey []byte, plaintext []byte) (apiKeyEnc, dekEnc []byte, err error) {
	if len(masterKey) != MasterKeyLength {
		return nil, nil, fmt.Errorf("主密钥长度必须为 %d 字节", MasterKeyLength)
	}

	dek, err := GenerateAESKey()
	if err != nil {
		return nil, nil, fmt.Errorf("生成 DEK 失败: %w", err)
	}
	defer zero(dek)

	apiKeyEnc, err = EncryptAESGCM(plaintext, dek)
	if err != nil {
		return nil, nil, fmt.Errorf("加密 API Key 失败: %w", err)
	}

	dekEnc, err = EncryptAESGCM(dek, masterKey)
	if err != nil {
		return nil, nil, fmt.Errorf("加密 DEK 失败: %w", err)
	}

	return apiKeyEnc, dekEnc, nil
}

// EnvelopeDecrypt 信封解密。调用方使用完明文后应自行清零。
func EnvelopeDecrypt(masterKey, apiKeyEnc, dekEnc []byte) ([]byte, error) {
	if len(masterKey) != MasterKeyLength {
		return nil, fmt.Errorf("主密钥长度必须为 %d 字节", MasterKeyLength)
	}
	if len(apiKeyEnc) == 0 || len(dekEnc) == 0 {
		return nil, errors.New("密文为空")
	}

	dek, err := DecryptAESGCM(dekEnc, masterKey)
	if err != nil {
		return nil, fmt.Errorf("解密 DEK 失败（主密钥可能错误）: %w", err)
	}
	defer zero(dek)

	plain, err := DecryptAESGCM(apiKeyEnc, dek)
	if err != nil {
		return nil, fmt.Errorf("解密 API Key 失败: %w", err)
	}
	return plain, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
