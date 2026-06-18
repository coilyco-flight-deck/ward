.PHONY: help build test vet lint tidy cover install ward-kdl install-tmp lock skew sync-ops-assets

SPECVERB_GEN := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen

REF    ?= v0.43.0
DRIVER := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen@$(REF)
export GOPRIVATE = forgejo.coilysiren.me

# ward-kdl reports the ward release tag via its --version. A dev `make` build
# stamps the git-described version; the brew formula passes --set-version too.
KDL_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

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
	go run $(DRIVER) build --guardfile ./cmd/ward-kdl/ward-kdl.forgejo.guardfile.kdl --out bin --set-version $(KDL_VERSION)
	# The driver writes each reference doc beside its guardfile; the committed
	# copies live under docs/, so relocate them after every rebuild.
	mv ./cmd/ward-kdl/ward-kdl.*.guardfile.md ./docs/
	$(MAKE) sync-ops-assets

sync-ops-assets: ## Mirror the canonical forgejo guardfile + spec lock into cmd/ward for embedding (ward#92).
	# go:embed cannot reach a sibling dir, so `ward ops forgejo` embeds copies of
	# the ward-kdl canonical files. Re-sync after every lock; opsassets_test.go
	# fails the build on drift.
	cp ./cmd/ward-kdl/ward-kdl.forgejo.guardfile.kdl ./cmd/ward/opsassets/forgejo.guardfile.kdl
	cp ./cmd/ward-kdl/forgejo.swagger.lock.json      ./cmd/ward/opsassets/forgejo.swagger.lock.json

test: ## Run the unit test suite.
	go test ./...

install: ## Install the ward + ward-kdl binaries into GOBIN (the Go-CLI install verb).
	go install ./...

vet: ## go vet across the tree.
	go vet ./...

lint: ## Lint with golangci-lint.
	golangci-lint run ./...

tidy: ## go mod tidy.
	go mod tidy

cover: ## Unit tests with a coverage profile.
	go test -coverprofile=coverage.out ./...
