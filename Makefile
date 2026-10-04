GO               ?= go
BIN_DIR          := bin
GOLANGCI         := $(BIN_DIR)/golangci-lint
GOLANGCI_VERSION := v2.12.2

.DEFAULT_GOAL := help

.PHONY: help build test lint fmt tidy docs sync-semconv drift py-test py-lint demo-conformance clean

# Every apps/*/ directory with a pyproject.toml is a uv-managed Python app.
PY_APPS := $(patsubst %/pyproject.toml,%,$(wildcard apps/*/pyproject.toml))

help: ## Show available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## Build the genai-conformance binary into bin/
	$(GO) build -o $(BIN_DIR)/genai-conformance ./cmd/genai-conformance

test: ## Run Go tests with race detector and coverage
	$(GO) test -race -cover ./...

lint: $(GOLANGCI) ## Run golangci-lint
	$(GOLANGCI) run

fmt: ## Format Go code
	$(GO) fmt ./...

tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

py-test: ## Run the Python demo apps' test suites
	@for d in $(PY_APPS); do \
		echo "==> $$d"; \
		(cd $$d && uv sync --all-groups --quiet && uv run pytest) || exit 1; \
	done

py-lint: ## Ruff-lint the Python demo apps
	@for d in $(PY_APPS); do \
		echo "==> $$d"; \
		(cd $$d && uv run ruff check . && uv run ruff format --check .) || exit 1; \
	done

demo-conformance: ## Cross-language gate: demo telemetry validated by the Go engine
	hack/check-demo-conformance.sh

docs: ## Regenerate generated documentation (docs/rules.md)
	$(GO) run ./cmd/genai-conformance rules list --format markdown > docs/rules.md

sync-semconv: ## Re-vendor the pinned GenAI semconv model (hack/sync-semconv.sh [sha])
	hack/sync-semconv.sh

drift: ## Report drift between the pinned semconv model and upstream main
	hack/check-drift.sh

$(GOLANGCI):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(BIN_DIR) $(GOLANGCI_VERSION)

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) out
