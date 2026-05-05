package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"license-server/internal/config"
	"os"
	"path/filepath"
)

const (
	defaultMaxScriptUploadSizeBytes       int64 = 20 << 20 // 20MB
	defaultMaxSecureScriptUploadSizeBytes int64 = 20 << 20 // 20MB
)

var errUploadFileTooLarge = errors.New("uploaded file too large")

type stagedUploadedFile struct {
	FinalPath string
	TempPath  string
	Size      int64
	Hash      string
	committed bool
}

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
	staged, err := stageUploadedFile(src, filePath)
	if err != nil {
		return 0, "", err
	}
	defer staged.Cleanup()
	if err := staged.Commit(); err != nil {
		return 0, "", err
	}
	return staged.Size, staged.Hash, nil
}

func stageUploadedFile(src io.Reader, filePath string) (*stagedUploadedFile, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	dst, err := os.CreateTemp(dir, filepath.Base(filePath)+".*.part")
	if err != nil {
		return nil, err
	}
	tempPath := dst.Name()

	hasher := sha256.New()
	writer := io.MultiWriter(dst, hasher)
	size, err := io.Copy(writer, src)
	if err != nil {
		_ = dst.Close()
		_ = os.Remove(tempPath)
		return nil, err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}

	return &stagedUploadedFile{
		FinalPath: filePath,
		TempPath:  tempPath,
		Size:      size,
		Hash:      hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func (f *stagedUploadedFile) Commit() error {
	if f == nil || f.committed {
		return nil
	}
	if err := replaceUploadedFile(f.TempPath, f.FinalPath); err != nil {
		return err
	}
	f.committed = true
	return nil
}

func (f *stagedUploadedFile) Cleanup() {
	if f == nil || f.committed {
		return
	}
	_ = os.Remove(f.TempPath)
}

func replaceUploadedFile(tempPath, finalPath string) error {
	if err := os.Rename(tempPath, finalPath); err == nil {
		return nil
	}
	if err := os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tempPath, finalPath)
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
