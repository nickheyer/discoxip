APP       := discoxip
BUILD_DIR := build
BIN       := $(BUILD_DIR)/$(APP)
SRC       ?= sample/5659
OUT       ?= $(BUILD_DIR)/dev
PORT      ?= 8000
XBE       ?= $(firstword $(wildcard $(SRC)/*.xbe))
BIOS      ?= kernels/5713_orig.bin
MCPX      ?= kernels/mcpx-1.1.bin
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS   := -ldflags "-X github.com/nickheyer/discoxip/pkg/cli.Version=$(VERSION) -X github.com/nickheyer/discoxip/pkg/cli.Commit=$(COMMIT)"

.PHONY: all build dev pipeline decompile serve test test-samples vet fmt clean

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BIN) ./cmd/discoxip

dev: pipeline decompile serve

pipeline: build
	rm -rf $(OUT)
	$(BIN) build $(SRC) -o $(OUT) --bios $(BIOS) --mcpx $(MCPX)

decompile: build
	$(BIN) xbe disasm $(XBE) -o $(OUT)/$(notdir $(basename $(XBE))).c

serve:
	cd $(OUT)/web && python3 -m http.server $(PORT)

test:
	go test ./...

test-samples:
	go test -tags samples -timeout 30m -run Samples ./pkg/cli/

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -rf $(BUILD_DIR)
