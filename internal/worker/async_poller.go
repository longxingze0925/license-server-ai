// Package worker 包含后台周期性任务（异步轮询、文件清理等）。
package worker

import (
	"context"
	"log"
	"time"

	"license-server/internal/service"
)

// AsyncPoller 周期性扫描 status=running 的生成任务，调上游推进状态。
//
// 默认每 5 秒一轮；任务量上去之后可以提到 2-3 秒，但要注意上游 rate limit。
type AsyncPoller struct {
	runner   *service.AsyncRunnerService
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewAsyncPoller 创建一个 poller。interval 为 0 时默认 5s。
func NewAsyncPoller(runner *service.AsyncRunnerService, interval time.Duration) *AsyncPoller {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &AsyncPoller{
		runner:   runner,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start 启动后台 goroutine。Stop 后会等到一轮 PollOnce 自然结束（最多 interval 长度）。
func (p *AsyncPoller) Start() {
	go p.loop()
}

// Stop 通知 goroutine 退出并等待。
func (p *AsyncPoller) Stop() {
	close(p.stopCh)
	<-p.doneCh
}

func (p *AsyncPoller) loop() {
	defer close(p.doneCh)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			n, err := p.runner.PollOnce(ctx)
			cancel()
			if err != nil {
				log.Printf("[async_poller] PollOnce err: %v", err)
			}
			if n > 0 {
				log.Printf("[async_poller] 推进了 %d 个任务", n)
			}
		}
	}
}
