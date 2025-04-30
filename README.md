# Балансировщик нагрузки

Простой балансировщик нагрузки на Go, который принимает входящие HTTP-запросы и распределяет их по пулу бэкенд-серверов. Включает в себя механизм ограничения скорости запросов (rate limiting) на базе алгоритма Token Bucket.

## Возможности

- HTTP-сервер с обратным прокси для перенаправления запросов
- Несколько алгоритмов балансировки нагрузки:
  - Round Robin
  - Least Connections
  - Random
- Проверка доступности бэкенд-серверов (Health Checks)
- Ограничение скорости запросов (Rate Limiting) на основе алгоритма Token Bucket
- Поддержка настройки различных ограничений для разных клиентов
- Сохранение настроек клиентов в PostgreSQL
- API для управления клиентами и их ограничениями
- Корректное завершение работы (Graceful Shutdown)
- Детальное логирование

## Требования

- Go 1.22 или выше
- PostgreSQL (опционально, для персистентного хранения настроек клиентов)
- Docker и Docker Compose (для запуска в контейнерах)

## Установка и запуск

### Локальная сборка и запуск

1. Клонировать репозиторий:
   ```
   git clone <url-репозитория>
   cd Load-balancer
   ```

2. Собрать приложение:
   ```
   make build
   ```

3. Запустить приложение:
   ```
   make run-local
   ```

### Запуск с помощью Docker Compose

1. Запустить контейнеры:
   ```
   make run-docker
   ```

2. Остановить контейнеры:
   ```
   make stop-docker
   ```

Этот метод запустит балансировщик, базу данных PostgreSQL и три тестовых бэкенд-сервера.

## Конфигурация

Конфигурация балансировщика осуществляется через файл `config.yaml`. Пример конфигурации:

```yaml
server:
  port: 8080
  readTimeout: 5s
  writeTimeout: 10s
  readHeaderTimeout: 5s
  
backends:
  - url: "http://localhost:8081"
    weight: 1
  - url: "http://localhost:8082"
    weight: 1
  - url: "http://localhost:8083"
    weight: 1
    
loadbalancing:
  algorithm: "round-robin"  # Доступные алгоритмы: round-robin, least-connections, random
  healthCheckInterval: 10s
  healthCheckTimeout: 2s
  
rateLimiting:
  defaultCapacity: 100 
  defaultRatePerSecond: 10
  enabled: true
  storageType: "postgres"  # Тип хранилища: postgres или memory
  
database:
  host: "postgres"
  port: 5432
  user: "postgres"
  password: "postgres" 
  dbname: "balancer"
  sslmode: "disable"
  
logging:
  level: "info"  # debug, info, warn, error
  format: "json"  # json или text
```