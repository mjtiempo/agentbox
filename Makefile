# agentbox build targets.
# F0 owns this file through Wave 1; later waves change it only via their owner
# (C4, D5, E5). Image-target gaps are reported, never edited by other tasks.

GO      ?= go
BIN     := agentbox
VERSION ?= dev
LDFLAGS := -X main.version=$(VERSION)

DOCKER  ?= docker

# Harness versions are pinned here, in the Dockerfiles, and in
# internal/adapter/builtins.go. Image tags follow FR-10:
# agentbox-<harness>:<agentboxVer>-<harness><harnessVer>
PI_VERSION       ?= 1.1.0
CLAUDE_VERSION   ?= 2.1.296
OPENCODE_VERSION ?= 2.0.26

.PHONY: all build vet test test-isolation image image-base image-pi image-claude image-opencode image-all release-check fmt clean

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/agentbox

vet:
	$(GO) vet ./...

# Unit + golden tests. No container engine required.
test:
	$(GO) test ./internal/... ./tests/cli/...

# Isolation acceptance tests: real containers. Skips with a clear message when
# no engine is reachable; CI sets AGENTBOX_REQUIRE_ENGINE=1 to fail instead.
test-isolation: build
	AGENTBOX_REQUIRE_ENGINE=$${AGENTBOX_REQUIRE_ENGINE:-0} bash tests/isolation/run-all.sh

# Build adapter images locally from the vendored Dockerfiles (dev path).
# Build context is the repo root so Dockerfiles COPY the embedded entrypoint.
image: image-base image-pi

image-base:
	$(DOCKER) build -f images/base/Dockerfile -t agentbox-base:$(VERSION) .

image-pi: image-base
	$(DOCKER) build -f images/pi/Dockerfile --build-arg BASE_IMAGE=agentbox-base:$(VERSION) --build-arg PI_VERSION=$(PI_VERSION) -t agentbox-pi:$(VERSION)-pi$(PI_VERSION) .

image-claude: image-base
	$(DOCKER) build -f images/claude/Dockerfile --build-arg BASE_IMAGE=agentbox-base:$(VERSION) --build-arg CLAUDE_VERSION=$(CLAUDE_VERSION) -t agentbox-claude:$(VERSION)-claude$(CLAUDE_VERSION) .

image-opencode: image-base
	$(DOCKER) build -f images/opencode/Dockerfile --build-arg BASE_IMAGE=agentbox-base:$(VERSION) --build-arg OPENCODE_VERSION=$(OPENCODE_VERSION) -t agentbox-opencode:$(VERSION)-opencode$(OPENCODE_VERSION) .

image-all: image-base image-pi image-claude image-opencode

release-check:
	@bash scripts/release-check.sh

# Release build: version-injected binary + gate.
release: vet test build
# shellcheck disable=SC2276
	@VERSION=$(VERSION) bash scripts/release-check.sh
	@echo "release artifacts ready: ./agentbox ($(VERSION))"

fmt:
	gofmt -l -w cmd internal tests

clean:
	rm -f $(BIN)
