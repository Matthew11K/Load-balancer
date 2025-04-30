package ratelimit

import (
	"balancer/internal/domain/errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type PersistentRateLimiter struct {
	clients           map[string]*Client
	mu                sync.RWMutex
	defaultCapacity   int
	defaultRatePerSec float64
	repository        Repository
	ticker            *time.Ticker
	stopTickerCh      chan struct{}
	tickerWg          sync.WaitGroup
}

func NewPersistentRateLimiter(defaultCapacity int, defaultRatePerSec float64, repository Repository) (*PersistentRateLimiter, error) {
	limiter := &PersistentRateLimiter{
		clients:           make(map[string]*Client),
		defaultCapacity:   defaultCapacity,
		defaultRatePerSec: defaultRatePerSec,
		repository:        repository,
		stopTickerCh:      make(chan struct{}),
	}

	clients, err := repository.ListClients()
	if err != nil {
		slog.Error("ошибка при загрузке клиентов из репозитория", "error", err)
		return nil, err
	}

	for _, clientData := range clients {
		client := NewClient(clientData.ID, clientData.Capacity, clientData.RatePerSec)
		limiter.clients[clientData.ID] = client
	}

	slog.Info("загружены клиенты из репозитория", "count", len(clients))

	return limiter, nil
}

func (r *PersistentRateLimiter) AllowRequest(clientID string) (bool, error) {
	r.mu.RLock()
	client, exists := r.clients[clientID]
	r.mu.RUnlock()

	if !exists {
		clientData, err := r.repository.GetClient(clientID)
		if err != nil {
			client = NewClient(clientID, r.defaultCapacity, r.defaultRatePerSec)

			saveErr := r.repository.SaveClient(&ClientData{
				ID:         clientID,
				Capacity:   r.defaultCapacity,
				RatePerSec: r.defaultRatePerSec,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			})
			if saveErr != nil {
				slog.Error("не удалось сохранить нового клиента в репозиторий", "error", saveErr)
			}
		} else {
			client = NewClient(clientID, clientData.Capacity, clientData.RatePerSec)
		}

		r.mu.Lock()
		r.clients[clientID] = client
		r.mu.Unlock()

		slog.Debug("клиент добавлен в кэш", "clientID", clientID)
	}

	allowed := client.AllowRequest()

	if !allowed {
		slog.Debug("запрос отклонен из-за превышения лимита", "clientID", clientID)
		return false, &errors.ErrRateLimitExceeded{ClientID: clientID}
	}

	return true, nil
}

func (r *PersistentRateLimiter) AddClient(clientID string, capacity int, ratePerSec float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	client := NewClient(clientID, capacity, ratePerSec)
	r.clients[clientID] = client

	err := r.repository.SaveClient(&ClientData{
		ID:         clientID,
		Capacity:   capacity,
		RatePerSec: ratePerSec,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})

	if err != nil {
		slog.Error("ошибка при сохранении клиента в репозиторий", "error", err)
		return err
	}

	slog.Info("добавлен/обновлен клиент", "clientID", clientID, "capacity", capacity, "ratePerSec", ratePerSec)

	return nil
}

func (r *PersistentRateLimiter) RemoveClient(clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.clients[clientID]; !exists {
		return &errors.ErrClientNotFound{ClientID: clientID}
	}

	delete(r.clients, clientID)

	err := r.repository.DeleteClient(clientID)
	if err != nil {
		slog.Error("ошибка при удалении клиента из репозитория", "error", err)
		return err
	}

	slog.Info("клиент удален", "clientID", clientID)

	return nil
}

func (r *PersistentRateLimiter) GetClient(clientID string) (*Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	client, exists := r.clients[clientID]
	if !exists {
		clientData, err := r.repository.GetClient(clientID)
		if err != nil {
			return nil, &errors.ErrClientNotFound{ClientID: clientID}
		}

		client = NewClient(clientData.ID, clientData.Capacity, clientData.RatePerSec)
		r.clients[clientID] = client
	}

	return client, nil
}

func (r *PersistentRateLimiter) CleanupExpired(maxAge time.Duration) {
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

		err := r.repository.DeleteClient(id)
		if err != nil {
			slog.Error("ошибка при удалении неактивного клиента из репозитория", "error", err)
		}

		slog.Debug("удален неактивный клиент", "clientID", id, "maxAge", maxAge)
	}

	if len(expiredClients) > 0 {
		slog.Info("выполнена очистка неактивных клиентов", "удалено", len(expiredClients))
	}
}

func (r *PersistentRateLimiter) StartRefillTicker(interval time.Duration) {
	if r.ticker != nil {
		slog.Warn("тикер пополнения токенов (persistent) уже запущен")
		return
	}

	r.ticker = time.NewTicker(interval)
	r.tickerWg.Add(1)

	go func() {
		defer r.tickerWg.Done()
		slog.Info("запуск тикера пополнения токенов (persistent)", "interval", interval)

		for {
			select {
			case <-r.ticker.C:
				r.refillAll()
			case <-r.stopTickerCh:
				slog.Info("остановка тикера пополнения токенов (persistent)")
				r.ticker.Stop()

				return
			}
		}
	}()
}

func (r *PersistentRateLimiter) StopRefillTicker() {
	if r.ticker == nil {
		slog.Warn("тикер пополнения токенов (persistent) не был запущен")
		return
	}

	close(r.stopTickerCh)
	r.tickerWg.Wait()
	r.ticker = nil

	slog.Info("тикер пополнения токенов (persistent) остановлен")
}

func (r *PersistentRateLimiter) refillAll() {
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
