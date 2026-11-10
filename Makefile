SHELL := /bin/bash

GO ?= go
NODE ?= node
# proton-drive-cli owns its JavaScript dependencies (package.json and
# yarn.lock in the submodule); Yarn 4 comes from Corepack through its
# packageManager field. npm cannot install its `portal:` dependency.
YARN ?= yarn
COREPACK_HOME_DIR := $(CURDIR)/.cache/corepack
# Pinned to the version bundled by DavidAnson/markdownlint-cli2-action in
# .github/workflows/docs.yml so local and CI results match.
MARKDOWNLINT_CLI2_VERSION := 0.24.0

# Cross-compile with GOOS=windows make build-adapter (binaries get .exe).
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
EXE := $(if $(filter windows,$(GOOS)),.exe,)
ADAPTER_BIN := bin/git-lfs-proton-adapter$(EXE)
TRAY_BIN := bin/proton-lfs-tray$(EXE)
DRIVE_CLI_SEA_BIN := bin/proton-drive-cli$(EXE)
# One version source: the release tag in CI (VERSION=...), git describe
# locally. scripts/build-go.sh stamps it together with the commit and a
# reproducible BuildTime (SOURCE_DATE_EPOCH or the commit date).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
export VERSION
GIT_LFS_DIR := submodules/git-lfs
DRIVE_CLI_DIR := submodules/proton-drive-cli
SDK_JS_DIR := $(DRIVE_CLI_DIR)/submodules/sdk/client/js
GO_CACHE_DIR := .cache/go-build
GO_TEST_PKGS := ./cmd/... ./internal/...
LIVE_CANARY_ACK_VALUE := I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT
# Credential store of the session under test; the Proton Pass export
# (scripts/export-pass-env.sh) is needed only for pass-cli.
CREDENTIAL_STORE ?= $(or $(PROTON_CREDENTIAL_STORE),pass-cli)
PASS_ENV := $(if $(filter pass-cli,$(CREDENTIAL_STORE)),eval "$$(./scripts/export-pass-env.sh)" &&,)

.PHONY: help setup setup-env install-deps \
	build build-adapter build-tray build-lfs build-sdk build-drive-cli build-sea build-all build-bundle \
	install uninstall \
	test test-adapter test-tray test-lfs test-integration test-integration-adapter-exit test-integration-stress \
	test-integration-failure-modes test-integration-config-matrix test-integration-credentials test-integration-sdk \
	test-e2e-mock test-e2e-real test-all \
	live-canary-preflight live-drive-scope-canary browser-fork-canary pass-env check-submodules check-live-canary-ack check-live-canary-doctor-args check-browser-fork-canary-args check-sdk-prereqs \
	fmt fmt-check lint lint-go lint-comments \
	docs docs-lint \
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

install-deps: ## Install Go modules and proton-drive-cli's JS dependencies (Yarn 4 via Corepack)
	$(GO) mod download
	@if [ ! -f $(DRIVE_CLI_DIR)/package.json ]; then \
		echo "$(DRIVE_CLI_DIR) is not initialised; skipped JS dependency install (see make check-submodules)"; \
	elif ! command -v $(YARN) >/dev/null 2>&1; then \
		echo "yarn not found on PATH. Run: corepack enable"; \
		exit 1; \
	else \
		YARN_VERSION="$$(cd $(DRIVE_CLI_DIR) && COREPACK_HOME=$(COREPACK_HOME_DIR) $(YARN) --version)"; \
		if [ "$${YARN_VERSION%%.*}" -lt 4 ]; then \
			echo "yarn $$YARN_VERSION detected; Yarn 4+ is required. Run: corepack enable"; \
			exit 1; \
		fi; \
		cd $(DRIVE_CLI_DIR) && COREPACK_HOME=$(COREPACK_HOME_DIR) $(YARN) install; \
	fi

build: build-adapter ## Build first-party binaries

build-all: check-submodules build-adapter build-tray build-lfs build-drive-cli ## Build adapter, tray app, Git LFS submodule, vendored SDK, and proton-drive-cli

build-adapter: ## Build the custom transfer adapter (pure Go; GOOS/GOARCH to cross-compile)
	@GO="$(GO)" bash scripts/build-go.sh adapter "$(ADAPTER_BIN)"

build-tray: ## Build the system tray application (cgo only on macOS)
	@GO="$(GO)" bash scripts/build-go.sh tray "$(TRAY_BIN)"

build-sea: build-drive-cli ## Build proton-drive-cli as a standalone Node.js SEA binary (Node.js 26, see .node-version)
	@NODE="$(NODE)" SEA_OUTPUT_DIR=bin bash $(DRIVE_CLI_DIR)/scripts/build-sea.sh

build-bundle: check-submodules build-adapter build-tray build-sea ## Build all components into dist/ for packaging
	@mkdir -p dist
	cp "$(ADAPTER_BIN)" "$(TRAY_BIN)" "$(DRIVE_CLI_SEA_BIN)" dist/
	@echo "Bundle assembled in dist/"

# ---------- install / uninstall ----------

INSTALL_APP ?= /Applications/ProtonLFS.app
INSTALL_BIN ?= $(HOME)/.local/bin

# install_exec copies $(1) beside $(2) and renames it into place: rename(2)
# atomically replaces an executable a running process uses (cp(1) fails with
# ETXTBSY); a failed step removes the temporary file.
define install_exec
t="$(2).tmp.$$$$"; cp "$(1)" "$$t" && chmod +x "$$t" && mv -f "$$t" "$(2)" || { rm -f "$$t"; exit 1; }
endef

# Help entries for the platform-specific rules below (one line each in
# `make help`; the recipes are chosen by host OS).
install: ## Install bundle (macOS: .app to INSTALL_APP; Linux: binaries to INSTALL_BIN)
uninstall: ## Remove the installed bundle or binaries

ifeq ($(shell uname -s),Darwin)

install: build-bundle
	@mkdir -p "$(INSTALL_APP)/Contents/MacOS" "$(INSTALL_APP)/Contents/Helpers" "$(INSTALL_APP)/Contents/Resources"
	@$(call install_exec,dist/proton-lfs-tray,$(INSTALL_APP)/Contents/MacOS/proton-lfs-tray)
	@$(call install_exec,dist/git-lfs-proton-adapter,$(INSTALL_APP)/Contents/Helpers/git-lfs-proton-adapter)
	@$(call install_exec,dist/proton-drive-cli,$(INSTALL_APP)/Contents/Helpers/proton-drive-cli)
	@if [ -f cmd/tray/AppIcon.icns ]; then \
		cp cmd/tray/AppIcon.icns "$(INSTALL_APP)/Contents/Resources/AppIcon.icns"; \
	fi
	@bash scripts/ensure-info-plist.sh "$(INSTALL_APP)/Contents/Info.plist" "$(VERSION)"
	@codesign --force --deep --sign - "$(INSTALL_APP)"
	@xattr -dr com.apple.quarantine "$(INSTALL_APP)" 2>/dev/null || true
	@mkdir -p "$(INSTALL_BIN)"
	@ln -sf "$(INSTALL_APP)/Contents/MacOS/proton-lfs-tray" "$(INSTALL_BIN)/proton-lfs-cli"
	@ln -sf "$(INSTALL_APP)/Contents/Helpers/proton-drive-cli" "$(INSTALL_BIN)/proton-drive-cli"
	@echo "Installed to $(INSTALL_APP)"
	@echo "CLI: $(INSTALL_BIN)/proton-lfs-cli"
	@echo "CLI: $(INSTALL_BIN)/proton-drive-cli"

uninstall:
	rm -rf "$(INSTALL_APP)"
	rm -f "$(INSTALL_BIN)/proton-lfs-cli" "$(INSTALL_BIN)/proton-drive-cli"
	@echo "Removed $(INSTALL_APP)"

else

install: build-bundle
	@if uname -s | grep -qiE 'mingw|msys|cygwin'; then \
		echo ""; \
		echo "Windows: unzip the release bundle onto PATH; make install targets macOS and Linux."; \
		echo ""; \
		echo "The built binaries are in dist/:"; \
		echo "  dist/git-lfs-proton-adapter$(EXE)"; \
		echo "  dist/proton-lfs-tray$(EXE)"; \
		echo "  dist/proton-drive-cli$(EXE)"; \
		echo ""; \
		echo "Copy them to a directory on your PATH, or use the .zip from GitHub Releases."; \
		echo ""; \
		exit 1; \
	fi
	@mkdir -p "$(INSTALL_BIN)"
	@$(call install_exec,dist/proton-lfs-tray,$(INSTALL_BIN)/proton-lfs-tray)
	@$(call install_exec,dist/git-lfs-proton-adapter,$(INSTALL_BIN)/git-lfs-proton-adapter)
	@$(call install_exec,dist/proton-drive-cli,$(INSTALL_BIN)/proton-drive-cli)
	@ln -sf "$(INSTALL_BIN)/proton-lfs-tray" "$(INSTALL_BIN)/proton-lfs-cli"
	@echo "Installed to $(INSTALL_BIN)"

uninstall:
	@if uname -s | grep -qiE 'mingw|msys|cygwin'; then \
		echo "Windows: unzip the release bundle onto PATH; make install targets macOS and Linux."; \
		exit 1; \
	fi
	rm -f "$(INSTALL_BIN)/proton-lfs-tray" \
		"$(INSTALL_BIN)/git-lfs-proton-adapter" \
		"$(INSTALL_BIN)/proton-drive-cli" \
		"$(INSTALL_BIN)/proton-lfs-cli"
	@echo "Removed binaries from $(INSTALL_BIN)"

endif

# ---------- end install / uninstall ----------

build-lfs: ## Build Git LFS submodule
	@if [ ! -d $(GIT_LFS_DIR) ]; then \
		echo "$(GIT_LFS_DIR) not found"; \
		exit 1; \
	fi
	@$(MAKE) -C $(GIT_LFS_DIR)

build-sdk: ## Build the vendored Proton Drive SDK (client/js) consumed by proton-drive-cli
	@if [ ! -f $(SDK_JS_DIR)/package.json ]; then \
		echo "$(SDK_JS_DIR) not initialized."; \
		echo "Run: git -C $(DRIVE_CLI_DIR) submodule update --init submodules/sdk"; \
		exit 1; \
	fi
	@# client/js ships no npm lockfile; --no-package-lock keeps the SDK checkout
	@# clean for check-submodules.
	cd $(SDK_JS_DIR) && npm install --no-package-lock --no-audit --no-fund && npm run build

# File target so build-drive-cli builds the SDK exactly once when its dist is
# missing (safe under make -j); run `make build-sdk` to rebuild after a bump.
$(SDK_JS_DIR)/dist/index.js:
	@$(MAKE) build-sdk

build-drive-cli: $(SDK_JS_DIR)/dist/index.js ## Build proton-drive-cli TypeScript bridge (builds the vendored SDK first if missing)
	@if [ ! -d $(DRIVE_CLI_DIR) ]; then \
		echo "$(DRIVE_CLI_DIR) not found."; \
		echo "Run the Local Bring-Up submodule commands in docs/operations/deployment.md,"; \
		echo "then run: make check-submodules"; \
		exit 1; \
	fi
	cd $(DRIVE_CLI_DIR) && COREPACK_HOME=$(COREPACK_HOME_DIR) $(YARN) build

test: ## Run unit tests for cmd/... and internal/... (race detector, coverage)
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -race -cover $(GO_TEST_PKGS)

test-all: check-submodules test test-lfs test-integration test-e2e-mock ## Run all test suites

test-adapter: ## Run adapter tests only
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -race -cover ./cmd/adapter/...

test-tray: ## Run tray app tests only (pure Go off macOS; -race needs a C toolchain)
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -race -cover ./cmd/tray/...

test-lfs: ## Run Git LFS submodule tests
	@if [ ! -d $(GIT_LFS_DIR) ]; then \
		echo "$(GIT_LFS_DIR) not found"; \
		exit 1; \
	fi
	@$(MAKE) -C $(GIT_LFS_DIR) test

test-integration: ## Run integration tests (requires git + git-lfs binaries)
	@mkdir -p $(GO_CACHE_DIR)
	GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/...

test-integration-adapter-exit: ## Run integration tests for an adapter that exits mid-transfer without completing
	@mkdir -p $(GO_CACHE_DIR)
	GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransferAdapterExitsMid' -v

test-integration-stress: ## Run high-volume concurrency stress/soak integration tests
	@mkdir -p $(GO_CACHE_DIR)
	GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransferConcurrentStressSoak$$' -v

test-integration-failure-modes: ## Run failure mode integration tests (wrong OID, crash, exit without response)
	@mkdir -p $(GO_CACHE_DIR)
	GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransfer(RejectsWrongOID|FailsWhenAdapter)' -v

test-integration-config-matrix: ## Run direction config matrix integration tests
	@mkdir -p $(GO_CACHE_DIR)
	GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run '^TestGitLFSCustomTransferDirection' -v

test-integration-credentials: ## Run credential flow security tests
	@mkdir -p $(GO_CACHE_DIR)
	GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run Credential -v

test-integration-sdk: check-submodules check-sdk-prereqs ## Run sdk backend integration tests (requires proton-drive-cli and pass-cli)
	@mkdir -p $(GO_CACHE_DIR)
	@$(PASS_ENV) \
		PROTON_LFS_RUN_SDK_INTEGRATION=1 \
		GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run SDK -v

test-e2e-mock: build-adapter ## Mocked E2E pipeline (no real credentials)
	@mkdir -p $(GO_CACHE_DIR)
	PROTON_DRIVE_CLI_BIN=$(CURDIR)/tests/testdata/mock-proton-drive-cli.js \
		GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run E2EMocked -v

live-canary-preflight: check-submodules build-adapter build-drive-cli ## Offline gate before any real Proton canary
	@echo "Running offline live-canary preflight (no Proton login)..."
	@mkdir -p $(GO_CACHE_DIR)
	GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test ./...
	cd $(DRIVE_CLI_DIR) && COREPACK_HOME=$(COREPACK_HOME_DIR) $(YARN) lint && \
		COREPACK_HOME=$(COREPACK_HOME_DIR) $(YARN) jest src/auth/flow-safety.test.ts src/cli/doctor.test.ts --runInBand
	@if [ -n "$${LIVE_CANARY_DOCTOR_ARGS:-}" ]; then \
		echo "Running optional offline doctor with LIVE_CANARY_DOCTOR_ARGS..."; \
		if ! DOCTOR_OUTPUT="$$( $(NODE) "$(CURDIR)/$(DRIVE_CLI_DIR)/dist/index.js" doctor --json $$LIVE_CANARY_DOCTOR_ARGS )"; then \
			echo "$$DOCTOR_OUTPUT"; \
			echo "Offline doctor failed; refusing live canary."; \
			exit 2; \
		fi; \
		echo "$$DOCTOR_OUTPUT"; \
		if ! printf '%s\n' "$$DOCTOR_OUTPUT" | $(GO) run ./scripts/check_doctor_readiness.go --require-live-canary; then \
			echo "Offline doctor did not mark live canary as ready."; \
			exit 2; \
		fi; \
	else \
		echo "Skipped credential-store doctor. Set LIVE_CANARY_DOCTOR_ARGS to run it."; \
	fi
	@echo "Offline live-canary preflight passed."

check-live-canary-ack: ## Refuse real Proton tests without explicit acknowledgement
	@if [ "$${PROTON_LFS_LIVE_CANARY:-}" != "$(LIVE_CANARY_ACK_VALUE)" ]; then \
		echo "Refusing to run a real Proton canary."; \
		echo "This target may touch a real Proton account and must never run by accident."; \
		echo "Read docs/operations/live-canary-runbook.md first."; \
		echo "Then run with:"; \
		echo "  PROTON_LFS_LIVE_CANARY=$(LIVE_CANARY_ACK_VALUE) LIVE_CANARY_DOCTOR_ARGS='--credential-store pass-cli' make test-e2e-real"; \
		exit 2; \
	fi

check-live-canary-doctor-args: ## Require explicit offline doctor args for real Proton tests
	@if [ -z "$${LIVE_CANARY_DOCTOR_ARGS:-}" ]; then \
		echo "Refusing to run a real Proton canary without LIVE_CANARY_DOCTOR_ARGS."; \
		echo "The offline doctor must pass with the exact credential store args"; \
		echo "that will be used by the live canary."; \
		echo "Example:"; \
		echo "  LIVE_CANARY_DOCTOR_ARGS='--credential-store pass-cli'"; \
		exit 2; \
	fi

check-browser-fork-canary-args: ## Require explicit browser-fork login args
	@if [ -z "$${LIVE_BROWSER_FORK_LOGIN_ARGS:-}" ]; then \
		echo "Refusing to run browser-fork canary without LIVE_BROWSER_FORK_LOGIN_ARGS."; \
		echo "This must name the credential store/host for the single"; \
		echo "browser login attempt."; \
		echo "Example:"; \
		echo "  LIVE_BROWSER_FORK_LOGIN_ARGS='--credential-store git-credential'"; \
		exit 2; \
	fi

browser-fork-canary: check-live-canary-ack check-live-canary-doctor-args check-browser-fork-canary-args live-canary-preflight ## One guarded browser-fork login canary; no transfer
	@NODE_BIN="$(NODE)" GO_BIN="$(GO)" DRIVE_CLI_BIN="$(CURDIR)/$(DRIVE_CLI_DIR)/dist/index.js" ./scripts/browser-fork-canary.sh

live-drive-scope-canary: check-live-canary-ack check-live-canary-doctor-args live-canary-preflight ## One guarded read-only Drive metadata canary; no transfer
	@NODE_BIN="$(NODE)" DRIVE_CLI_BIN="$(CURDIR)/$(DRIVE_CLI_DIR)/dist/index.js" ./scripts/live-drive-scope-canary.sh

test-e2e-real: live-drive-scope-canary ## Real Proton Drive E2E (requires explicit live canary acknowledgement)
	@mkdir -p $(GO_CACHE_DIR)
	@$(PASS_ENV) \
		GO="$(GO)" GOCACHE=$(CURDIR)/$(GO_CACHE_DIR) $(GO) test -tags integration ./tests/integration/... -run E2E -v

pass-env: ## Print the PROTON_PASS_CLI_BIN export for the pass-cli credential store
	@./scripts/export-pass-env.sh

check-submodules: ## Verify pinned root and Proton SDK submodule checkouts
	@./scripts/check-submodules.sh

check-sdk-prereqs: ## Verify prerequisites for sdk integration tests
	@command -v git-lfs >/dev/null 2>&1 || (echo "git-lfs not found on PATH" && exit 1)
	@if [ "$(CREDENTIAL_STORE)" = "pass-cli" ]; then \
		command -v "$${PROTON_PASS_CLI_BIN:-pass-cli}" >/dev/null 2>&1 || { echo "pass-cli not found on PATH (or PROTON_PASS_CLI_BIN invalid)"; exit 1; }; \
		./scripts/export-pass-env.sh >/dev/null; \
	fi
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
	@echo "SDK integration prerequisites OK"

fmt: ## Format Go code
	$(GO) fmt ./...

fmt-check: ## Fail if any Go file is not gofmt-formatted
	@unformatted="$$(gofmt -l cmd internal tests scripts)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt required for:"; echo "$$unformatted"; exit 1; \
	fi

lint: lint-go lint-comments ## Run lint checks

lint-comments: ## Enforce the comment policy (CLAUDE.md, "Code Comments and Legacy")
	@bash scripts/check-comment-policy.sh

lint-go: ## Run go vet and golangci-lint (golangci-lint is mandatory when CI=true)
	$(GO) vet ./...
	$(GO) vet -tags integration ./tests/...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	elif [ "$${CI:-}" = "true" ]; then \
		echo "golangci-lint is required in CI but is not installed."; \
		echo "Install: https://golangci-lint.run/docs/welcome/install/"; \
		exit 1; \
	else \
		echo "golangci-lint not installed; skipped (CI fails without it)."; \
		echo "Install: https://golangci-lint.run/docs/welcome/install/"; \
	fi

install-hooks: ## Install pre-commit hooks
	@if ! command -v pre-commit >/dev/null 2>&1; then \
		echo "pre-commit is not installed"; \
		exit 1; \
	fi
	pre-commit install --hook-type pre-commit --hook-type commit-msg

status: ## Print project status
	@echo "Go: $$($(GO) version)"
	@NODE_BIN_RESOLVED="$$(command -v $(NODE) 2>/dev/null || true)"; \
		if [ -n "$$NODE_BIN_RESOLVED" ]; then \
			echo "Node: $$($$NODE_BIN_RESOLVED --version)"; \
		else \
			echo "Node: not found"; \
		fi
	@if command -v $(YARN) >/dev/null 2>&1; then \
		echo "Yarn ($(DRIVE_CLI_DIR)): $$(cd $(DRIVE_CLI_DIR) 2>/dev/null && COREPACK_HOME=$(COREPACK_HOME_DIR) $(YARN) --version 2>/dev/null || echo unavailable)"; \
	else \
		echo "Yarn: not found (run: corepack enable)"; \
	fi
	@echo "Adapter binary: $$([ -f $(ADAPTER_BIN) ] && echo present || echo missing)"

docs: docs-lint ## Validate documentation

docs-lint: ## Lint markdown files
	@echo "Linting markdown files..."
	@if ! command -v npx >/dev/null 2>&1; then \
		echo "Error: npx not found. Install Node.js first."; \
		exit 1; \
	fi
	@npx --yes markdownlint-cli2@$(MARKDOWNLINT_CLI2_VERSION) \
		--config .markdownlint.json \
		"README.md" "USAGE.md" "SECURITY.md" "docs/**/*.md"

clean: ## Remove generated files (project-local only; never the global Go caches)
	rm -rf bin build dist
	rm -rf $(GO_CACHE_DIR)
