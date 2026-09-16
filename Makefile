GO ?= $(firstword $(wildcard $(CURDIR)/.tools/go/bin/go $(CURDIR)/../.tools/go/bin/go) go)
GOFMT ?= $(shell $(GO) env GOROOT)/bin/gofmt
BINARY ?= bin/devbox-neo
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS ?= -X devbox/internal/cli.Version=$(VERSION)
DOCS_IMAGE := zensical/zensical:0.0.62@sha256:162b7e191224f57b8c584debe51b157b9802efd25d3a8948e4e0f64c1baaaee6

.PHONY: install-go fmt test test-race test-integration build build-migrate docs-build docs-serve check tidy clean
install-go:
	bash scripts/install-go.sh
fmt:
	$(GOFMT) -w $$(find cmd internal -name '*.go' -type f)
test:
	$(GO) test -timeout=2m ./...
test-race:
	$(GO) test -race -timeout=5m ./...
test-integration:
	DEVBOX_DOCKER_TEST=1 $(GO) test -tags integration -count=1 -timeout=60m ./...
build:
	mkdir -p $(dir $(BINARY))
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/devbox
build-migrate:
	mkdir -p bin
	$(GO) build -o bin/devbox-migrate ./cmd/devbox-migrate
docs-build:
	docker run --rm --user "$$(id -u):$$(id -g)" -v "$(CURDIR):/docs" $(DOCS_IMAGE) build
docs-serve:
	docker run --rm --user "$$(id -u):$$(id -g)" -v "$(CURDIR):/docs" \
		-p 127.0.0.1:3000:8000 $(DOCS_IMAGE) serve --dev-addr 0.0.0.0:8000
check: fmt test test-race build
tidy:
	$(GO) mod tidy
clean:
	rm -rf bin
