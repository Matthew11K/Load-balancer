package test //nolint:testpackage // Этот пакет является интеграционным тестом, поэтому использует свой пакет

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"balancer/internal/config"
	"balancer/internal/domain/balancer"
	"balancer/internal/domain/ratelimit"
	"balancer/internal/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	balancerPort = 8888
	testDuration = 5 * time.Second
)

// TestLoadBalancerIntegration проверяет работу балансировщика в интеграционном тесте
//
//nolint:gocognit // Интеграционные тесты могут иметь более высокую когнитивную сложность
func TestLoadBalancerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:         balancerPort,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		LoadBalancing: config.LoadBalancingConfig{
			Algorithm:           "round-robin",
			HealthCheckInterval: 2 * time.Second,
			HealthCheckTimeout:  1 * time.Second,
		},
		RateLimiting: config.RateLimitingConfig{
			DefaultCapacity:      5000,
			DefaultRatePerSecond: 1000,
			Enabled:              true,
		},
	}

	backends := []struct {
		URL  string
		Port int
	}{
		{"http://localhost:9001", 9001},
		{"http://localhost:9002", 9002},
		{"http://localhost:9003", 9003},
	}

	for i, b := range backends {
		backendID := i + 1
		go startTestBackend(t, b.Port, backendID)

		cfg.Backends = append(cfg.Backends, config.BackendConfig{
			URL:    b.URL,
			Weight: 1,
		})
	}

	time.Sleep(1 * time.Second)

	healthChecker := balancer.NewHTTPHealthChecker()
	balancerFactory := balancer.NewBalancerFactory(healthChecker)
	lb, err := balancerFactory.CreateBalancer(cfg.LoadBalancing.Algorithm)
	require.NoError(t, err)

	for _, backendCfg := range cfg.Backends {
		backend, err := balancer.NewBackend(backendCfg.URL, backendCfg.Weight)
		require.NoError(t, err)
		lb.AddBackend(backend)
	}

	rateLimiter := ratelimit.NewInMemoryRateLimiter(
		cfg.RateLimiting.DefaultCapacity,
		cfg.RateLimiting.DefaultRatePerSecond,
	)

	srv := server.NewServer(
		cfg,
		lb,
		server.WithRateLimiter(rateLimiter, cfg.RateLimiting.Enabled),
	)

	err = srv.Start()
	require.NoError(t, err)

	defer func() {
		err := srv.GracefulShutdown()
		if err != nil {
			t.Logf("Ошибка при остановке сервера: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	t.Run("RoundRobinDistribution", func(t *testing.T) {
		responses := make(map[string]int)

		var mu sync.Mutex

		for i := 0; i < 30; i++ {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", balancerPort))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			resp.Body.Close()

			mu.Lock()
			responses[string(body)]++
			mu.Unlock()
		}

		mu.Lock()
		defer mu.Unlock()

		for i := 1; i <= len(backends); i++ {
			expectedResponse := fmt.Sprintf("Backend %d\n", i)
			count, exists := responses[expectedResponse]
			assert.True(t, exists, "Backend %d должен получить запросы", i)
			assert.Greater(t, count, 0, "Backend %d должен получить не менее 1 запроса", i)
		}
	})

	t.Run("RateLimiting", func(t *testing.T) {
		lowLimitClientID := "test-low-limit"
		err := rateLimiter.AddClient(lowLimitClientID, 3, 1)
		require.NoError(t, err)

		client := &http.Client{}

		for i := 0; i < 5; i++ {
			req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/", balancerPort), http.NoBody)
			require.NoError(t, err)

			req.Header.Set("X-Rate-Limit-Client", lowLimitClientID)

			resp, err := client.Do(req)
			require.NoError(t, err)

			resp.Body.Close()

			if i < 3 {
				assert.Equal(t, http.StatusOK, resp.StatusCode, "Запрос %d должен быть успешным", i+1)
			} else {
				assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "Запрос %d должен быть отклонен", i+1)
			}
		}
	})

	t.Run("BenchmarkConcurrentRequests", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping benchmark in short mode")
		}

		var wg sync.WaitGroup

		client := &http.Client{
			Timeout: 5 * time.Second,
		}

		var (
			successCount int32
			errorCount   int32
		)

		numClients := 100
		requestsPerClient := 50

		startTime := time.Now()

		for i := 0; i < numClients; i++ {
			wg.Add(1)

			go func(_ int) {
				defer wg.Done()

				for j := 0; j < requestsPerClient; j++ {
					resp, err := client.Get(fmt.Sprintf("http://localhost:%d/", balancerPort))

					if err != nil {
						atomic.AddInt32(&errorCount, 1)
					} else {
						resp.Body.Close()

						if resp.StatusCode == http.StatusOK {
							atomic.AddInt32(&successCount, 1)
						} else {
							atomic.AddInt32(&errorCount, 1)
						}
					}

					time.Sleep(10 * time.Millisecond)
				}
			}(i)
		}

		wg.Wait()

		elapsed := time.Since(startTime)

		totalSuccessCount := atomic.LoadInt32(&successCount)
		totalErrorCount := atomic.LoadInt32(&errorCount)
		totalRequests := totalSuccessCount + totalErrorCount

		requestsPerSecond := float64(totalRequests) / elapsed.Seconds()

		t.Logf("Производительность: %.2f запросов в секунду", requestsPerSecond)
		t.Logf("Успешных запросов: %d (%.2f%%)", totalSuccessCount, float64(totalSuccessCount)/float64(totalRequests)*100)
		t.Logf("Ошибочных запросов: %d (%.2f%%)", totalErrorCount, float64(totalErrorCount)/float64(totalRequests)*100)

		assert.Greater(t, requestsPerSecond, 100.0, "Производительность должна быть не менее 100 запросов в секунду")
		assert.Greater(t, float64(totalSuccessCount)/float64(totalRequests), 0.95, "Успешность запросов должна быть не менее 95%")
	})
}

func startTestBackend(t *testing.T, port, id int) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "Backend %d\n", id)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("OK"))
		if err != nil {
			t.Logf("Ошибка при отправке ответа health check: %v", err)
		}
	})

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Logf("Ошибка запуска тестового бэкенда %d: %v", id, err)
		}
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			t.Logf("Ошибка при остановке тестового бэкенда %d: %v", id, err)
		}
	})
}
