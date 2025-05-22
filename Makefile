# Makefile for rate-limit project

# Variables
TOOLS_DIR := $(PWD)/bin/tools

# Default target
.PHONY: default
default: help

.PHONY: install
install: ## Install development tools
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install golang.org/x/tools/cmd/goimports@latest

# Main targets
.PHONY: check
check: tidy fmt tests ## Run all project checks

.PHONY: tests
tests: ## Run tests
	@echo "### Tests"
	go test -v ./...

.PHONY: tidy
tidy: ## Tidy go modules
	@echo "### Tidy"
	go mod tidy

.PHONY: fmt
fmt: ## Run code formatter
	@echo "### Format"
	go fmt ./...
	$(TOOLS_DIR)/goimports -w $$(find . -type f -name '*.go' -not -path "*generated.go")

help: ## Display this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)