# ==========================================
# GRON - Makefile
# ==========================================

BINARY_NAME := gron
GO := go
GOFLAGS := -mod=readonly
COVER_PROFILE := coverage.out
LINTER := golangci-lint

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help message
	@echo "🔧 Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the project
	$(GO) build $(GOFLAGS) ./...

.PHONY: test
test: ## Run tests with race detector
	$(GO) test -race ./...

.PHONY: test-verbose
test-verbose: ## Run tests with verbose output & race detector
	$(GO) test -v -race ./...

.PHONY: cover
cover: ## Run tests, generate coverage report & open HTML
	$(GO) test -race -coverprofile=$(COVER_PROFILE) -covermode=atomic ./...
	@echo "📊 Coverage report generated: $(COVER_PROFILE)"
	@$(GO) tool cover -html=$(COVER_PROFILE) -o coverage.html
	@echo "🌐 Coverage HTML generated: coverage.html"

.PHONY: vet
vet: ## Run go vet for static analysis
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format code with gofmt
	$(GO) fmt ./...

.PHONY: lint
lint: ## Run golangci-lint (falls back to go vet if not installed)
	@if command -v $(LINTER) > /dev/null; then \
		$(LINTER) run --timeout=3m; \
	else \
		echo "⚠️  $(LINTER) not found. Running 'go vet' instead."; \
		$(GO) vet ./...; \
	fi

.PHONY: tidy
tidy: ## Clean and verify go.mod dependencies
	$(GO) mod tidy && $(GO) mod verify

.PHONY: clean
clean: ## Remove build artifacts and coverage files
	rm -f $(COVER_PROFILE) coverage.html
	rm -rf dist/
	@echo "🧹 Cleaned up build artifacts."

.PHONY: ci
ci: fmt vet lint test ## Run full CI pipeline locally

.PHONY: precommit
precommit: tidy fmt vet test ## Prepare code for commit

# Cross-compilation (optional)
.PHONY: cross-build
cross-build: ## Build for linux/amd64, darwin/arm64, windows/amd64
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o dist/$(BINARY_NAME)-linux-amd64 ./...
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o dist/$(BINARY_NAME)-darwin-arm64 ./...
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o dist/$(BINARY_NAME)-windows-amd64.exe ./...
	@echo "📦 Cross-build complete in dist/"

.PHONY: changelog
changelog: ## Preview unreleased changelog entries
	@echo "📋 Unreleased changes:"
	@grep -A 100 "## \[Unreleased\]" CHANGELOG.md | grep -B 100 "## \[" | head -n -1

.PHONY: release
release: ## Prepare release: update changelog, commit, tag (set VERSION=v1.0.0)
ifndef VERSION
	$(error VERSION is not set. Usage: make release VERSION=v1.0.0)
endif
	@echo "🔄 Preparing release $(VERSION)..."
	@sed -i.bak "s/## \[Unreleased\]/## [$(VERSION)] - $$(date +'%Y-%m-%d')/" CHANGELOG.md
	@printf "\n## [Unreleased]\n### Added\n### Changed\n### Fixed\n" >> CHANGELOG.md
	@rm -f CHANGELOG.md.bak
	@git add CHANGELOG.md
	@git commit -m "chore: finalize $(VERSION) release" || true
	@git tag -a $(VERSION) -m "Release $(VERSION)"
	@echo "✅ Release $(VERSION) ready. Run 'git push origin main --tags' to publish."