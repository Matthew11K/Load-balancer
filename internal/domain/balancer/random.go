package balancer

import (
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"balancer/internal/domain/errors"
)

type RandomBalancer struct {
	backends      []*Backend
	mutex         sync.RWMutex
	healthChecker HealthChecker
	random        *rand.Rand
}

func NewRandomBalancer(healthChecker HealthChecker) *RandomBalancer {
	return &RandomBalancer{
		backends:      make([]*Backend, 0),
		healthChecker: healthChecker,
		// gosec G404: Для балансировки нагрузки не требуется криптографически стойкий генератор случайных чисел
		random: rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // Для балансировки нагрузки не требуется криптостойкий RNG
	}
}

func (b *RandomBalancer) NextBackend() (*Backend, error) {
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

	randomIndex := b.random.Intn(len(healthyBackends))

	return healthyBackends[randomIndex], nil
}

func (b *RandomBalancer) AddBackend(backend *Backend) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.backends = append(b.backends, backend)
	slog.Info("добавлен новый бэкенд", "url", backend.URL.String())
}

func (b *RandomBalancer) RemoveBackend(backend *Backend) {
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

func (b *RandomBalancer) HealthCheck(timeout time.Duration) error {
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
