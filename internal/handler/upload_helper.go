package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"license-server/internal/config"
	"os"
)

const (
	defaultMaxScriptUploadSizeBytes       int64 = 20 << 20 // 20MB
	defaultMaxSecureScriptUploadSizeBytes int64 = 20 << 20 // 20MB
)

var errUploadFileTooLarge = errors.New("uploaded file too large")

func resolveUploadLimitBytes(configMB int, defaultBytes int64) int64 {
	if configMB <= 0 {
		return defaultBytes
	}
	return int64(configMB) << 20
}

func getMaxScriptUploadSizeBytes() int64 {
	cfg := config.Get()
	if cfg == nil {
		return defaultMaxScriptUploadSizeBytes
	}
	return resolveUploadLimitBytes(cfg.Security.MaxScriptUploadMB, defaultMaxScriptUploadSizeBytes)
}

func getMaxSecureScriptUploadSizeBytes() int64 {
	cfg := config.Get()
	if cfg == nil {
		return defaultMaxSecureScriptUploadSizeBytes
	}
	return resolveUploadLimitBytes(cfg.Security.MaxSecureScriptUploadMB, defaultMaxSecureScriptUploadSizeBytes)
}

// saveUploadedFile 按流式写入文件并计算 SHA256，避免大文件整包读入内存。
func saveUploadedFile(src io.Reader, filePath string) (int64, string, error) {
	dst, err := os.Create(filePath)
	if err != nil {
		return 0, "", err
	}
	defer dst.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(dst, hasher)
	size, err := io.Copy(writer, src)
	if err != nil {
		return 0, "", err
	}

	return size, hex.EncodeToString(hasher.Sum(nil)), nil
}

// readUploadedContentWithLimit 读取上传内容并限制最大大小，防止内存占用过高。
func readUploadedContentWithLimit(src io.Reader, maxBytes int64) ([]byte, error) {
	limited := &io.LimitedReader{R: src, N: maxBytes + 1}
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, errUploadFileTooLarge
	}
	return content, nil
}
