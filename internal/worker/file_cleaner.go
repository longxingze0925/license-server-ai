package worker

import (
	"context"
	"log"
	"time"

	"license-server/internal/service"
)

// FileCleaner 周期性清理过期生成文件。
//
// 默认每小时跑一次，每轮清理最多 200 条；过期判定 = expires_at < NOW()。
// 删除顺序：先删磁盘文件，再删 generation_files 行 —— 反过来会留孤文件。
//
// 第一阶段不做"清理前 N 天通知"。
type FileCleaner struct {
	files    *service.GenerationFileService
	interval time.Duration
	batch    int
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewFileCleaner 创建清理器。interval 为 0 时默认 1 小时；batch 为 0 时默认 200。
func NewFileCleaner(files *service.GenerationFileService, interval time.Duration, batch int) *FileCleaner {
	if interval <= 0 {
		interval = time.Hour
	}
	if batch <= 0 {
		batch = 200
	}
	return &FileCleaner{
		files:    files,
		interval: interval,
		batch:    batch,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

func (c *FileCleaner) Start() {
	go c.loop()
}

func (c *FileCleaner) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *FileCleaner) loop() {
	defer close(c.doneCh)

	// 启动后立即跑一次（追上上次停机期间过期的文件），再走周期。
	c.runOnce()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.runOnce()
		}
	}
}

func (c *FileCleaner) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	n, err := c.files.DeleteExpired(ctx, time.Now(), c.batch)
	if err != nil {
		log.Printf("[file_cleaner] DeleteExpired err: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[file_cleaner] 清理 %d 个过期文件", n)
	}
}
