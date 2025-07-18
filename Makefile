# Build information
VERSION := $(shell git describe --tags --dirty --always 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GO_VERSION := $(shell go version | cut -d' ' -f3)

LDFLAGS := -ldflags "-w -s \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.goVersion=$(GO_VERSION)"

.PHONY: lint
lint:
	@echo "Running linter..."
	golangci-lint run ./...

.PHONY: test
test:
	@echo "Running tests and checking coverage..."
	go test -race -cover -coverprofile="coverage.out" -covermode=atomic ./...
	@real_coverage=$$(go tool cover -func=coverage.out | grep total | awk '{print substr($$3, 1, length($$3)-1)}'); \
	min_coverage=$$(cat min-coverage); \
	if (( $$(echo "$$real_coverage < $$min_coverage" | bc -l) )); then \
		echo "Coverage check failed: $$real_coverage% is lower than the required $$min_coverage%"; \
		exit 1; \
	else \
		echo "Coverage check passed: $$real_coverage% meets the minimum requirement of $$min_coverage%"; \
	fi

.PHONY: build
build:
	@echo "Building..."
	go build $(LDFLAGS) -trimpath -o mcp-prompt-engine .

.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t mcp-prompt-engine .

.PHONY: docker-run
docker-run:
	@echo "Running MCP server with mounted prompts and logs directories..."
	docker run -i --rm -v "$(PWD)/prompts:/app/prompts:ro" -v "$(PWD)/logs:/app/logs" mcp-prompt-engine

.PHONY: release-dry-run
release-dry-run:
	@echo "Running goreleaser in dry-run mode..."
	GO_VERSION=$(GO_VERSION) goreleaser release --snapshot --clean --skip=publish

.PHONY: release-local
release-local:
	@echo "Building release locally..."
	goreleaser build --snapshot --clean
