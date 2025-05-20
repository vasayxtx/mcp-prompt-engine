# get the latest commit hash in the short form
COMMIT := $(shell git rev-parse --short HEAD)
# get the latest commit date in the form of YYYYmmdd
DATE := $(shell git log -1 --format=%cd --date=format:"%Y%m%d")

VERSION := $(COMMIT)-$(DATE)
FLAGS := -ldflags "-w -s -X main.version=$(VERSION)"

.PHONY: build
build:
	go build $(FLAGS) -trimpath -o mcp-custom-prompts main.go
