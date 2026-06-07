.PHONY: help build test vet lint tidy cover ward-kdl install-tmp lock skew

SPECVERB_GEN := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen

# The committed spec lock is ward-kdl's hermetic, reproducible view of forgejo's
# API: the binary embeds it, so a normal run hits no network for the spec.
# `make lock` is the only step that pulls upstream; `make skew` warns on drift.
SPEC_URL  := https://forgejo.coilysiren.me/swagger.v1.json
SPEC_LOCK := cmd/ward-kdl/forgejo.swagger.lock.json

help: ## Print this help.
	@awk 'BEGIN{FS=":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  make %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build all packages.
	go build ./...

ward-kdl: $(SPEC_LOCK) ## Generate (gitignored main.go) + build the isolated ward-kdl proving module.
	cd cmd/ward-kdl && go run $(SPECVERB_GEN) --guardfile forgejo.guardfile.kdl --out main.go
	cd cmd/ward-kdl && go build -o ../../bin/ward-kdl .

install-tmp: ## Build ward-kdl + symlink it onto PATH as `ward-kdl-tmp` for ad hoc human testing.
	./scripts/install-ward-kdl-tmp.sh

$(SPEC_LOCK):
	@echo "ward-kdl: no spec lock at $@ - run 'make lock' to fetch it" >&2; exit 1

lock: ## Fetch the live forgejo spec into the committed lock (the deliberate 'absorb upstream' step).
	curl -fsS $(SPEC_URL) -o $(SPEC_LOCK)
	@echo "ward-kdl: locked $(SPEC_LOCK) ($$(wc -c < $(SPEC_LOCK) | tr -d ' ') bytes)"

skew: ## Warn if the live forgejo spec has drifted from the committed lock.
	@tmp=$$(mktemp); \
	if curl -fsS $(SPEC_URL) -o $$tmp; then \
	  if cmp -s $$tmp $(SPEC_LOCK); then echo "ward-kdl: spec lock in sync with $(SPEC_URL)"; \
	  else echo "ward-kdl: SKEW - live spec differs from $(SPEC_LOCK); run 'make lock' to absorb" >&2; fi; \
	else echo "ward-kdl: skew check could not fetch $(SPEC_URL)" >&2; fi; \
	rm -f $$tmp

test: ## Run the unit test suite.
	go test ./...

vet: ## go vet across the tree.
	go vet ./...

lint: ## Lint with golangci-lint.
	golangci-lint run ./...

tidy: ## go mod tidy.
	go mod tidy

cover: ## Unit tests with a coverage profile.
	go test -coverprofile=coverage.out ./...
