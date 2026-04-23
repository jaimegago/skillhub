.DEFAULT_GOAL := help
.PHONY: build test lint run install clean help

BIN         := bin/skillhub
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE        := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
# Install directory: $GOBIN if set, else $HOME/go/bin
INSTALL_DIR := $(shell echo $${GOBIN:-$$HOME/go/bin})

build: ## Compile binary to bin/skillhub
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/skillhub

test: ## Run all tests
	go test ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

run: build ## Build and start MCP server on stdio
	$(BIN) mcp

install: build ## Build and install binary to $$GOBIN (else $$HOME/go/bin)
	@mkdir -p $(INSTALL_DIR)
	cp $(BIN) $(INSTALL_DIR)/skillhub
	@echo "Installed: $(INSTALL_DIR)/skillhub"
	@echo "Hint: ensure $(INSTALL_DIR) is on your PATH"

clean: ## Remove compiled binary
	rm -f $(BIN)

help: ## Show available make targets
	@echo "Usage: make [target]"
	@echo ""
	@printf "  %-12s %s\n" "Target" "Description"
	@printf "  %-12s %s\n" "------" "-----------"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
