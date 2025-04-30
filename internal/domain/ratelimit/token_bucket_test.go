package ratelimit_test

import (
	"testing"
	"time"

	"balancer/internal/domain/ratelimit"

	"github.com/stretchr/testify/assert"
)

func TestTokenBucket_AllowRequest(t *testing.T) {
	bucket := ratelimit.NewTokenBucket(5, 1)

	for i := 0; i < 5; i++ {
		allowed := bucket.AllowRequest()
		assert.True(t, allowed, "Запрос %d должен быть разрешен", i+1)
	}

	allowed := bucket.AllowRequest()
	assert.False(t, allowed, "Шестой запрос должен быть отклонен")
}

func TestTokenBucket_Refill(t *testing.T) {
	bucket := ratelimit.NewTokenBucket(5, 2)

	for i := 0; i < 5; i++ {
		allowed := bucket.AllowRequest()
		assert.True(t, allowed, "Запрос %d должен быть разрешен", i+1)
	}

	time.Sleep(1 * time.Second)

	bucket.RefillNow()

	for i := 0; i < 2; i++ {
		allowed := bucket.AllowRequest()
		assert.True(t, allowed, "Запрос %d после ожидания должен быть разрешен", i+1)
	}

	allowed := bucket.AllowRequest()
	assert.False(t, allowed, "Третий запрос после ожидания должен быть отклонен")
}

func TestClient_AllowRequest(t *testing.T) {
	client := ratelimit.NewClient("test-client", 3, 1)

	for i := 0; i < 3; i++ {
		allowed := client.AllowRequest()
		assert.True(t, allowed, "Запрос %d должен быть разрешен", i+1)
	}

	allowed := client.AllowRequest()
	assert.False(t, allowed, "Четвертый запрос должен быть отклонен")

	prevAccess := client.GetLastAccessTime()

	time.Sleep(1 * time.Second)

	client.Bucket.RefillNow()

	allowed = client.AllowRequest()
	assert.True(t, allowed, "Запрос после ожидания должен быть разрешен")

	assert.True(t, client.GetLastAccessTime().After(prevAccess), "Время последнего доступа должно быть обновлено")
}
