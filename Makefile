SHELL := /bin/bash

# Targets stay intentionally few. Every target here is runnable as-is.

BIN_DIR := bin
BINARY  := $(BIN_DIR)/api
PKG     := ./...
CMD     := ./cmd/api

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API with default configuration
	go run $(CMD)

.PHONY: build
build: ## Build the API into bin/api
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BINARY) $(CMD)

.PHONY: test
test: ## Run all tests
	go test $(PKG)

.PHONY: test-race
test-race: ## Run all tests with the race detector and coverage
	go test -race -covermode=atomic -coverprofile=coverage.out $(PKG)

.PHONY: cover
cover: test-race ## Run tests and open the coverage report
	go tool cover -html=coverage.out

.PHONY: fmt
fmt: ## Format all Go files
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Verify formatting without writing changes
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not formatted:"; \
		echo "$$unformatted"; \
		echo "Run 'make fmt' to fix them."; \
		exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: lint
lint: ## Run golangci-lint (requires it on PATH)
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: check
check: fmt-check vet test build ## Run every check required before pushing

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out