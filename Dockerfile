# Этап сборки
FROM golang:1.22-alpine AS builder

# Установка необходимых пакетов
RUN apk add --no-cache git

# Установка рабочей директории
WORKDIR /app

# Копирование и загрузка зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копирование исходного кода
COPY . .

# Сборка приложения
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o balancer ./cmd/balancer

# Финальный этап
FROM alpine:latest

# Установка необходимых пакетов
RUN apk --no-cache add ca-certificates tzdata

# Установка временной зоны
ENV TZ=Europe/Moscow

# Создание пользователя без прав root
RUN adduser -D -u 1000 appuser
USER appuser

# Создание рабочей директории
WORKDIR /app

# Копирование бинарного файла из этапа сборки
COPY --from=builder /app/balancer .
COPY --from=builder /app/config.yaml .

# Открытие порта
EXPOSE 8080

# Запуск приложения
ENTRYPOINT ["./balancer"]
CMD ["--config", "config.yaml"]
