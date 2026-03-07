package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

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

