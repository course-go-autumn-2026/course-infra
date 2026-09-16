SHELL := /bin/sh

# Embedded release builds share package-local staging paths; never run recipes concurrently.
.NOTPARALLEL:

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILT_AT ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT ?= build/tools/golangci-lint
BUF_VERSION ?= v1.50.0
PROTOC_GEN_GO_VERSION ?= v1.36.1
PROTOC_GEN_GO_GRPC_VERSION ?= v1.5.1
INSTALL_DIR ?=

BUILDINFO_PACKAGE := github.com/course-go-autumn-2026/tripgo-infra/internal/buildinfo
LDFLAGS := -s -w -X $(BUILDINFO_PACKAGE).version=$(VERSION) -X $(BUILDINFO_PACKAGE).commit=$(COMMIT) -X $(BUILDINFO_PACKAGE).builtAt=$(BUILT_AT)

.PHONY: all build install build-tripgoctl build-tripgoctl-embedded build-push-service cross-build container-build \
        test test-race vet fmt fmt-check lint lint-install proto-tools-install \
        proto-generate proto-check contract-sync contract-check fixture-tests installer-test \
        staging-check source-check verify integration-lifecycle integration-release-runtime \
        integration-isolation release release-repro-check release-audit release-platform-check clean

all: build

build: build-tripgoctl build-push-service

install: build-tripgoctl-embedded
	@set -eu; \
	dir='$(INSTALL_DIR)'; \
	if [ -z "$$dir" ]; then \
		if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then dir=/usr/local/bin; \
		else dir="$$HOME/.local/bin"; fi; \
	fi; \
	mkdir -p "$$dir"; \
	if [ ! -w "$$dir" ]; then \
		echo "installation directory is not writable: $$dir" >&2; \
		echo "set INSTALL_DIR to a writable directory" >&2; \
		exit 1; \
	fi; \
	tmp=$$(mktemp "$$dir/.tripgoctl.new.XXXXXX"); \
	trap 'rm -f "$$tmp"' EXIT HUP INT TERM; \
	install -m 0755 build/bin/tripgoctl-embedded "$$tmp"; \
	mv -f "$$tmp" "$$dir/tripgoctl"; \
	trap - EXIT HUP INT TERM; \
	echo "Installed locally built tripgoctl to $$dir/tripgoctl"; \
	case ":$$PATH:" in *":$$dir:"*) ;; *) \
		echo "Add the installation directory to PATH:"; \
		echo "  export PATH=\"$$dir:\$$PATH\""; \
	esac

build-tripgoctl:
	@mkdir -p build/bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o build/bin/tripgoctl ./cmd/tripgoctl

build-tripgoctl-embedded: proto-check
	@set -eu; \
	arch=$$(go env GOARCH); push_generated=internal/pushartifact/generated; contract_generated=internal/contractasset/generated; \
	case "$$arch" in amd64|arm64) ;; *) echo "unsupported local architecture: $$arch" >&2; exit 1;; esac; \
	rm -rf $$push_generated $$contract_generated; mkdir -p $$push_generated build/bin; \
	trap 'rm -rf $$push_generated $$contract_generated' EXIT HUP INT TERM; \
	./scripts/stage-contract-assets; \
	CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$push_generated/push-service ./cmd/push-service; \
	CGO_ENABLED=0 go build -tags embedded_push,embedded_contracts -trimpath -ldflags "$(LDFLAGS)" -o build/bin/tripgoctl-embedded ./cmd/tripgoctl

build-push-service:
	@mkdir -p build/bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o build/bin/push-service ./cmd/push-service

cross-build: proto-check
	@set -eu; \
	push_generated=internal/pushartifact/generated; contract_generated=internal/contractasset/generated; \
	rm -rf $$push_generated $$contract_generated; mkdir -p $$push_generated build/cross; \
	trap 'rm -rf $$push_generated $$contract_generated' EXIT HUP INT TERM; \
	./scripts/stage-contract-assets; \
	go test -tags embedded_contracts ./internal/contractasset; \
	for arch in amd64 arm64; do \
		push_output=build/cross/push-service-linux-$${arch}; \
		echo "building $$push_output"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$push_output ./cmd/push-service; \
		cp $$push_output $$push_generated/push-service; \
		EXPECTED_PUSH_ARCH=$$arch go test -tags embedded_push ./internal/pushartifact; \
		for os in darwin linux; do \
			cli_output=build/cross/tripgoctl-$${os}-$${arch}; \
			echo "building $$cli_output with embedded linux/$$arch Push Service"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -tags embedded_push,embedded_contracts -trimpath -ldflags "$(LDFLAGS)" -o $$cli_output ./cmd/tripgoctl; \
		done; \
	done; \
	./scripts/check-embedded-push

container-build: cross-build
	@set -eu; \
	for arch in amd64 arm64; do \
		echo "building embedded Push Service runtime image for linux/$$arch"; \
		context=build/container/$$arch-context; \
		rm -rf $$context build/container/$$arch-rootfs; \
		mkdir -p $$context; \
		cp internal/pushartifact/assets/Dockerfile $$context/Dockerfile; \
		cp build/cross/push-service-linux-$$arch $$context/push-service; \
		docker buildx build \
			--platform linux/$$arch \
			--output type=local,dest=build/container/$$arch-rootfs \
			$$context; \
	done

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find cmd internal templates -name '*.go' -type f)

fmt-check:
	@test -z "$$(gofmt -l $$(find cmd internal templates -name '*.go' -type f))" || \
		(echo "Go files require gofmt; run: make fmt" >&2; exit 1)

lint-install:
	@mkdir -p build/tools
	GOBIN=$(CURDIR)/build/tools go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint:
	@test -x "$(GOLANGCI_LINT)" || \
		(echo "golangci-lint is missing; run: make lint-install" >&2; exit 1)
	$(GOLANGCI_LINT) run

proto-tools-install:
	@mkdir -p build/tools
	GOBIN=$(CURDIR)/build/tools go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	GOBIN=$(CURDIR)/build/tools go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN=$(CURDIR)/build/tools go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

proto-generate:
	./scripts/generate-proto

proto-check: contract-check
	./scripts/check-generated-proto

contract-sync:
	./scripts/sync-contracts sync

contract-check:
	./scripts/sync-contracts check

fixture-tests:
	./scripts/test-proto-generate
	./scripts/test-release-provenance
	./scripts/test-release-failure-cleanup
	./scripts/test-install-tripgoctl
	./scripts/test-publish-release

installer-test:
	./scripts/test-install-tripgoctl

staging-check:
	@test ! -e internal/pushartifact/generated
	@test ! -e internal/contractasset/generated

# Safe source gate: no Docker daemon, kind cluster, or release-clean worktree required.
source-check: fmt-check vet test-race fixture-tests proto-check contract-check staging-check

# Full local gate additionally requires the pinned linter and a running Docker daemon.
verify: source-check lint cross-build container-build

# Destructive integration gates require no existing tripgo-local cluster or registry.
integration-lifecycle: build-tripgoctl
	./scripts/integration-lifecycle

integration-release-runtime: build-tripgoctl-embedded
	./scripts/integration-release-runtime

integration-isolation: build-tripgoctl-embedded
	./scripts/integration-isolation

# SOURCE_DATE_EPOCH is part of the release input. Its default is stable so repeated
# local builds are byte-for-byte identical; production invocations should use the
# source commit timestamp.
SOURCE_DATE_EPOCH ?= 0
release:
	VERSION='$(VERSION)' COMMIT='$(COMMIT)' SOURCE_DATE_EPOCH='$(SOURCE_DATE_EPOCH)' ./scripts/run-release

release-repro-check:
	@set -eu; \
	tmp=$$(mktemp -d "$${TMPDIR:-/tmp}/tripgo-release-repro.XXXXXX"); \
	trap 'rm -rf "$$tmp"' EXIT HUP INT TERM; \
	$(MAKE) release VERSION='$(VERSION)' COMMIT='$(COMMIT)' SOURCE_DATE_EPOCH='$(SOURCE_DATE_EPOCH)'; \
	cp build/release/SHA256SUMS build/release/RELEASE_NOTES.md "$$tmp"/; \
	$(MAKE) release VERSION='$(VERSION)' COMMIT='$(COMMIT)' SOURCE_DATE_EPOCH='$(SOURCE_DATE_EPOCH)'; \
	cmp "$$tmp/SHA256SUMS" build/release/SHA256SUMS; \
	cmp "$$tmp/RELEASE_NOTES.md" build/release/RELEASE_NOTES.md; \
	echo 'Release rebuild is byte-for-byte reproducible'

release-audit:
	./scripts/audit-release-cleanup

release-platform-check:
	./scripts/check-release-platform

clean:
	rm -rf build internal/pushartifact/generated internal/contractasset/generated
