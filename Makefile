COVERAGE_FILE ?= coverage.out

.PHONY: build
build:
	@go build -o ./bin/balancer ./cmd/balancer

## test: run all tests
.PHONY: test
test:
	@go test -coverpkg='balancer/...' --race -count=1 -coverprofile='$(COVERAGE_FILE)' ./...
	@go tool cover -func='$(COVERAGE_FILE)' | grep ^total | tr -s '\t'

## bench: run benchmark tests
.PHONY: bench
bench:
	@go test -bench=. -race ./test/...

## load-test: run load test with Apache Bench
.PHONY: load-test
load-test:
	@if ! command -v 'ab' &> /dev/null; then \
		echo "Apache Bench (ab) не установлен. Установите его для запуска нагрузочного тестирования."; exit 1; \
	fi;
	@echo "Запуск нагрузочного тестирования с помощью Apache Bench..."
	@ab -n 5000 -c 100 http://localhost:8080/

.PHONY: lint
lint:
	@if ! command -v 'golangci-lint' &> /dev/null; then \
  		echo "Please install golangci-lint!"; exit 1; \
  	fi;
	@golangci-lint -v run --fix ./...

.PHONY: clean
clean:
	@rm -rf ./bin

.PHONY: run-local
run-local: build
	@./bin/balancer -config=./config.local.yaml

.PHONY: run-local-background
run-local-background: build
	@./bin/balancer -config=./config.local.yaml &

.PHONY: run-docker
run-docker:
	@docker compose down
	@docker compose up --build

.PHONY: stop-docker
stop-docker:
	@docker compose down
