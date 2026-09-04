GO ?= $(firstword $(wildcard $(CURDIR)/.tools/go/bin/go $(CURDIR)/../.tools/go/bin/go) go)
GOFMT ?= $(shell $(GO) env GOROOT)/bin/gofmt
BINARY ?= bin/devbox-neo
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS ?= -X devbox/internal/cli.Version=$(VERSION)

.PHONY: install-go fmt test test-race test-integration build check tidy clean
install-go:
	bash scripts/install-go.sh
fmt:
	$(GOFMT) -w $$(find cmd internal -name '*.go' -type f)
test:
	$(GO) test -timeout=2m ./...
test-race:
	$(GO) test -race -timeout=5m ./...
test-integration:
	DEVBOX_DOCKER_TEST=1 $(GO) test -tags integration -count=1 -timeout=20m ./...
build:
	mkdir -p $(dir $(BINARY))
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/devbox
check: fmt test test-race build
tidy:
	$(GO) mod tidy
clean:
	rm -rf bin
