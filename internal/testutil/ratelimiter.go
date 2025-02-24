package testutil

import (
	"sync"
	"time"
)

// RateLimiter 实现一个简单的限流器
type RateLimiter struct {
	interval time.Duration
	lastCall time.Time
	mu       sync.Mutex
}

// NewRateLimiter 创建一个新的限流器
func NewRateLimiter(ratePerSecond float64) *RateLimiter {
	return &RateLimiter{
		interval: time.Duration(float64(time.Second) / ratePerSecond),
	}
}

// Wait 等待直到可以进行下一次调用
func (r *RateLimiter) Wait() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if diff := r.interval - now.Sub(r.lastCall); diff > 0 {
		time.Sleep(diff)
	}
	r.lastCall = time.Now()
}
