.PHONY: help build test vet lint tidy cover ward-kdl install-tmp lock skew

SPECVERB_GEN := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen

REF    ?= v0.13.0
DRIVER := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen@$(REF)
export GOPRIVATE = forgejo.coilysiren.me

help: ## Print this help.
	@awk 'BEGIN{FS=":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  make %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build all packages.
	go build ./...

build-ward-kdl: ## build or rebuild the ward-kdl binary, one shot for ease of use in development.
	rm -rf bin
	@mkdir -p bin
	# The driver discovers every ward-kdl.*.guardfile.kdl beside this one that
	# shares the `wrap ward-kdl` binary name and merges them into one binary,
	# keeping each API's spec lock and reference doc separate. Adding a new
	# ward-kdl.<api>.guardfile.kdl is the only step to grow the surface.
	go run $(DRIVER) lock  --guardfile ./cmd/ward-kdl/ward-kdl.forgejo.guardfile.kdl
	go run $(DRIVER) build --guardfile ./cmd/ward-kdl/ward-kdl.forgejo.guardfile.kdl --out bin

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
