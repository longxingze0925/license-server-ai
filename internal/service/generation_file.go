package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"license-server/internal/config"
	"license-server/internal/model"

	"gorm.io/gorm"
)

// GenerationFileService 生成结果文件元数据 + 物理文件协同管理。
//
// 写入：在 async_poller 完成后调用 SaveResult(taskID, userID, kind, mime, reader) →
//
//	落盘到 LocalFS + 写 generation_files 行 + 设置 expires_at = now + 保留天数。
//
// 读取：handler 调 OpenForUser(userID, fileID) 拿到 row + 流，自动校验所有权。
//
// 删除：用户主动删除 / cron 清理过期，都走 DeletePhysicalAndRow（先删盘后删表）。
type GenerationFileService struct {
	storage  FileStorageProvider
	keepDays int
}

func NewGenerationFileService() *GenerationFileService {
	cfg := config.Get()
	keep := 15
	if cfg != nil && cfg.Storage.GenerationKeepDays > 0 {
		keep = cfg.Storage.GenerationKeepDays
	}
	return &GenerationFileService{
		storage:  GetFileStorage(),
		keepDays: keep,
	}
}

// 错误。
var (
	ErrFileNotFound  = errors.New("文件不存在")
	ErrFileForbidden = errors.New("无权访问该文件")
)

// SaveResult 异步任务完成后保存生成的媒体文件。
//
// 路径形态：generations/{yyyy-MM}/{user_id}/{task_id}.{ext}（多个文件时附加 -1 -2 ...）
func (s *GenerationFileService) SaveResult(
	ctx context.Context,
	taskID, userID string,
	kind model.GenerationFileKind,
	mimeType string,
	originalURL string, // 仅用于推断扩展名
	reader io.Reader,
	durationMs, width, height int,
) (*model.GenerationFile, error) {
	if s.storage == nil {
		return nil, errors.New("FileStorage 未初始化")
	}
	ext := guessExt(mimeType, originalURL)
	now := time.Now()
	// FileStorage root 已经是 storage/generations/，所以这里直接用 yyyy-MM 而不加 "generations/" 前缀。
	relPath := fmt.Sprintf("%s/%s/%s%s", now.Format("2006-01"), userID, taskID, ext)

	// 已存在同名 → 加序号（多文件场景）
	suffix := 0
	for {
		if _, err := s.storage.Stat(ctx, relPath); err != nil {
			break
		}
		suffix++
		relPath = fmt.Sprintf("%s/%s/%s-%d%s", now.Format("2006-01"), userID, taskID, suffix, ext)
	}

	size, err := s.storage.Save(ctx, relPath, reader)
	if err != nil {
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}

	row := &model.GenerationFile{
		TaskID:     taskID,
		UserID:     userID,
		Kind:       kind,
		Storage:    "local",
		Path:       relPath,
		SizeBytes:  size,
		MimeType:   mimeType,
		DurationMs: durationMs,
		Width:      width,
		Height:     height,
		ExpiresAt:  now.AddDate(0, 0, s.keepDays),
	}
	if err := model.DB.Create(row).Error; err != nil {
		// 写元数据失败 → 把刚落盘的文件清掉，避免孤儿
		_ = s.storage.Delete(ctx, relPath)
		return nil, fmt.Errorf("写文件元数据失败: %w", err)
	}
	return row, nil
}

// GetForUser 取文件元信息，并校验 user 拥有该文件。
func (s *GenerationFileService) GetForUser(fileID, userID string) (*model.GenerationFile, error) {
	var row model.GenerationFile
	err := model.DB.First(&row, "id = ?", fileID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	if row.UserID != userID {
		return nil, ErrFileForbidden
	}
	return &row, nil
}

// Open 打开文件（已校验所有权）。调用方负责关闭返回的 reader。
func (s *GenerationFileService) Open(ctx context.Context, row *model.GenerationFile) (io.ReadSeekCloser, error) {
	if s.storage == nil {
		return nil, errors.New("FileStorage 未初始化")
	}
	return s.storage.Open(ctx, row.Path)
}

// DeleteForUser 用户主动删除自己的文件。返回 false 表示不存在 / 没权限。
func (s *GenerationFileService) DeleteForUser(ctx context.Context, fileID, userID string) error {
	row, err := s.GetForUser(fileID, userID)
	if err != nil {
		return err
	}
	return s.deletePhysicalAndRow(ctx, row)
}

// DeleteExpired 清理 expires_at < now 的所有文件。返回清理数量。
// 用于 file_cleaner cron。
func (s *GenerationFileService) DeleteExpired(ctx context.Context, now time.Time, batch int) (int, error) {
	if batch <= 0 {
		batch = 200
	}
	var rows []model.GenerationFile
	if err := model.DB.
		Where("expires_at < ?", now).
		Order("expires_at ASC").
		Limit(batch).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	deleted := 0
	for i := range rows {
		if err := s.deletePhysicalAndRow(ctx, &rows[i]); err != nil {
			// 单条失败不阻塞批次（多半是磁盘文件已被外部删过），只记一下数。
			continue
		}
		deleted++
	}
	return deleted, nil
}

// ListForUser 用户分页查看自己的文件。
func (s *GenerationFileService) ListForUser(userID string, page, pageSize int) ([]model.GenerationFile, int64, error) {
	q := model.DB.Where("user_id = ?", userID)
	var total int64
	if err := q.Model(&model.GenerationFile{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	var rows []model.GenerationFile
	if err := q.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ====================== 内部 ======================

func (s *GenerationFileService) ListForTask(taskID string) ([]model.GenerationFile, error) {
	var rows []model.GenerationFile
	if err := model.DB.
		Where("task_id = ?", taskID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *GenerationFileService) DeleteForTask(ctx context.Context, taskID string) error {
	rows, err := s.ListForTask(taskID)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := s.deletePhysicalAndRow(ctx, &rows[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *GenerationFileService) deletePhysicalAndRow(ctx context.Context, row *model.GenerationFile) error {
	// 顺序：先删磁盘文件，后删 DB 行；失败时如果磁盘删了 DB 没删，下次清理还能再来一遍（幂等）。
	if s.storage != nil {
		if err := s.storage.Delete(ctx, row.Path); err != nil {
			return err
		}
	}
	return model.DB.Delete(&model.GenerationFile{}, "id = ?", row.ID).Error
}

// guessExt 根据 MIME 与原始 URL 推断扩展名。
func guessExt(mime, url string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch {
	case strings.HasPrefix(mime, "video/mp4"):
		return ".mp4"
	case strings.HasPrefix(mime, "video/webm"):
		return ".webm"
	case strings.HasPrefix(mime, "video/quicktime"):
		return ".mov"
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	}
	// 退而求其次：从 URL 取
	ext := strings.ToLower(path.Ext(strings.SplitN(url, "?", 2)[0]))
	if ext != "" && len(ext) <= 6 {
		return ext
	}
	return ".bin"
}
