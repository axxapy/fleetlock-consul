.PHONY: help bootstrap fmt lint test coverage e2e run build docker clean
.DEFAULT_GOAL := help

BUILD_VERSION ?= $(shell git describe --tags --always --dirty)

help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-25s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## Auto-format code
	gofmt -s -w .

lint: ## Run linter
	go vet ./...

test: generate ## Run tests
	go test ./...

coverage: generate ## Run tests with coverage
	go test -cover ./...

e2e: ## Run E2E tests (docker compose)
	docker compose -f build/docker-compose.e2e.yml up --build --abort-on-container-exit --exit-code-from e2e; \
	ret=$$?; docker compose -f build/docker-compose.e2e.yml down; exit $$ret

run: ## Run the server
	go run ./cmd/server

build: ## Build binary
	go build -ldflags="-s -w -X main.version=$(BUILD_VERSION)" -trimpath -o bin/fleetlock-consul ./cmd/server

docker: ## Build Docker image
	docker build --build-arg VERSION=$(BUILD_VERSION) -t axxapy/fleetlock-consul -f build/Dockerfile .

generate: ## Run go generate
	go generate ./...

clean: ## Remove build artifacts
	rm -rf bin/
