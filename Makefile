# get the latest commit hash in the short form
COMMIT := $(shell git rev-parse --short HEAD)
# get the latest commit date in the form of YYYYmmdd
DATE := $(shell git log -1 --format=%cd --date=format:"%Y%m%d")

VERSION := $(COMMIT)-$(DATE)
FLAGS := -ldflags "-w -s -X main.version=$(VERSION)"

.PHONY: build
build:
	@echo "Building..."
	go build $(FLAGS) -trimpath -o mcp-custom-prompts main.go

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
