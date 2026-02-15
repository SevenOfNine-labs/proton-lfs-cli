SHELL := /bin/bash

GO ?= go
NODE ?= node
JS_PM ?= yarn
COREPACK_HOME_DIR := $(PWD)/.cache/corepack

ADAPTER_BIN := bin/git-lfs-proton-adapter
TRAY_BIN := bin/proton-git-lfs-tray
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS_VERSION := -X main.Version=$(VERSION)
GIT_LFS_DIR := submodules/git-lfs
DRIVE_CLI_DIR := submodules/proton-drive-cli
GO_CACHE_DIR := .cache/go-build

.PHONY: help setup setup-env install-deps \
	build build-adapter build-tray build-lfs build-drive-cli build-sea build-all build-bundle \
	install uninstall \
	test test-adapter test-tray test-lfs test-integration test-integration-timeout test-integration-stress test-integration-sdk test-e2e-mock test-e2e-real test-all \
	pass-env check-sdk-prereqs check-sdk-real-prereqs \
	fmt lint lint-go \
	clean status install-hooks

.DEFAULT_GOAL := help

help: ## Show available commands
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "%-20s %s\n", $$1, $$2}'

setup: setup-env install-deps ## Prepare local environment

setup-env: ## Create .env from .env.example if needed
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "Created .env from .env.example"; \
	else \
		echo ".env already exists"; \
	fi

install-deps: ## Install Go dependencies and JS dependencies (default: yarn via JS_PM)
	$(GO) mod download
	@if [ ! -f package.json ]; then \
		echo "package.json not found; skipped JS dependency install"; \
	elif [ "$(JS_PM)" = "yarn" ] && ! command -v yarn >/dev/null 2>&1; then \
		echo "yarn not found on PATH. Run: corepack enable"; \
		echo "Fallback: make setup JS_PM=npm"; \
		exit 1; \
	elif command -v $(JS_PM) >/dev/null 2>&1; then \
		if [ "$(JS_PM)" = "yarn" ]; then \
			YARN_VERSION="$$(COREPACK_HOME=$(COREPACK_HOME_DIR) yarn --version)"; \
			YARN_MAJOR="$${YARN_VERSION%%.*}"; \
			if [ "$$YARN_MAJOR" -lt 4 ]; then \
				echo "yarn $$YARN_VERSION detected; Yarn 4+ required for this repository."; \
				echo "Run: corepack enable && corepack prepare yarn@4.1.1 --activate"; \
				echo "Fallback: make setup JS_PM=npm"; \
				exit 1; \
			fi; \
			COREPACK_HOME=$(COREPACK_HOME_DIR) $(JS_PM) install; \
		else \
			$(JS_PM) install; \
		fi; \
	else \
		echo "$(JS_PM) not found; skipped JS dependency install"; \
		echo "Install npm/yarn or run with JS_PM=<available-manager>"; \
	fi

build: build-adapter ## Build first-party binaries

build-all: build-adapter build-tray build-lfs build-drive-cli ## Build adapter, tray app, Git LFS submodule, and proton-drive-cli

build-adapter: ## Build the custom transfer adapter
	@mkdir -p bin
	$(GO) build -trimpath -o $(ADAPTER_BIN) ./cmd/adapter

build-tray: ## Build the system tray application (requires CGO)
	@mkdir -p bin
	CGO_ENABLED=1 $(GO) build -trimpath -ldflags '$(LDFLAGS_VERSION)' -o $(TRAY_BIN) ./cmd/tray

build-sea: build-drive-cli ## Build proton-drive-cli as a standalone Node.js SEA binary
	@bash scripts/build-sea.sh

build-bundle: build-adapter build-tray build-sea ## Build all components into dist/ for packaging
	@mkdir -p dist
	@cp bin/git-lfs-proton-adapter dist/ 2>/dev/null || true
	@cp bin/proton-git-lfs-tray dist/ 2>/dev/null || true
	@cp bin/proton-drive-cli dist/ 2>/dev/null || true
	@echo "Bundle assembled in dist/"

# ---------- install / uninstall ----------

INSTALL_APP ?= /Applications/ProtonGitLFS.app
INSTALL_BIN ?= $(HOME)/.local/bin

ifeq ($(shell uname -s),Darwin)

install: build-bundle ## Install bundle (macOS: .app to /Applications, or set INSTALL_APP)
	@mkdir -p "$(INSTALL_APP)/Contents/MacOS" "$(INSTALL_APP)/Contents/Helpers" "$(INSTALL_APP)/Contents/Resources"
	@cp dist/proton-git-lfs-tray      "$(INSTALL_APP)/Contents/MacOS/proton-git-lfs-tray"
	@cp dist/git-lfs-proton-adapter   "$(INSTALL_APP)/Contents/Helpers/git-lfs-proton-adapter"
	@cp dist/proton-drive-cli         "$(INSTALL_APP)/Contents/Helpers/proton-drive-cli"
	@chmod +x "$(INSTALL_APP)/Contents/MacOS/proton-git-lfs-tray" \
		"$(INSTALL_APP)/Contents/Helpers/git-lfs-proton-adapter" \
		"$(INSTALL_APP)/Contents/Helpers/proton-drive-cli"
	@bash scripts/ensure-info-plist.sh "$(INSTALL_APP)/Contents/Info.plist" "$(VERSION)"
	@codesign --force --deep --sign - "$(INSTALL_APP)"
	@xattr -dr com.apple.quarantine "$(INSTALL_APP)" 2>/dev/null || true
	@mkdir -p "$(INSTALL_BIN)"
	@ln -sf "$(INSTALL_APP)/Contents/MacOS/proton-git-lfs-tray" "$(INSTALL_BIN)/proton-git-lfs"
	@ln -sf "$(INSTALL_APP)/Contents/Helpers/proton-drive-cli" "$(INSTALL_BIN)/proton-drive-cli"
	@echo "Installed to $(INSTALL_APP)"
	@echo "CLI: $(INSTALL_BIN)/proton-git-lfs"
	@echo "CLI: $(INSTALL_BIN)/proton-drive-cli"

uninstall: ## Remove installed .app bundle
	rm -rf "$(INSTALL_APP)"
	rm -f "$(INSTALL_BIN)/proton-git-lfs" "$(INSTALL_BIN)/proton-drive-cli"
	@echo "Removed $(INSTALL_APP)"

else

install: build-bundle ## Install bundle (Linux: binaries to ~/.local/bin, or set INSTALL_BIN)
	@if uname -s | grep -qiE 'mingw|msys|cygwin'; then \
		echo ""; \
		echo "Error: 'make install' is not yet supported on Windows."; \
		echo ""; \
		echo "The built binaries are in dist/:"; \
		echo "  dist/git-lfs-proton-adapter.exe"; \
		echo "  dist/proton-git-lfs-tray.exe"; \
		echo "  dist/proton-drive-cli.exe"; \
		echo ""; \
		echo "Copy them to a directory on your PATH, or use the .zip from GitHub Releases."; \
		echo ""; \
		exit 1; \
	fi
	@mkdir -p "$(INSTALL_BIN)"
	@cp dist/proton-git-lfs-tray    "$(INSTALL_BIN)/proton-git-lfs-tray"
	@cp dist/git-lfs-proton-adapter "$(INSTALL_BIN)/git-lfs-proton-adapter"
	@cp dist/proton-drive-cli       "$(INSTALL_BIN)/proton-drive-cli"
	@chmod +x "$(INSTALL_BIN)/proton-git-lfs-tray" \
		"$(INSTALL_BIN)/proton-drive-cli" \
		"$(INSTALL_BIN)/git-lfs-proton-adapter"
	@ln -sf "$(INSTALL_BIN)/proton-git-lfs-tray" "$(INSTALL_BIN)/proton-git-lfs"
	@echo "Installed to $(INSTALL_BIN)"

uninstall: ## Remove installed binaries
	@if uname -s | grep -qiE 'mingw|msys|cygwin'; then \
		echo "Error: 'make uninstall' is not yet supported on Windows."; \
		exit 1; \
	fi
	rm -f "$(INSTALL_BIN)/proton-git-lfs-tray" \
		"$(INSTALL_BIN)/git-lfs-proton-adapter" \
		"$(INSTALL_BIN)/proton-drive-cli" \
		"$(INSTALL_BIN)/proton-git-lfs"
	@echo "Removed binaries from $(INSTALL_BIN)"

endif

# ---------- end install / uninstall ----------

build-lfs: ## Build Git LFS submodule
	@if [ ! -d $(GIT_LFS_DIR) ]; then \
		echo "$(GIT_LFS_DIR) not found"; \
		exit 1; \
	fi
	@$(MAKE) -C $(GIT_LFS_DIR)

build-drive-cli: ## Build proton-drive-cli TypeScript bridge
	@if [ ! -d $(DRIVE_CLI_DIR) ]; then \
		echo "$(DRIVE_CLI_DIR) not found. Run: git submodule update --init --recursive"; \
		exit 1; \
	fi
	@if [ "$(JS_PM)" = "yarn" ]; then \
		COREPACK_HOME=$(COREPACK_HOME_DIR) $(JS_PM) workspace @sevenofnine-ai/proton-drive-cli build; \
	else \
		$(JS_PM) --workspace $(DRIVE_CLI_DIR) run build; \
	fi

test: test-adapter test-tray ## Run core tests

test-all: test-adapter test-tray test-lfs test-integration test-e2e-mock ## Run all test suites

test-adapter: ## Run adapter tests
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -race -cover ./cmd/adapter/...

test-tray: ## Run tray app tests (requires CGO)
	@mkdir -p $(GO_CACHE_DIR)
	CGO_ENABLED=1 GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -race -cover ./cmd/tray/...

test-lfs: ## Run Git LFS submodule tests
	@if [ ! -d $(GIT_LFS_DIR) ]; then \
		echo "$(GIT_LFS_DIR) not found"; \
		exit 1; \
	fi
	@$(MAKE) -C $(GIT_LFS_DIR) test

test-integration: ## Run integration tests (requires git + git-lfs binaries)
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/...

test-integration-timeout: ## Run timeout semantics integration tests for stalled adapter behavior
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransferTimeout' -v

test-integration-stress: ## Run high-volume concurrency stress/soak integration tests
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransferConcurrentStressSoak$$' -v

test-integration-failure-modes: ## Run failure mode integration tests (wrong OID, crash, hang)
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransfer(RejectsWrongOID|FailsWhenAdapter)' -v

test-integration-config-matrix: ## Run direction config matrix integration tests
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransferDirection' -v

test-integration-credentials: ## Run credential flow security tests
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run Credential -v

test-integration-sdk: check-sdk-prereqs ## Run sdk backend integration tests (requires proton-drive-cli and pass-cli)
	@mkdir -p $(GO_CACHE_DIR)
	@eval "$$(./scripts/export-pass-env.sh)" && \
		GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run SDK -v

test-e2e-mock: build-adapter ## Mocked E2E pipeline (no real credentials)
	@mkdir -p $(GO_CACHE_DIR)
	@chmod +x scripts/mock-pass-cli.sh
	PROTON_PASS_CLI_BIN=$(PWD)/scripts/mock-pass-cli.sh \
		PROTON_DRIVE_CLI_BIN=$(PWD)/tests/testdata/mock-proton-drive-cli.js \
		PASS_MOCK_USERNAME=integration-user@proton.test \
		PASS_MOCK_PASSWORD=integration-password \
		GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run E2EMocked -v

test-e2e-real: build-adapter build-drive-cli ## Real Proton Drive E2E (requires pass-cli login + build-drive-cli)
	@mkdir -p $(GO_CACHE_DIR)
	@eval "$$(./scripts/export-pass-env.sh)" && \
		GOCACHE=$(PWD)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run E2E -v

pass-env: ## Print export commands for Proton Pass-based adapter credentials
	@./scripts/export-pass-env.sh

check-sdk-prereqs: ## Verify prerequisites for sdk integration tests
	@command -v git-lfs >/dev/null 2>&1 || (echo "git-lfs not found on PATH" && exit 1)
	@command -v "$${PROTON_PASS_CLI_BIN:-pass-cli}" >/dev/null 2>&1 || (echo "pass-cli not found on PATH (or PROTON_PASS_CLI_BIN invalid)" && exit 1)
	@DRIVE_CLI_BIN="$${PROTON_DRIVE_CLI_BIN:-$(DRIVE_CLI_DIR)/dist/index.js}"; \
		if [ ! -f "$$DRIVE_CLI_BIN" ]; then \
			echo "proton-drive-cli not built: $$DRIVE_CLI_BIN not found"; \
			echo "Run: make build-drive-cli"; \
			exit 1; \
		fi
	@NODE_BIN_RESOLVED="$$(command -v $(NODE) 2>/dev/null || true)"; \
		if [ -z "$$NODE_BIN_RESOLVED" ] && command -v zsh >/dev/null 2>&1; then \
			NODE_BIN_RESOLVED="$$(zsh -lc 'command -v node' 2>/dev/null || true)"; \
		fi; \
		if [ -z "$$NODE_BIN_RESOLVED" ]; then \
			echo "node not found on PATH for non-interactive make shell"; \
			echo "If node is configured in ~/.zshrc (nvm/fnm), run:"; \
			echo "  make test-integration-sdk NODE=/absolute/path/to/node"; \
			exit 1; \
		fi; \
		echo "Resolved node binary: $$NODE_BIN_RESOLVED"
	@./scripts/export-pass-env.sh >/dev/null
	@echo "SDK integration prerequisites OK"

fmt: ## Format Go code
	$(GO) fmt ./cmd/... ./internal/...

lint: lint-go ## Run lint checks

lint-go: ## Run Go vet and golangci-lint when available
	$(GO) vet ./cmd/... ./internal/...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./cmd/... ./internal/...; \
	else \
		echo "golangci-lint not installed; skipped"; \
	fi

install-hooks: ## Install pre-commit hooks
	@if ! command -v pre-commit >/dev/null 2>&1; then \
		echo "pre-commit is not installed"; \
		exit 1; \
	fi
	pre-commit install

status: ## Print project status
	@echo "Go: $$($(GO) version)"
	@NODE_BIN_RESOLVED="$$(command -v $(NODE) 2>/dev/null || true)"; \
		if [ -n "$$NODE_BIN_RESOLVED" ]; then \
			echo "Node: $$($$NODE_BIN_RESOLVED --version)"; \
		else \
			echo "Node: not found"; \
		fi
	@if command -v $(JS_PM) >/dev/null 2>&1; then \
		if [ "$(JS_PM)" = "yarn" ]; then \
			echo "JS PM ($(JS_PM)): $$(COREPACK_HOME=$(COREPACK_HOME_DIR) $(JS_PM) --version 2>/dev/null || echo unavailable)"; \
		else \
			echo "JS PM ($(JS_PM)): $$($(JS_PM) --version)"; \
		fi; \
	else \
		echo "JS PM ($(JS_PM)): not found"; \
	fi
	@echo "Adapter binary: $$([ -f $(ADAPTER_BIN) ] && echo present || echo missing)"

clean: ## Remove generated files
	rm -rf bin
	rm -rf $(GO_CACHE_DIR)
	$(GO) clean -cache -testcache
