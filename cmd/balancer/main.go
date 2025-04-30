package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"balancer/internal/config"
	"balancer/internal/domain/balancer"
	"balancer/internal/domain/ratelimit"
	"balancer/internal/repository/postgres"
	"balancer/internal/server"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		slog.Error("ошибка при загрузке конфигурации", "error", err)
		os.Exit(1)
	}

	setupLogger(cfg)

	loadBalancer, err := setupLoadBalancer(cfg)
	if err != nil {
		slog.Error("ошибка при создании балансировщика", "error", err)
		os.Exit(1)
	}

	var (
		rateLimiter ratelimit.RateLimiterService
		repository  ratelimit.Repository
	)

	var serverOptions []server.Option

	if cfg.RateLimiting.Enabled {
		var err error

		rateLimiter, repository, err = setupRateLimiter(cfg)
		if err != nil {
			slog.Error("ошибка при создании сервиса ограничения скорости", "error", err)
			os.Exit(1)
		}

		if repository != nil {
			serverOptions = append(serverOptions, server.WithRepository(repository))
		}

		serverOptions = append(serverOptions, server.WithRateLimiter(rateLimiter, cfg.RateLimiting.Enabled))

		if rlTicker, ok := rateLimiter.(interface {
			StartRefillTicker(interval time.Duration)
		}); ok {
			rlTicker.StartRefillTicker(1 * time.Second)
		}
	}

	srv := server.NewServer(cfg, loadBalancer, serverOptions...)

	go func() {
		if err := srv.Start(); err != nil {
			slog.Error("ошибка при старте сервера", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("получен сигнал завершения, начинаем остановку...")

	if rateLimiter != nil {
		if rlTickerStopper, ok := rateLimiter.(interface {
			StopRefillTicker()
		}); ok {
			slog.Info("остановка тикера rate limiter...")
			rlTickerStopper.StopRefillTicker()
		}
	}

	if err := srv.GracefulShutdown(); err != nil {
		slog.Error("ошибка при корректной остановке сервера", "error", err)
		os.Exit(1)
	}

	slog.Info("сервер успешно остановлен")
}

func setupLogger(cfg *config.Config) {
	var handler slog.Handler
	if cfg.Logging.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: getLogLevel(cfg.Logging.Level),
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: getLogLevel(cfg.Logging.Level),
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
}

func getLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func setupLoadBalancer(cfg *config.Config) (balancer.Balancer, error) {
	healthChecker := balancer.NewHTTPHealthChecker()

	balancerFactory := balancer.NewBalancerFactory(healthChecker)

	lb, err := balancerFactory.CreateBalancer(cfg.LoadBalancing.Algorithm)
	if err != nil {
		return nil, err
	}

	for _, backendCfg := range cfg.Backends {
		backend, err := balancer.NewBackend(backendCfg.URL, backendCfg.Weight)
		if err != nil {
			return nil, err
		}

		lb.AddBackend(backend)
	}

	return lb, nil
}

func setupRateLimiter(cfg *config.Config) (ratelimit.RateLimiterService, ratelimit.Repository, error) {
	storageType := cfg.RateLimiting.StorageType

	if strings.EqualFold(storageType, "postgres") {
		pgPool, err := setupPostgresConnection(cfg)
		if err != nil {
			return nil, nil, err
		}

		repository := postgres.NewClientRepository(pgPool)

		persistentLimiter, err := ratelimit.NewPersistentRateLimiter(
			cfg.RateLimiting.DefaultCapacity,
			cfg.RateLimiting.DefaultRatePerSecond,
			repository,
		)
		if err != nil {
			pgPool.Close()
			return nil, nil, err
		}

		return persistentLimiter, repository, nil
	}

	inMemoryLimiter := ratelimit.NewInMemoryRateLimiter(
		cfg.RateLimiting.DefaultCapacity,
		cfg.RateLimiting.DefaultRatePerSecond,
	)

	return inMemoryLimiter, nil, nil
}

func setupPostgresConnection(cfg *config.Config) (*pgxpool.Pool, error) {
	pgConnString := buildPgConnectionString(&cfg.Database)

	pgConfig, err := pgxpool.ParseConfig(pgConnString)
	if err != nil {
		return nil, fmt.Errorf("ошибка при создании конфигурации пула подключений: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return nil, fmt.Errorf("ошибка при подключении к базе данных: %w", err)
	}

	if err := pgPool.Ping(ctx); err != nil {
		pgPool.Close()
		return nil, fmt.Errorf("ошибка при пинге базы данных: %w", err)
	}

	return pgPool, nil
}

func buildPgConnectionString(dbCfg *config.DatabaseConfig) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		dbCfg.User,
		dbCfg.Password,
		dbCfg.Host,
		dbCfg.Port,
		dbCfg.DBName,
		dbCfg.SSLMode,
	)
}
