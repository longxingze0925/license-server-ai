package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileStorageProvider 把生成结果文件存到某种后端：本机磁盘 / MinIO / 云对象存储。
//
// 第一阶段只实现 LocalFSProvider；接口预留扩展，后续新增 S3 / OSS 不动业务代码。
//
// 路径含义：
//   - root：存储根目录，由 config.yaml 注入；
//   - relPath：相对 root 的路径，建议形如 "generations/2026-04/{user_id}/{task_id}.mp4"；
//   - 业务层只持 relPath，不持绝对路径；切换 storage backend 只换 root / 实现。
type FileStorageProvider interface {
	// Save 把 reader 内容写到 relPath，返回实际写入字节数。
	// 若 relPath 父目录不存在会自动创建（多级）。
	Save(ctx context.Context, relPath string, r io.Reader) (size int64, err error)

	// Open 打开 relPath 用于读取。返回的 ReadSeekCloser 支持 Range（用于视频拖进度）。
	Open(ctx context.Context, relPath string) (io.ReadSeekCloser, error)

	// Delete 删除 relPath 对应文件；不存在返回 nil（幂等）。
	Delete(ctx context.Context, relPath string) error

	// Stat 取文件元信息。不存在返回 os.IsNotExist 错误。
	Stat(ctx context.Context, relPath string) (FileStat, error)
}

// FileStat 简化版 fs.FileInfo。
type FileStat struct {
	Size    int64
	ModTime time.Time
}

// ====================== LocalFSProvider ======================

// LocalFSProvider 把文件存到本机磁盘。
type LocalFSProvider struct {
	root string
	mu   sync.Mutex // 创建目录时的并发保护
}

// NewLocalFSProvider 用绝对/相对路径作为根目录；启动期会确保根目录存在并可写。
func NewLocalFSProvider(root string) (*LocalFSProvider, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("文件存储根目录不能为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析根目录失败: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("创建根目录失败: %w", err)
	}
	// 写一个探针文件试试能不能写
	probe := filepath.Join(abs, ".write_probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return nil, fmt.Errorf("根目录不可写: %w", err)
	}
	_ = os.Remove(probe)

	return &LocalFSProvider{root: abs}, nil
}

// Root 返回根目录绝对路径（仅用于日志/admin 展示，不要拼接业务文件路径）。
func (p *LocalFSProvider) Root() string { return p.root }

func (p *LocalFSProvider) full(relPath string) (string, error) {
	// 显式拒绝任何 ".." 段，防止"安静地"把路径重写到 root 内的别处。
	slashed := filepath.ToSlash(relPath)
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return "", fmt.Errorf("非法相对路径（含 ..）: %q", relPath)
		}
	}
	clean := filepath.Clean("/" + slashed)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		return "", fmt.Errorf("非法相对路径: %q", relPath)
	}
	return filepath.Join(p.root, filepath.FromSlash(clean)), nil
}

func (p *LocalFSProvider) Save(_ context.Context, relPath string, r io.Reader) (int64, error) {
	full, err := p.full(relPath)
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		p.mu.Unlock()
		return 0, err
	}
	p.mu.Unlock()

	tmp := full + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return written, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return written, closeErr
	}
	// 原子 rename，避免半截文件被读取
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return written, err
	}
	return written, nil
}

func (p *LocalFSProvider) Open(_ context.Context, relPath string) (io.ReadSeekCloser, error) {
	full, err := p.full(relPath)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

func (p *LocalFSProvider) Delete(_ context.Context, relPath string) error {
	full, err := p.full(relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// 顺便删空目录（best-effort，不报错）
	_ = os.Remove(filepath.Dir(full))
	return nil
}

func (p *LocalFSProvider) Stat(_ context.Context, relPath string) (FileStat, error) {
	full, err := p.full(relPath)
	if err != nil {
		return FileStat{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return FileStat{}, err
	}
	return FileStat{Size: info.Size(), ModTime: info.ModTime()}, nil
}

// ====================== 单例注册 ======================

var (
	storageOnce     sync.Once
	storageProvider FileStorageProvider
	storageInitErr  error
)

// InitFileStorage 在 main.go 启动期注入。第一版用 LocalFSProvider。
func InitFileStorage(p FileStorageProvider) {
	storageOnce.Do(func() {
		storageProvider = p
	})
}

// GetFileStorage 取已注入的 FileStorageProvider。
func GetFileStorage() FileStorageProvider {
	return storageProvider
}

// 占位防 unused
var _ = storageInitErr
