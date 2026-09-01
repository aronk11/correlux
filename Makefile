# kubeui — development entry points.
#
# Everything CI does is available here, so a contributor can reproduce a failed
# pipeline locally with a single command.

BINARY      := kubeui
MODULE      := github.com/akiesel/kubeui
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.Date=$(DATE)

GOLANGCI_VERSION := v2.6.2
TOOLS_DIR := $(CURDIR)/.tools

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the binary into ./bin
	@mkdir -p bin
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/kubeui

.PHONY: install
install: ## Install the binary into $GOPATH/bin
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/kubeui

.PHONY: run
run: ## Run kubeui against your current kubeconfig
	go run ./cmd/kubeui

.PHONY: test
test: ## Run the unit tests
	go test ./...

.PHONY: test-race
test-race: ## Run the tests under the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run tests with coverage and print a summary
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: $(TOOLS_DIR)/golangci-lint ## Run golangci-lint
	$(TOOLS_DIR)/golangci-lint run ./...

.PHONY: fmt
fmt: $(TOOLS_DIR)/golangci-lint ## Format the code
	$(TOOLS_DIR)/golangci-lint fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: vuln
vuln: ## Check dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: tidy
tidy: ## Tidy and verify the module graph
	go mod tidy
	go mod verify

.PHONY: check
check: vet lint test-race ## Everything CI runs

.PHONY: frames
frames: ## Render the TUI to plain text files for review (no terminal needed)
	@mkdir -p .frames
	KUBEUI_DUMP_DIR=$(CURDIR)/.frames go test ./internal/ui/app -run TestDumpFrames -count=1
	@echo "wrote .frames/*.txt"

.PHONY: clean
clean: ## Remove build and test artefacts
	rm -rf bin dist coverage.out .frames

$(TOOLS_DIR)/golangci-lint:
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
