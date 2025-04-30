package balancer

import (
	"log/slog"
	"sync"
	"time"

	"balancer/internal/domain/errors"
)

type RoundRobinBalancer struct {
	backends      []*Backend
	currentIndex  int
	mutex         sync.RWMutex
	healthChecker HealthChecker
}

func NewRoundRobinBalancer(healthChecker HealthChecker) *RoundRobinBalancer {
	return &RoundRobinBalancer{
		backends:      make([]*Backend, 0),
		currentIndex:  0,
		healthChecker: healthChecker,
	}
}

func (b *RoundRobinBalancer) NextBackend() (*Backend, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	healthyBackends := make([]*Backend, 0)

	for _, backend := range b.backends {
		if backend.GetHealth() {
			healthyBackends = append(healthyBackends, backend)
		}
	}

	if len(healthyBackends) == 0 {
		return nil, &errors.ErrNoAvailableBackends{}
	}

	if b.currentIndex >= len(healthyBackends) {
		b.currentIndex = 0
	}

	backend := healthyBackends[b.currentIndex]
	b.currentIndex = (b.currentIndex + 1) % len(healthyBackends)

	return backend, nil
}

func (b *RoundRobinBalancer) AddBackend(backend *Backend) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.backends = append(b.backends, backend)
	slog.Info("добавлен новый бэкенд", "url", backend.URL.String())
}

func (b *RoundRobinBalancer) RemoveBackend(backend *Backend) {
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

func (b *RoundRobinBalancer) HealthCheck(timeout time.Duration) error {
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
