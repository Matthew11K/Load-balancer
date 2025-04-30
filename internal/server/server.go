package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"balancer/internal/config"
	"balancer/internal/domain/balancer"
	"balancer/internal/domain/errors"
	"balancer/internal/domain/ratelimit"
)

type Server struct {
	server           *http.Server
	balancer         balancer.Balancer
	rateLimiter      ratelimit.RateLimiterService
	rateLimitEnabled bool
	repository       ratelimit.Repository
	stopCh           chan struct{}
	wg               sync.WaitGroup
}

type Option func(*Server)

func WithRateLimiter(rateLimiter ratelimit.RateLimiterService, enabled bool) Option {
	return func(s *Server) {
		s.rateLimiter = rateLimiter
		s.rateLimitEnabled = enabled
	}
}

func WithRepository(repository ratelimit.Repository) Option {
	return func(s *Server) {
		s.repository = repository
	}
}

func NewServer(cfg *config.Config, balancer balancer.Balancer, options ...Option) *Server {
	srv := &Server{
		balancer: balancer,
		stopCh:   make(chan struct{}),
	}

	for _, option := range options {
		option(srv)
	}

	srv.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           srv.createHandler(),
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return srv
}

func (s *Server) Start() error {
	go func() {
		slog.Info("запуск HTTP-сервера", "addr", s.server.Addr)

		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("ошибка при запуске HTTP-сервера", "error", err)
			return
		}
	}()

	s.startHealthChecks()

	return nil
}

func (s *Server) startHealthChecks() {
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := s.balancer.HealthCheck(2 * time.Second); err != nil {
					slog.Error("ошибка при проверке здоровья бэкендов", "error", err)
				}
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *Server) GracefulShutdown() error {
	slog.Info("начало корректной остановки сервера")

	close(s.stopCh)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		slog.Error("ошибка при остановке HTTP-сервера", "error", err)
		return err
	}

	s.wg.Wait()

	slog.Info("сервер остановлен")

	return nil
}

func (s *Server) createHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.proxyHandler)

	mux.HandleFunc("POST /api/clients", s.addClientHandler)
	mux.HandleFunc("GET /api/clients", s.listClientsHandler)
	mux.HandleFunc("GET /api/clients/{id}", s.getClientHandler)
	mux.HandleFunc("DELETE /api/clients/{id}", s.deleteClientHandler)

	return mux
}

func getClientIP(r *http.Request) string {
	if clientID := r.Header.Get("X-Rate-Limit-Client"); clientID != "" {
		return clientID
	}

	if forwardedIP := r.Header.Get("X-Forwarded-For"); forwardedIP != "" {
		return forwardedIP
	}

	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

func (s *Server) proxyHandler(w http.ResponseWriter, r *http.Request) {
	clientIP := getClientIP(r)

	if !s.rateLimitEnabled || s.rateLimiter == nil {
		s.handleProxyRequest(w, r, clientIP)
		return
	}

	allowed, err := s.rateLimiter.AllowRequest(clientIP)
	if !allowed {
		statusCode := http.StatusTooManyRequests

		slog.Warn("запрос отклонен из-за превышения ограничения скорости",
			"clientIP", clientIP,
			"path", r.URL.Path,
			"method", r.Method,
			"error", err)

		s.writeJSONError(w, statusCode, "Превышено ограничение скорости запросов")

		return
	}

	s.handleProxyRequest(w, r, clientIP)
}

func (s *Server) handleProxyRequest(w http.ResponseWriter, r *http.Request, clientIP string) {
	backend, err := s.balancer.NextBackend()
	if err != nil {
		slog.Error("ошибка при выборе бэкенда", "error", err)
		s.writeJSONError(w, http.StatusServiceUnavailable, "Нет доступных бэкенд-серверов")

		return
	}

	backend.IncreaseActiveRequests()
	defer backend.DecreaseActiveRequests()

	slog.Info("запрос перенаправлен",
		"method", r.Method,
		"path", r.URL.Path,
		"client_ip", clientIP,
		"backend", backend.URL.String())

	proxy := httputil.NewSingleHostReverseProxy(backend.URL)

	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		slog.Error("ошибка проксирования",
			"error", err,
			"backend", backend.URL.String())
		s.writeJSONError(w, http.StatusBadGateway, "Ошибка при проксировании запроса")
	}

	if clientIP != "" {
		r.Header.Set("X-Forwarded-For", clientIP)
	}

	proxy.ServeHTTP(w, r)
}

type ClientRequest struct {
	ClientID   string  `json:"client_id"`
	Capacity   int     `json:"capacity"`
	RatePerSec float64 `json:"rate_per_sec"`
}

type ClientResponse struct {
	ID         string    `json:"id"`
	Capacity   int       `json:"capacity"`
	RatePerSec float64   `json:"rate_per_sec"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) addClientHandler(w http.ResponseWriter, r *http.Request) {
	if s.rateLimiter == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Сервис ограничения скорости не настроен")
		return
	}

	var req ClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	if req.ClientID == "" {
		s.writeJSONError(w, http.StatusBadRequest, "ID клиента не указан")
		return
	}

	if req.Capacity <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "Емкость должна быть положительной")
		return
	}

	if req.RatePerSec <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "Скорость пополнения должна быть положительной")
		return
	}

	if err := s.rateLimiter.AddClient(req.ClientID, req.Capacity, req.RatePerSec); err != nil {
		slog.Error("ошибка при добавлении клиента", "error", err)
		s.writeJSONError(w, http.StatusInternalServerError, "Ошибка при добавлении клиента")

		return
	}

	client, err := s.rateLimiter.GetClient(req.ClientID)
	if err != nil {
		slog.Error("ошибка при получении данных клиента", "error", err)
		s.writeJSONError(w, http.StatusInternalServerError, "Ошибка при получении данных клиента")

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(ClientResponse{
		ID:         client.ID,
		Capacity:   req.Capacity,
		RatePerSec: req.RatePerSec,
		CreatedAt:  client.GetLastAccessTime(),
		UpdatedAt:  client.GetLastAccessTime(),
	}); err != nil {
		slog.Error("ошибка при кодировании ответа", "error", err)
	}
}

func (s *Server) getClientHandler(w http.ResponseWriter, r *http.Request) {
	if s.rateLimiter == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Сервис ограничения скорости не настроен")
		return
	}

	clientID := r.PathValue("id")
	if clientID == "" {
		s.writeJSONError(w, http.StatusBadRequest, "ID клиента не указан")
		return
	}

	client, err := s.rateLimiter.GetClient(clientID)
	if err != nil {
		if _, ok := err.(*errors.ErrClientNotFound); ok {
			s.writeJSONError(w, http.StatusNotFound, "Клиент не найден")
			return
		}

		slog.Error("ошибка при получении данных клиента", "error", err)
		s.writeJSONError(w, http.StatusInternalServerError, "Ошибка при получении данных клиента")

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(ClientResponse{
		ID:        client.ID,
		CreatedAt: client.GetLastAccessTime(),
		UpdatedAt: client.GetLastAccessTime(),
	}); err != nil {
		slog.Error("ошибка при кодировании ответа", "error", err)
	}
}

func (s *Server) listClientsHandler(w http.ResponseWriter, _ *http.Request) {
	if s.rateLimiter == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Сервис ограничения скорости не настроен")
		return
	}

	if s.repository == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Репозиторий клиентов не настроен")
		return
	}

	clients, err := s.repository.ListClients()
	if err != nil {
		slog.Error("ошибка при получении списка клиентов", "error", err)
		s.writeJSONError(w, http.StatusInternalServerError, "Ошибка при получении списка клиентов")

		return
	}

	response := make([]ClientResponse, 0, len(clients))
	for _, clientData := range clients {
		response = append(response, ClientResponse{
			ID:         clientData.ID,
			Capacity:   clientData.Capacity,
			RatePerSec: clientData.RatePerSec,
			CreatedAt:  clientData.CreatedAt,
			UpdatedAt:  clientData.UpdatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("ошибка при кодировании ответа", "error", err)
	}
}

func (s *Server) deleteClientHandler(w http.ResponseWriter, r *http.Request) {
	if s.rateLimiter == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Сервис ограничения скорости не настроен")
		return
	}

	clientID := r.PathValue("id")
	if clientID == "" {
		s.writeJSONError(w, http.StatusBadRequest, "ID клиента не указан")
		return
	}

	err := s.rateLimiter.RemoveClient(clientID)
	if err != nil {
		if _, ok := err.(*errors.ErrClientNotFound); ok {
			s.writeJSONError(w, http.StatusNotFound, "Клиент не найден")
			return
		}

		slog.Error("ошибка при удалении клиента", "error", err)
		s.writeJSONError(w, http.StatusInternalServerError, "Ошибка при удалении клиента")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(ErrorResponse{
		Code:    statusCode,
		Message: message,
	}); err != nil {
		slog.Error("ошибка при кодировании ответа с ошибкой", "error", err)
	}
}

func StartWithGracefulShutdown(srv *Server) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if err := srv.Start(); err != nil {
		return err
	}

	sig := <-sigCh
	slog.Info("получен сигнал завершения", "signal", sig)

	return srv.GracefulShutdown()
}
