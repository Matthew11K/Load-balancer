COVERAGE_FILE ?= coverage.out

.PHONY: build
build: build_bot build_scrapper

## test: run all tests
.PHONY: test
test:
	@go test -coverpkg='balancer/...' --race -count=1 -coverprofile='$(COVERAGE_FILE)' ./...
	@go tool cover -func='$(COVERAGE_FILE)' | grep ^total | tr -s '\t'

.PHONY: lint
lint-golang:
	@if ! command -v 'golangci-lint' &> /dev/null; then \
  		echo "Please install golangci-lint!"; exit 1; \
  	fi;
	@golangci-lint -v run --fix ./...

.PHONY: clean
clean:
	@rm -rf./bin