GO_IMAGE ?= golang:1.26-bookworm
VERSION ?= 0.3.5
GOOS ?= linux
GOARCH ?= amd64

ifeq ($(GOOS),darwin)
PLUGIN_EXT := dylib
else ifeq ($(GOOS),windows)
PLUGIN_EXT := dll
else
PLUGIN_EXT := so
endif

PLUGIN_DIR := build/plugins/$(GOOS)/$(GOARCH)
PLUGIN_LIBRARY := $(PLUGIN_DIR)/cliproxyapi-copilot.$(PLUGIN_EXT)
CACHE_DIR := .cache
VERSION_LDFLAG := -X github.com/arthur-sommer-etc/cliproxyapi-copilot-plugin/internal/provider.PluginVersion=$(VERSION)

.PHONY: test build build-local package package-local clean

test:
	go test ./...

build:
	mkdir -p $(PLUGIN_DIR) $(CACHE_DIR)/go-build $(CACHE_DIR)/go-mod $(CACHE_DIR)/home
	docker run --rm \
		--user "$$(id -u):$$(id -g)" \
		-e HOME=/src/$(CACHE_DIR)/home \
		-e GOCACHE=/src/$(CACHE_DIR)/go-build \
		-e GOMODCACHE=/src/$(CACHE_DIR)/go-mod \
		-v "$(CURDIR):/src" \
		-w /src \
		$(GO_IMAGE) \
		sh -ec 'CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -buildvcs=false -trimpath -ldflags "$(VERSION_LDFLAG)" -buildmode=c-shared -o $(PLUGIN_LIBRARY) ./cmd/cliproxyapi-copilot'

build-local:
	mkdir -p $(PLUGIN_DIR)
	CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -buildvcs=false -trimpath -ldflags "$(VERSION_LDFLAG)" -buildmode=c-shared -o $(PLUGIN_LIBRARY) ./cmd/cliproxyapi-copilot

package: build
	scripts/package-release.sh "$(VERSION)" "$(GOOS)" "$(GOARCH)"

package-local: build-local
	scripts/package-release.sh "$(VERSION)" "$(GOOS)" "$(GOARCH)"

clean:
	rm -rf build dist $(CACHE_DIR)
