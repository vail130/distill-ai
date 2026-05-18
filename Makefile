# distill-ai build orchestration.
#
# Targets exist for the contributor workflow described in CONTRIBUTING.md.
# Keep this file POSIX-make compatible; no GNU-only constructs.

BINARY      := distill-ai
PKG         := github.com/vail130/distill-ai
CMD         := ./cmd/$(BINARY)
BIN_DIR     := ./bin
OUT         := $(BIN_DIR)/$(BINARY)

# Skill install location for opencode. Override on the command line to
# install elsewhere: `make install-skill SKILL_DEST=/path/to/skill`.
SKILL_SRC   := $(CURDIR)/skills/distill-ai
SKILL_DEST  ?= $(HOME)/.config/opencode/skills/distill-ai

# Version metadata: prefer git tag, fall back to short SHA + dev marker.
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

GO         ?= go
GOFLAGS    ?=
TESTFLAGS  ?= -race -timeout=60s

.PHONY: all
all: build

.PHONY: build
build: ## Build the binary into ./bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(OUT) $(CMD)

.PHONY: install
install: ## Install into $GOBIN / $GOPATH/bin
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD)

.PHONY: install-skill
install-skill: ## Symlink the consumer skill into $$HOME/.config/opencode/skills/
	@if [ ! -d "$(SKILL_SRC)" ]; then \
		echo "error: source skill not found at $(SKILL_SRC)" >&2; exit 1; \
	fi
	@mkdir -p "$$(dirname "$(SKILL_DEST)")"
	@if [ -L "$(SKILL_DEST)" ]; then \
		existing=$$(readlink "$(SKILL_DEST)"); \
		if [ "$$existing" = "$(SKILL_SRC)" ]; then \
			echo "skill already linked: $(SKILL_DEST) -> $(SKILL_SRC)"; \
		else \
			echo "replacing symlink: $(SKILL_DEST) (was -> $$existing)"; \
			rm "$(SKILL_DEST)" && ln -s "$(SKILL_SRC)" "$(SKILL_DEST)"; \
			echo "linked: $(SKILL_DEST) -> $(SKILL_SRC)"; \
		fi; \
	elif [ -e "$(SKILL_DEST)" ]; then \
		echo "error: $(SKILL_DEST) exists and is not a symlink; refusing to overwrite" >&2; exit 1; \
	else \
		ln -s "$(SKILL_SRC)" "$(SKILL_DEST)"; \
		echo "linked: $(SKILL_DEST) -> $(SKILL_SRC)"; \
	fi

.PHONY: uninstall-skill
uninstall-skill: ## Remove the symlink installed by install-skill
	@if [ -L "$(SKILL_DEST)" ]; then \
		rm "$(SKILL_DEST)" && echo "removed: $(SKILL_DEST)"; \
	elif [ -e "$(SKILL_DEST)" ]; then \
		echo "error: $(SKILL_DEST) is not a symlink; refusing to remove" >&2; exit 1; \
	else \
		echo "nothing to remove at $(SKILL_DEST)"; \
	fi

.PHONY: test
test: ## Run all tests with race detector
	$(GO) test $(TESTFLAGS) ./...

.PHONY: test-integration
test-integration: ## Run only the integration suite (forks the compiled binary)
	$(GO) test $(TESTFLAGS) ./test/integration/...

.PHONY: test-update
test-update: ## Refresh golden-file fixtures
	$(GO) test $(TESTFLAGS) ./... -update

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test -run=^$$ -bench=. -benchmem ./...

.PHONY: lint
lint: ## Run golangci-lint
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not installed; see CONTRIBUTING.md" >&2; exit 1; \
	}
	golangci-lint run

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format Go sources
	$(GO) fmt ./...

.PHONY: tidy
tidy: ## Sync go.mod / go.sum
	$(GO) mod tidy

.PHONY: check
check: vet lint test ## Run vet, lint, and tests

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf $(BIN_DIR) dist

.PHONY: release-dry-run
release-dry-run: ## Build all release artefacts locally without publishing
	@command -v goreleaser >/dev/null 2>&1 || { \
		echo "goreleaser not installed; see https://goreleaser.com/install/" >&2; exit 1; \
	}
	goreleaser release --snapshot --clean --skip=publish

.PHONY: help
help: ## Print this help
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} \
		/^[a-zA-Z_-]+:.*##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' \
		$(MAKEFILE_LIST)
