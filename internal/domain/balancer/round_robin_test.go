package balancer_test

import (
	stderrors "errors"
	"testing"
	"time"

	"balancer/internal/domain/balancer"
	"balancer/internal/domain/errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockHealthChecker struct {
	mock.Mock
}

func (m *MockHealthChecker) CheckHealth(backend *balancer.Backend, timeout time.Duration) (bool, error) {
	args := m.Called(backend, timeout)
	return args.Bool(0), args.Error(1)
}

func TestRoundRobinBalancer_NextBackend(t *testing.T) {
	mockHealthChecker := &MockHealthChecker{}
	lb := balancer.NewRoundRobinBalancer(mockHealthChecker)

	_, err := lb.NextBackend()
	assert.Error(t, err)
	assert.IsType(t, &errors.ErrNoAvailableBackends{}, err)

	backend1, _ := balancer.NewBackend("http://backend1", 1)
	backend2, _ := balancer.NewBackend("http://backend2", 1)
	backend3, _ := balancer.NewBackend("http://backend3", 1)

	lb.AddBackend(backend1)
	lb.AddBackend(backend2)
	lb.AddBackend(backend3)

	result1, err := lb.NextBackend()
	assert.NoError(t, err)
	assert.Equal(t, backend1.URL.String(), result1.URL.String())

	result2, err := lb.NextBackend()
	assert.NoError(t, err)
	assert.Equal(t, backend2.URL.String(), result2.URL.String())

	result3, err := lb.NextBackend()
	assert.NoError(t, err)
	assert.Equal(t, backend3.URL.String(), result3.URL.String())

	result4, err := lb.NextBackend()
	assert.NoError(t, err)
	assert.Equal(t, backend1.URL.String(), result4.URL.String())
}

func TestRoundRobinBalancer_HealthCheck(t *testing.T) {
	mockHealthChecker := &MockHealthChecker{}
	lb := balancer.NewRoundRobinBalancer(mockHealthChecker)

	backend1, _ := balancer.NewBackend("http://backend1", 1)
	backend2, _ := balancer.NewBackend("http://backend2", 1)

	lb.AddBackend(backend1)
	lb.AddBackend(backend2)

	mockHealthChecker.On("CheckHealth", backend1, 2*time.Second).Return(true, nil)
	mockHealthChecker.On("CheckHealth", backend2, 2*time.Second).Return(false, stderrors.New("connection refused"))

	err := lb.HealthCheck(2 * time.Second)
	assert.NoError(t, err)

	mockHealthChecker.AssertExpectations(t)

	assert.True(t, backend1.GetHealth())
	assert.False(t, backend2.GetHealth())

	result, err := lb.NextBackend()
	assert.NoError(t, err)
	assert.Equal(t, backend1.URL.String(), result.URL.String())

	backend1.SetHealth(false)

	_, err = lb.NextBackend()
	assert.Error(t, err)
	assert.IsType(t, &errors.ErrNoAvailableBackends{}, err)
}
