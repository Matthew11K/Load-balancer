package balancer

import (
	"net/http"
	"net/url"
	"sync"
	"time"
)

type AlgorithmType string

const (
	RoundRobin       AlgorithmType = "round-robin"
	LeastConnections AlgorithmType = "least-connections"
	Random           AlgorithmType = "random"
)

type Backend struct {
	URL            *url.URL
	Weight         int
	ActiveRequests int
	IsHealthy      bool
	mu             sync.RWMutex
}

func NewBackend(urlStr string, weight int) (*Backend, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	return &Backend{
		URL:            u,
		Weight:         weight,
		ActiveRequests: 0,
		IsHealthy:      true,
	}, nil
}

func (b *Backend) IncreaseActiveRequests() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ActiveRequests++
}

func (b *Backend) DecreaseActiveRequests() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ActiveRequests > 0 {
		b.ActiveRequests--
	}
}

func (b *Backend) GetActiveRequests() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.ActiveRequests
}

func (b *Backend) SetHealth(isHealthy bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.IsHealthy = isHealthy
}

func (b *Backend) GetHealth() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.IsHealthy
}

type Balancer interface {
	NextBackend() (*Backend, error)
	AddBackend(backend *Backend)
	RemoveBackend(backend *Backend)
	HealthCheck(timeout time.Duration) error
}

type HealthChecker interface {
	CheckHealth(backend *Backend, timeout time.Duration) (bool, error)
}

type HTTPHealthChecker struct {
	client *http.Client
}

func NewHTTPHealthChecker() *HTTPHealthChecker {
	return &HTTPHealthChecker{
		client: &http.Client{},
	}
}

func (c *HTTPHealthChecker) CheckHealth(backend *Backend, timeout time.Duration) (bool, error) {
	client := &http.Client{
		Timeout: timeout,
	}

	resp, err := client.Get(backend.URL.String())
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode < 500, nil
}
