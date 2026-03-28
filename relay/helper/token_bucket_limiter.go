package helper

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// TokenBucketLimiter 令牌桶限流器
// 支持平滑限流和突发容量
type TokenBucketLimiter struct {
	tokens      chan struct{} // 令牌桶
	burstSize   int           // 突发容量（桶大小）
	ratePerSec  float64       // 每秒产生令牌数
	
	// 控制令牌生产
	stopCh     chan struct{}
	wg         sync.WaitGroup
	
	// 统计信息
	mu          sync.RWMutex
	pendingReqs int64 // 等待中的请求数
}

// NewTokenBucketLimiter 创建令牌桶限流器
// ratePerSec: 每秒产生令牌数（AirForce是1）
// burstSize: 突发容量（桶大小，建议2-3，允许瞬间处理多个请求）
func NewTokenBucketLimiter(ratePerSec float64, burstSize int) *TokenBucketLimiter {
	if ratePerSec <= 0 {
		ratePerSec = 1
	}
	if burstSize <= 0 {
		burstSize = 2
	}

	limiter := &TokenBucketLimiter{
		tokens:     make(chan struct{}, burstSize),
		burstSize:  burstSize,
		ratePerSec: ratePerSec,
		stopCh:     make(chan struct{}),
	}

	// 初始化填满令牌桶
	for i := 0; i < burstSize; i++ {
		limiter.tokens <- struct{}{}
	}

	// 启动令牌生产协程
	limiter.wg.Add(1)
	go limiter.produceTokens()

	return limiter
}

// produceTokens 持续生产令牌
func (l *TokenBucketLimiter) produceTokens() {
	defer l.wg.Done()
	
	interval := time.Duration(float64(time.Second) / l.ratePerSec)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			select {
			case l.tokens <- struct{}{}:
				// 成功放入令牌
			default:
				// 桶已满，丢弃令牌
			}
		case <-l.stopCh:
			return
		}
	}
}

// Acquire 获取令牌（阻塞式，带超时）
// 返回true表示获取成功，false表示超时
func (l *TokenBucketLimiter) Acquire(ctx context.Context, timeout time.Duration) bool {
	l.mu.Lock()
	l.pendingReqs++
	l.mu.Unlock()
	
	defer func() {
		l.mu.Lock()
		l.pendingReqs--
		l.mu.Unlock()
	}()

	if timeout <= 0 {
		// 无限等待
		select {
		case <-l.tokens:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// 带超时等待
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-l.tokens:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// TryAcquire 尝试获取令牌（非阻塞）
// 返回true表示获取成功
func (l *TokenBucketLimiter) TryAcquire() bool {
	select {
	case <-l.tokens:
		return true
	default:
		return false
	}
}

// Stop 停止限流器
func (l *TokenBucketLimiter) Stop() {
	close(l.stopCh)
	l.wg.Wait()
}

// GetPendingCount 获取等待中的请求数
func (l *TokenBucketLimiter) GetPendingCount() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.pendingReqs
}

// ==================== AirForce 专用限流管理器 ====================

// AirForceRateLimiterV2 新版 AirForce 限流管理器
type AirForceRateLimiterV2 struct {
	// 全局IP限流：1请求/秒，突发容量2
	globalLimiter *TokenBucketLimiter
	
	// Key级别限流：1请求/60秒
	// 每个key有自己的限流器
	keyLimiters   map[int]*TokenBucketLimiter
	keyMutex      sync.RWMutex
	
	// Key冷却时间（被限流后的冷却）
	keyCooldown   map[int]int64 // keyIndex -> 冷却结束时间
	cooldownMutex sync.RWMutex
	cooldownSecs  int64
}

var (
	airForceLimiterV2     *AirForceRateLimiterV2
	airForceLimiterV2Once sync.Once
)

// GetAirForceRateLimiterV2 获取限流器实例
func GetAirForceRateLimiterV2() *AirForceRateLimiterV2 {
	airForceLimiterV2Once.Do(func() {
		airForceLimiterV2 = &AirForceRateLimiterV2{
			// 全局：1请求/秒，突发容量2（允许瞬间2个请求）
			globalLimiter: NewTokenBucketLimiter(1, 2),
			keyLimiters:   make(map[int]*TokenBucketLimiter),
			keyCooldown:   make(map[int]int64),
			cooldownSecs:  60, // 被限流后冷却60秒
		}
	})
	return airForceLimiterV2
}

// AcquireGlobal 获取全局令牌（IP级别限流）
// 返回true表示可以发送请求
func (r *AirForceRateLimiterV2) AcquireGlobal(ctx context.Context) bool {
	// 等待全局令牌，最多等3秒
	return r.globalLimiter.Acquire(ctx, 3*time.Second)
}

// AcquireKey 获取Key级别令牌
// 如果key被限流，返回false
func (r *AirForceRateLimiterV2) AcquireKey(keyIndex int) bool {
	// 先检查冷却状态
	r.cooldownMutex.RLock()
	if cooldownEnd, exists := r.keyCooldown[keyIndex]; exists {
		if time.Now().Unix() < cooldownEnd {
			r.cooldownMutex.RUnlock()
			return false
		}
	}
	r.cooldownMutex.RUnlock()

	// 获取或创建key的限流器（1请求/60秒）
	r.keyMutex.Lock()
	limiter, exists := r.keyLimiters[keyIndex]
	if !exists {
		// 每分钟1个请求，突发容量1（严格限制）
		limiter = NewTokenBucketLimiter(1.0/60.0, 1)
		r.keyLimiters[keyIndex] = limiter
	}
	r.keyMutex.Unlock()

	// 尝试获取令牌（非阻塞）
	return limiter.TryAcquire()
}

// MarkKeyRateLimited 标记key被限流（进入冷却）
func (r *AirForceRateLimiterV2) MarkKeyRateLimited(keyIndex int) {
	r.cooldownMutex.Lock()
	r.keyCooldown[keyIndex] = time.Now().Unix() + r.cooldownSecs
	r.cooldownMutex.Unlock()
	
	common.SysLog(fmt.Sprintf("AirForce key [%d] rate limited, cooldown for %d seconds", 
		keyIndex, r.cooldownSecs))
}

// IsKeyAvailable 检查key是否可用（不在冷却中）
func (r *AirForceRateLimiterV2) IsKeyAvailable(keyIndex int) bool {
	r.cooldownMutex.RLock()
	defer r.cooldownMutex.RUnlock()
	
	if cooldownEnd, exists := r.keyCooldown[keyIndex]; exists {
		return time.Now().Unix() >= cooldownEnd
	}
	return true
}

// GetNextAvailableKey 获取下一个可用的key
// keyCount: key的总数
// startIndex: 开始搜索的索引
// 优先选择不在冷却中且有令牌的key
func (r *AirForceRateLimiterV2) GetNextAvailableKey(keyCount int, startIndex int) int {
	if keyCount <= 0 {
		return -1
	}

	// 先找不在冷却中且有令牌的key
	for i := 0; i < keyCount; i++ {
		idx := (startIndex + i) % keyCount
		if r.IsKeyAvailable(idx) && r.AcquireKey(idx) {
			return idx
		}
	}

	// 如果没有立即可用的，找不在冷却中的（等待全局令牌）
	for i := 0; i < keyCount; i++ {
		idx := (startIndex + i) % keyCount
		if r.IsKeyAvailable(idx) {
			return idx
		}
	}

	return -1
}

// ClearKeyCooldown 清除key的冷却状态
func (r *AirForceRateLimiterV2) ClearKeyCooldown(keyIndex int) {
	r.cooldownMutex.Lock()
	delete(r.keyCooldown, keyIndex)
	r.cooldownMutex.Unlock()
}

// WaitForRequest 等待发送请求的许可
// 这是主要入口：先获取全局令牌，然后获取key令牌
// keyCount: key的总数
// preferredKey: 优先使用的key索引
// 返回keyIndex和是否成功
func (r *AirForceRateLimiterV2) WaitForRequest(ctx context.Context, keyCount int, preferredKey int) (int, bool) {
	if keyCount <= 0 {
		return -1, false
	}
	
	// 1. 等待全局IP限流（阻塞，但平滑）
	if !r.AcquireGlobal(ctx) {
		return -1, false
	}

	// 2. 获取可用的key
	keyIndex := r.GetNextAvailableKey(keyCount, preferredKey)
	if keyIndex < 0 {
		// 所有key都在冷却中，立即返回错误（RPM是60秒，等待没有意义）
		common.SysLog("All AirForce keys are rate limited, rejecting request")
		return -1, false
	}

	return keyIndex, true
}

// Release 释放资源（停止所有限流器）
func (r *AirForceRateLimiterV2) Release() {
	r.globalLimiter.Stop()
	
	r.keyMutex.Lock()
	for _, limiter := range r.keyLimiters {
		limiter.Stop()
	}
	r.keyLimiters = make(map[int]*TokenBucketLimiter)
	r.keyMutex.Unlock()
}
