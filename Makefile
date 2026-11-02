APP       := discoxip
BUILD_DIR := build
BIN       := $(BUILD_DIR)/$(APP)
SRC       ?= sample/5659
OUT       ?= $(BUILD_DIR)/dev
XBE       ?= $(firstword $(wildcard $(SRC)/*.xbe))
BIOS      ?= kernels/5713_orig.bin
MCPX      ?= kernels/mcpx-1.1.bin
NATIVE_SETS ?= 5659=sample/5659:kernels/5713_orig.bin:kernels/mcpx-1.1.bin:references/xdk/5659 4304=sample/4304:kernels/4034_orig.bin:kernels/mcpx-1.0.bin:references/xdk/4627
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS   := -ldflags "-X github.com/nickheyer/discoxip/pkg/cli.Version=$(VERSION) -X github.com/nickheyer/discoxip/pkg/cli.Commit=$(COMMIT)"

.PHONY: all build dev pipeline decompile native-c test test-samples vet fmt clean

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BIN) ./cmd/discoxip

dev: pipeline decompile

pipeline: build
	rm -rf $(OUT)
	$(BIN) build $(SRC) -o $(OUT) --bios $(BIOS) --mcpx $(MCPX)

decompile: build
	$(BIN) xbe disasm $(XBE) -o $(OUT)/$(notdir $(basename $(XBE))).c

native-c: build
	python3 -B references/scripts/gen_native_c.py --bin $(BIN) --cache $(BUILD_DIR)/native --out NATIVE_C.md $(NATIVE_SETS)

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
