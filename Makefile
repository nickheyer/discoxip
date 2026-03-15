APP_NAME := discoxip
BUILD_DIR := build
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -ldflags "-X github.com/nickheyer/discoxip/pkg/cli.Version=$(VERSION) \
                      -X github.com/nickheyer/discoxip/pkg/cli.Commit=$(COMMIT)"

.PHONY: all build clean test fmt

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/discoxip

clean:
	rm -rf $(BUILD_DIR)

test:
	go test ./...

fmt:
	go fmt ./...
