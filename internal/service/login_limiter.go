package service

import (
	"license-server/internal/config"
	"sync"
	"time"
)

// LoginAttempt 登录尝试记录
type LoginAttempt struct {
	FailCount   int       // 失败次数
	LastAttempt time.Time // 最后尝试时间
	LockedUntil time.Time // 锁定截止时间
}

// LoginLimiter 登录限制器
type LoginLimiter struct {
	attempts     map[string]*LoginAttempt
	mu           sync.RWMutex
	maxAttempts  int           // 最大尝试次数
	lockDuration time.Duration // 锁定时长
	resetAfter   time.Duration // 重置时间（无失败后多久重置计数）
}

var (
	defaultLoginLimiter *LoginLimiter
	loginLimiterOnce    sync.Once
)

// GetLoginLimiter 获取登录限制器单例
func GetLoginLimiter() *LoginLimiter {
	loginLimiterOnce.Do(func() {
		maxAttempts, lockDuration := loginLimitConfig()
		defaultLoginLimiter = NewLoginLimiter(maxAttempts, lockDuration, lockDuration*2)
	})
	return defaultLoginLimiter
}

// NewLoginLimiter 创建登录限制器
func NewLoginLimiter(maxAttempts int, lockDuration, resetAfter time.Duration) *LoginLimiter {
	ll := &LoginLimiter{
		attempts:     make(map[string]*LoginAttempt),
		maxAttempts:  maxAttempts,
		lockDuration: lockDuration,
		resetAfter:   resetAfter,
	}
	go ll.cleanup()
	return ll
}

// IsLocked 检查账号是否被锁定
func (ll *LoginLimiter) IsLocked(key string) (bool, time.Duration) {
	ll.mu.RLock()
	defer ll.mu.RUnlock()

	attempt, exists := ll.attempts[key]
	if !exists {
		return false, 0
	}

	if time.Now().Before(attempt.LockedUntil) {
		remaining := time.Until(attempt.LockedUntil)
		return true, remaining
	}

	return false, 0
}

// RecordFailure 记录登录失败
func (ll *LoginLimiter) RecordFailure(key string) (locked bool, remaining time.Duration) {
	ll.mu.Lock()
	defer ll.mu.Unlock()

	now := time.Now()
	attempt, exists := ll.attempts[key]

	if !exists {
		attempt = &LoginAttempt{}
		ll.attempts[key] = attempt
	}

	// 如果已过重置时间，重置计数
	if now.Sub(attempt.LastAttempt) > ll.resetAfter {
		attempt.FailCount = 0
	}

	attempt.FailCount++
	attempt.LastAttempt = now

	// 达到最大尝试次数，锁定账号
	if attempt.FailCount >= ll.maxAttempts {
		attempt.LockedUntil = now.Add(ll.lockDuration)
		return true, ll.lockDuration
	}

	return false, 0
}

// RecordSuccess 记录登录成功，清除失败记录
func (ll *LoginLimiter) RecordSuccess(key string) {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	delete(ll.attempts, key)
}

// GetRemainingAttempts 获取剩余尝试次数
func (ll *LoginLimiter) GetRemainingAttempts(key string) int {
	ll.mu.RLock()
	defer ll.mu.RUnlock()

	attempt, exists := ll.attempts[key]
	if !exists {
		return ll.maxAttempts
	}

	// 如果已过重置时间
	if time.Now().Sub(attempt.LastAttempt) > ll.resetAfter {
		return ll.maxAttempts
	}

	remaining := ll.maxAttempts - attempt.FailCount
	if remaining < 0 {
		return 0
	}
	return remaining
}

// cleanup 定期清理过期记录
func (ll *LoginLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		ll.mu.Lock()
		now := time.Now()
		for key, attempt := range ll.attempts {
			// 清理已解锁且超过重置时间的记录
			if now.After(attempt.LockedUntil) && now.Sub(attempt.LastAttempt) > ll.resetAfter {
				delete(ll.attempts, key)
			}
		}
		ll.mu.Unlock()
	}
}

// IPLoginLimiter IP 登录限制器（防止同一 IP 大量尝试不同账号）
type IPLoginLimiter struct {
	attempts     map[string]*LoginAttempt
	mu           sync.RWMutex
	maxAttempts  int
	lockDuration time.Duration
	resetAfter   time.Duration
}

var (
	defaultIPLimiter *IPLoginLimiter
	ipLimiterOnce    sync.Once
)

// GetIPLoginLimiter 获取 IP 登录限制器单例
func GetIPLoginLimiter() *IPLoginLimiter {
	ipLimiterOnce.Do(func() {
		maxAttempts, lockDuration := ipLoginLimitConfig()
		defaultIPLimiter = NewIPLoginLimiter(maxAttempts, lockDuration, lockDuration*2)
	})
	return defaultIPLimiter
}

func loginLimitConfig() (int, time.Duration) {
	if cfg := config.Get(); cfg != nil {
		maxAttempts := cfg.Security.MaxLoginAttempts
		lockMinutes := cfg.Security.LoginLockMinutes
		if maxAttempts > 0 && lockMinutes > 0 {
			return maxAttempts, time.Duration(lockMinutes) * time.Minute
		}
	}
	return 5, 15 * time.Minute
}

func ipLoginLimitConfig() (int, time.Duration) {
	if cfg := config.Get(); cfg != nil {
		maxAttempts := cfg.Security.IPMaxAttempts
		lockMinutes := cfg.Security.IPLockMinutes
		if maxAttempts > 0 && lockMinutes > 0 {
			return maxAttempts, time.Duration(lockMinutes) * time.Minute
		}
	}
	return 20, 30 * time.Minute
}

// NewIPLoginLimiter 创建 IP 登录限制器
func NewIPLoginLimiter(maxAttempts int, lockDuration, resetAfter time.Duration) *IPLoginLimiter {
	ll := &IPLoginLimiter{
		attempts:     make(map[string]*LoginAttempt),
		maxAttempts:  maxAttempts,
		lockDuration: lockDuration,
		resetAfter:   resetAfter,
	}
	go ll.cleanup()
	return ll
}

// IsLocked 检查 IP 是否被锁定
func (ll *IPLoginLimiter) IsLocked(ip string) (bool, time.Duration) {
	ll.mu.RLock()
	defer ll.mu.RUnlock()

	attempt, exists := ll.attempts[ip]
	if !exists {
		return false, 0
	}

	if time.Now().Before(attempt.LockedUntil) {
		return true, time.Until(attempt.LockedUntil)
	}

	return false, 0
}

// RecordFailure 记录失败
func (ll *IPLoginLimiter) RecordFailure(ip string) (locked bool, remaining time.Duration) {
	ll.mu.Lock()
	defer ll.mu.Unlock()

	now := time.Now()
	attempt, exists := ll.attempts[ip]

	if !exists {
		attempt = &LoginAttempt{}
		ll.attempts[ip] = attempt
	}

	if now.Sub(attempt.LastAttempt) > ll.resetAfter {
		attempt.FailCount = 0
	}

	attempt.FailCount++
	attempt.LastAttempt = now

	if attempt.FailCount >= ll.maxAttempts {
		attempt.LockedUntil = now.Add(ll.lockDuration)
		return true, ll.lockDuration
	}

	return false, 0
}

// RecordSuccess 记录成功
func (ll *IPLoginLimiter) RecordSuccess(ip string) {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	// IP 限制器成功时只减少计数，不完全清除
	if attempt, exists := ll.attempts[ip]; exists {
		attempt.FailCount--
		if attempt.FailCount <= 0 {
			delete(ll.attempts, ip)
		}
	}
}

// cleanup 清理过期记录
func (ll *IPLoginLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		ll.mu.Lock()
		now := time.Now()
		for key, attempt := range ll.attempts {
			if now.After(attempt.LockedUntil) && now.Sub(attempt.LastAttempt) > ll.resetAfter {
				delete(ll.attempts, key)
			}
		}
		ll.mu.Unlock()
	}
}
