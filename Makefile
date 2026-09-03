# prompton-cli
#
# make            build the binary into ./prompton
# make test       run the whole test suite
# make check      fmt + vet + test, i.e. what CI must see pass

BINARY  := prompton
MODULE  := github.com/polimo-dev/prompton-cli
# main-latest is the rolling pre-release tag, not a version; describe past it.
VERSION ?= $(shell git describe --tags --exclude=main-latest --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/meta.Version=$(VERSION)

GO ?= go

.PHONY: all
all: build

.PHONY: build
build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) .

.PHONY: install
install:
	$(GO) install -trimpath -ldflags '$(LDFLAGS)' .

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race ./...

.PHONY: cover
cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

# Fails when anything is unformatted, which is what CI wants.
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: tidy
tidy:
	$(GO) mod tidy

# install.sh: syntax, then its release-resolution tests (plain sh, no network).
.PHONY: test-install
test-install:
	sh -n install.sh
	sh install_test.sh
	sh -n uninstall.sh
	sh uninstall_test.sh

.PHONY: check
check: fmt-check vet test test-install

.PHONY: snapshot
snapshot:
	goreleaser release --snapshot --clean

.PHONY: release-check
release-check:
	goreleaser check
	shellcheck install.sh install_test.sh

.PHONY: clean
clean:
	rm -rf dist coverage.out coverage.html $(BINARY)
