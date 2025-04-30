package balancer

import (
	"log/slog"
	"sync"
	"time"

	"balancer/internal/domain/errors"
)

type LeastConnectionsBalancer struct {
	backends      []*Backend
	mutex         sync.RWMutex
	healthChecker HealthChecker
}

func NewLeastConnectionsBalancer(healthChecker HealthChecker) *LeastConnectionsBalancer {
	return &LeastConnectionsBalancer{
		backends:      make([]*Backend, 0),
		healthChecker: healthChecker,
	}
}

func (b *LeastConnectionsBalancer) NextBackend() (*Backend, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	healthyBackends := make([]*Backend, 0)

	for _, backend := range b.backends {
		if backend.GetHealth() {
			healthyBackends = append(healthyBackends, backend)
		}
	}

	if len(healthyBackends) == 0 {
		return nil, &errors.ErrNoAvailableBackends{}
	}

	var selectedBackend *Backend

	minConnections := -1

	for _, backend := range healthyBackends {
		activeRequests := backend.GetActiveRequests()

		if minConnections == -1 || activeRequests < minConnections {
			minConnections = activeRequests
			selectedBackend = backend
		}
	}

	return selectedBackend, nil
}

func (b *LeastConnectionsBalancer) AddBackend(backend *Backend) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.backends = append(b.backends, backend)
	slog.Info("добавлен новый бэкенд", "url", backend.URL.String())
}

func (b *LeastConnectionsBalancer) RemoveBackend(backend *Backend) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	for i, existingBackend := range b.backends {
		if existingBackend.URL.String() == backend.URL.String() {
			b.backends = append(b.backends[:i], b.backends[i+1:]...)

			slog.Info("бэкенд удален", "url", backend.URL.String())

			return
		}
	}
}

func (b *LeastConnectionsBalancer) HealthCheck(timeout time.Duration) error {
	b.mutex.RLock()
	backends := make([]*Backend, len(b.backends))
	copy(backends, b.backends)
	b.mutex.RUnlock()

	for _, backend := range backends {
		isHealthy, err := b.healthChecker.CheckHealth(backend, timeout)
		prevStatus := backend.GetHealth()

		if err != nil {
			slog.Error("ошибка проверки здоровья бэкенда", "url", backend.URL.String(), "error", err)
			backend.SetHealth(false)
		} else {
			backend.SetHealth(isHealthy)

			if !prevStatus && isHealthy {
				slog.Info("бэкенд восстановлен", "url", backend.URL.String())
			} else if prevStatus && !isHealthy {
				slog.Warn("бэкенд недоступен", "url", backend.URL.String())
			}
		}
	}

	return nil
}
