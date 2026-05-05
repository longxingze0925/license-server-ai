package crypto

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	masterKey, err := GenerateAESKey()
	if err != nil {
		t.Fatalf("GenerateAESKey: %v", err)
	}

	plaintext := []byte("sk-proj-this-is-a-fake-openai-api-key-12345")

	apiKeyEnc, dekEnc, err := EnvelopeEncrypt(masterKey, plaintext)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}
	if bytes.Contains(apiKeyEnc, plaintext) {
		t.Fatal("加密后密文里居然能找到原文，加密无效")
	}
	if bytes.Contains(dekEnc, masterKey) {
		t.Fatal("DEK 密文里居然能找到主密钥，加密无效")
	}

	got, err := EnvelopeDecrypt(masterKey, apiKeyEnc, dekEnc)
	if err != nil {
		t.Fatalf("EnvelopeDecrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("解密结果不一致：want=%s got=%s", plaintext, got)
	}
}

func TestEnvelopeWrongMasterKey(t *testing.T) {
	master1, _ := GenerateAESKey()
	master2, _ := GenerateAESKey()
	plain := []byte("secret")

	apiEnc, dekEnc, err := EnvelopeEncrypt(master1, plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnvelopeDecrypt(master2, apiEnc, dekEnc); err == nil {
		t.Fatal("用错的主密钥居然解密成功")
	}
}

func TestEnvelopeShortMasterKey(t *testing.T) {
	short := []byte("too-short")
	if _, _, err := EnvelopeEncrypt(short, []byte("x")); err == nil {
		t.Fatal("应该拒绝 < 32 字节的主密钥")
	}
}

func TestEnvMasterKeyProvider_OK(t *testing.T) {
	// 生成 32B 主密钥并塞进环境变量
	master, _ := GenerateAESKey()
	encoded := base64.StdEncoding.EncodeToString(master)
	t.Setenv(MasterKeyEnvVar, encoded)

	provider := NewEnvMasterKeyProvider()
	got, err := provider.GetMasterKey(context.Background())
	if err != nil {
		t.Fatalf("GetMasterKey: %v", err)
	}
	if !bytes.Equal(got, master) {
		t.Fatal("Provider 返回的主密钥与设置的不一致")
	}
	if provider.KeyID() != "env-v1" {
		t.Fatalf("KeyID 期望 env-v1，得到 %s", provider.KeyID())
	}
}

func TestEnvMasterKeyProvider_Missing(t *testing.T) {
	os.Unsetenv(MasterKeyEnvVar)
	provider := NewEnvMasterKeyProvider()
	if _, err := provider.GetMasterKey(context.Background()); err == nil {
		t.Fatal("环境变量未设置时应该报错")
	}
}

func TestEnvMasterKeyProvider_BadBase64(t *testing.T) {
	t.Setenv(MasterKeyEnvVar, "this-is-not-base64-!!!")
	provider := NewEnvMasterKeyProvider()
	if _, err := provider.GetMasterKey(context.Background()); err == nil {
		t.Fatal("非法 base64 时应该报错")
	}
}

func TestEnvMasterKeyProvider_WrongLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("only-16-bytes!!!"))
	t.Setenv(MasterKeyEnvVar, short)
	provider := NewEnvMasterKeyProvider()
	if _, err := provider.GetMasterKey(context.Background()); err == nil {
		t.Fatal("长度不是 32 字节时应该报错")
	}
}

func TestGenerateMasterKeyBase64(t *testing.T) {
	encoded, err := GenerateMasterKeyBase64()
	if err != nil {
		t.Fatalf("GenerateMasterKeyBase64: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("生成的 base64 解码失败: %v", err)
	}
	if len(decoded) != MasterKeyLength {
		t.Fatalf("生成的主密钥长度 %d，期望 %d", len(decoded), MasterKeyLength)
	}
}
