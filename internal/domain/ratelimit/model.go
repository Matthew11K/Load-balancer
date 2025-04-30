package ratelimit

import (
	"sync/atomic"
	"time"
)

type TokenBucket struct {
	capacity       int64
	tokens         atomic.Int64
	ratePerSec     float64
	lastRefillTime int64
}

func NewTokenBucket(capacity int, ratePerSec float64) *TokenBucket {
	tb := &TokenBucket{
		capacity:       int64(capacity),
		ratePerSec:     ratePerSec,
		lastRefillTime: time.Now().UnixNano(),
	}
	tb.tokens.Store(int64(capacity))

	return tb
}

func (tb *TokenBucket) refill() {
	now := time.Now()
	nowNano := now.UnixNano()
	lastRefillNano := atomic.LoadInt64(&tb.lastRefillTime)
	lastRefill := time.Unix(0, lastRefillNano)

	elapsed := now.Sub(lastRefill).Seconds()

	if elapsed <= 0 {
		return
	}

	newTokens := int64(elapsed * tb.ratePerSec)

	if newTokens <= 0 {
		return
	}

	if !atomic.CompareAndSwapInt64(&tb.lastRefillTime, lastRefillNano, nowNano) {
		return
	}

	currentTokens := tb.tokens.Load()

	newTokensTotal := currentTokens + newTokens
	if newTokensTotal > tb.capacity {
		newTokensTotal = tb.capacity
	}

	tb.tokens.Store(newTokensTotal)
}

func (tb *TokenBucket) AllowRequest() bool {
	tb.refill()

	for {
		current := tb.tokens.Load()
		if current < 1 {
			return false
		}

		if tb.tokens.CompareAndSwap(current, current-1) {
			return true
		}
	}
}

type Client struct {
	ID         string
	Bucket     *TokenBucket
	LastAccess int64
}

func NewClient(id string, capacity int, ratePerSec float64) *Client {
	return &Client{
		ID:         id,
		Bucket:     NewTokenBucket(capacity, ratePerSec),
		LastAccess: time.Now().UnixNano(),
	}
}

func (c *Client) UpdateLastAccess() {
	atomic.StoreInt64(&c.LastAccess, time.Now().UnixNano())
}

func (c *Client) GetLastAccessTime() time.Time {
	return time.Unix(0, atomic.LoadInt64(&c.LastAccess))
}

func (c *Client) AllowRequest() bool {
	allowed := c.Bucket.AllowRequest()
	if allowed {
		c.UpdateLastAccess()
	}

	return allowed
}
