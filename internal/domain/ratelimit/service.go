package ratelimit

import (
	"balancer/internal/domain/errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type RateLimiterService interface {
	AllowRequest(clientID string) (bool, error)
	AddClient(clientID string, capacity int, ratePerSec float64) error
	RemoveClient(clientID string) error
	GetClient(clientID string) (*Client, error)
	CleanupExpired(maxAge time.Duration)
	StartRefillTicker(interval time.Duration)
	StopRefillTicker()
}

type InMemoryRateLimiter struct {
	clients           map[string]*Client
	mu                sync.RWMutex
	defaultCapacity   int
	defaultRatePerSec float64
	ticker            *time.Ticker
	stopTickerCh      chan struct{}
	tickerWg          sync.WaitGroup
}

func NewInMemoryRateLimiter(defaultCapacity int, defaultRatePerSec float64) *InMemoryRateLimiter {
	return &InMemoryRateLimiter{
		clients:           make(map[string]*Client),
		defaultCapacity:   defaultCapacity,
		defaultRatePerSec: defaultRatePerSec,
		stopTickerCh:      make(chan struct{}),
	}
}

func (r *InMemoryRateLimiter) StartRefillTicker(interval time.Duration) {
	if r.ticker != nil {
		slog.Warn("тикер пополнения токенов уже запущен")
		return
	}

	r.ticker = time.NewTicker(interval)
	r.tickerWg.Add(1)

	go func() {
		defer r.tickerWg.Done()
		slog.Info("запуск тикера пополнения токенов", "interval", interval)

		for {
			select {
			case <-r.ticker.C:
				r.refillAll()
			case <-r.stopTickerCh:
				slog.Info("остановка тикера пополнения токенов")
				r.ticker.Stop()

				return
			}
		}
	}()
}

func (r *InMemoryRateLimiter) StopRefillTicker() {
	if r.ticker == nil {
		slog.Warn("тикер пополнения токенов не был запущен")
		return
	}

	close(r.stopTickerCh)
	r.tickerWg.Wait()
	r.ticker = nil

	slog.Info("тикер пополнения токенов остановлен")
}

func (r *InMemoryRateLimiter) refillAll() {
	r.mu.RLock()

	clientsToRefill := make([]*Client, 0, len(r.clients))
	for _, client := range r.clients {
		clientsToRefill = append(clientsToRefill, client)
	}
	r.mu.RUnlock()

	if len(clientsToRefill) == 0 {
		return
	}

	for _, client := range clientsToRefill {
		client.Bucket.RefillNow()
	}
}

func (r *InMemoryRateLimiter) AllowRequest(clientID string) (bool, error) {
	r.mu.RLock()
	client, exists := r.clients[clientID]
	r.mu.RUnlock()

	if !exists {
		client = NewClient(clientID, r.defaultCapacity, r.defaultRatePerSec)

		r.mu.Lock()
		r.clients[clientID] = client
		r.mu.Unlock()

		slog.Debug("создан новый клиент с дефолтными настройками", "clientID", clientID)
	}

	allowed := client.AllowRequest()

	if !allowed {
		slog.Debug("запрос отклонен из-за превышения лимита", "clientID", clientID)
		return false, &errors.ErrRateLimitExceeded{ClientID: clientID}
	}

	return true, nil
}

func (r *InMemoryRateLimiter) AddClient(clientID string, capacity int, ratePerSec float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	client := NewClient(clientID, capacity, ratePerSec)
	r.clients[clientID] = client

	slog.Info("добавлен/обновлен клиент", "clientID", clientID, "capacity", capacity, "ratePerSec", ratePerSec)

	return nil
}

func (r *InMemoryRateLimiter) RemoveClient(clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.clients[clientID]; !exists {
		return &errors.ErrClientNotFound{ClientID: clientID}
	}

	delete(r.clients, clientID)
	slog.Info("клиент удален", "clientID", clientID)

	return nil
}

func (r *InMemoryRateLimiter) GetClient(clientID string) (*Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	client, exists := r.clients[clientID]
	if !exists {
		return nil, &errors.ErrClientNotFound{ClientID: clientID}
	}

	return client, nil
}

func (r *InMemoryRateLimiter) CleanupExpired(maxAge time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	var expiredClients []string

	for id, client := range r.clients {
		lastAccessTime := time.Unix(0, atomic.LoadInt64(&client.LastAccess))
		if now.Sub(lastAccessTime) > maxAge {
			expiredClients = append(expiredClients, id)
		}
	}

	for _, id := range expiredClients {
		delete(r.clients, id)
		slog.Debug("удален неактивный клиент", "clientID", id, "maxAge", maxAge)
	}

	if len(expiredClients) > 0 {
		slog.Info("выполнена очистка неактивных клиентов", "удалено", len(expiredClients))
	}
}
