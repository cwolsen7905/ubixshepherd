# Build and check uBixShepherd. CI runs the same targets.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/ubixsys/ubixshepherd/internal/version.Version=$(VERSION)
TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# Core code must not name a product: product knowledge lives in packs (design.md §3.12).
CORE_DIRS := cmd internal
PRODUCT_PATTERN := ubixcore|ubixvault|ubixops|replikate|ubixos

PREFIX ?= $(HOME)/.local/bin

.PHONY: build install test check core-boundary cross clean

build:
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o bin/shepherd ./cmd/shepherd

# Copy the binary to a stable path (the one `shepherd daemon install` should register),
# replacing it atomically, and restart the daemon if one is running so it runs the new
# build.
install: build
	@mkdir -p $(PREFIX)
	@cp bin/shepherd $(PREFIX)/.shepherd.new && mv -f $(PREFIX)/.shepherd.new $(PREFIX)/shepherd
	@echo "installed $(PREFIX)/shepherd ($(VERSION))"
	@if SHEPHERD_NO_AUTOSTART=1 $(PREFIX)/shepherd daemon status | grep -q 'running, pid'; then \
		$(PREFIX)/shepherd daemon restart; \
	fi

test:
	go test ./...

check: core-boundary
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test ./...

core-boundary:
	@if grep -rniE '$(PRODUCT_PATTERN)' $(CORE_DIRS); then \
		echo "Core code names a product; move it into a pack."; exit 1; \
	fi
	@echo "Core is product-free."

cross:
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' \
			-o dist/shepherd-$$os-$$arch$$ext ./cmd/shepherd || exit 1; \
	done

clean:
	rm -rf bin dist
