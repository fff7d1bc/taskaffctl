APP := taskaffctl
BUILD_DIR := $(CURDIR)/build
GOTOOLCHAIN ?= auto
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
PLATFORM := $(GOOS)-$(GOARCH)
PLATFORM_BUILD_DIR := $(BUILD_DIR)/$(PLATFORM)
BIN_DIR := $(PLATFORM_BUILD_DIR)/bin
BIN := $(BIN_DIR)/$(APP)
STATIC_BIN := $(BIN_DIR)/$(APP)-static
GO_SOURCES := $(wildcard *.go)
GO_MODULE_FILES := go.mod $(wildcard go.sum)
GOCACHE := $(PLATFORM_BUILD_DIR)/gocache
GOMODCACHE := $(PLATFORM_BUILD_DIR)/gomodcache
GOPATH := $(PLATFORM_BUILD_DIR)/gopath
GOTMPDIR := $(PLATFORM_BUILD_DIR)/tmp
GOTELEMETRYDIR := $(PLATFORM_BUILD_DIR)/telemetry
GOENV := off
GOFLAGS := -modcacherw -buildvcs=false
STATIC_TAGS ?= netgo osusergo
STATIC_LDFLAGS ?= -s -w -buildid=

export GOOS
export GOARCH
export GOCACHE
export GOMODCACHE
export GOPATH
export GOTMPDIR
export GOTELEMETRYDIR
export GOENV
export GOFLAGS
export GOTOOLCHAIN
export GOTELEMETRY=off

.PHONY: all build static test install clean

all: build

build: $(BIN)

static: $(STATIC_BIN)

test:
	mkdir -p "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(GOTMPDIR)" "$(GOTELEMETRYDIR)"
	go test ./...

$(BIN): $(GO_MODULE_FILES) $(GO_SOURCES)
	mkdir -p "$(BIN_DIR)" "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(GOTMPDIR)" "$(GOTELEMETRYDIR)"
	go build -trimpath -o "$(BIN)" .

$(STATIC_BIN): $(GO_MODULE_FILES) $(GO_SOURCES)
	mkdir -p "$(BIN_DIR)" "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(GOTMPDIR)" "$(GOTELEMETRYDIR)"
	CGO_ENABLED=0 go build -trimpath -tags "$(STATIC_TAGS)" -ldflags "$(STATIC_LDFLAGS)" -o "$(STATIC_BIN)" .
	@echo "static binary: $(STATIC_BIN)"

install: build
	if [ "$$(id -u)" -eq 0 ]; then \
		mkdir -p /usr/local/bin; \
		install -m 0755 "$(BIN)" "/usr/local/bin/$(APP)"; \
	else \
		mkdir -p "$$HOME/.local/bin"; \
		install -m 0755 "$(BIN)" "$$HOME/.local/bin/$(APP)"; \
	fi

clean:
	chmod -R u+w "$(BUILD_DIR)" 2>/dev/null || true
	rm -rf "$(BUILD_DIR)"
