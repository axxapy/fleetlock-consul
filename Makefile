.PHONY: help bootstrap fmt lint test coverage run build docker clean
.DEFAULT_GOAL := help

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

run: ## Run the server
	go run ./cmd/server

build: ## Build binary
	go build -o bin/fleetlock-consul ./cmd/server

docker: ## Build Docker image
	docker build -t axxapy/fleetlock-consul -f build/Dockerfile .

generate: ## Run go generate
	go generate ./...

clean: ## Remove build artifacts
	rm -rf bin/
