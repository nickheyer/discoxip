APP_NAME := discoxip
BUILD_DIR := build
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -ldflags "-X github.com/nickheyer/discoxip/pkg/cli.Version=$(VERSION) \
                      -X github.com/nickheyer/discoxip/pkg/cli.Commit=$(COMMIT)"
GO_RUN := go run $(LDFLAGS) ./cmd/discoxip

.PHONY: all build clean test fmt serve extract

all: build

build: clean
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/discoxip

clean:
	rm -rf $(BUILD_DIR)

test:
	go test ./...

fmt:
	go fmt ./...

extract:
	$(GO_RUN) build $(SRC) -o $(BUILD_DIR)/$(SRC)

serve: extract
	cd $(BUILD_DIR)/$(SRC)/web && python3 -m http.server
