.PHONY: help build test vet lint tidy cover install ward-kdl install-tmp lock skew sync-ops-assets sync-exec-assets build-ward-kdl build-ward-kdl-forgejo-tiers

SPECVERB_GEN := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen

REF    ?= v0.48.0
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
	# copies live under docs/ward-kdl/, so relocate them after every rebuild.
	@mkdir -p docs/ward-kdl
	mv ./cmd/ward-kdl/ward-kdl.*.guardfile.md ./docs/ward-kdl/
	$(MAKE) build-ward-kdl-forgejo-tiers
	$(MAKE) sync-ops-assets
	$(MAKE) sync-exec-assets

build-ward-kdl-forgejo-tiers: ## build the read/write/admin forgejo tier binaries (ward#240).
	@mkdir -p bin
	# Forgejo permission tiers: read ⊂ write ⊂ admin, composed by `inherit`
	# (cli-guard#160) over wildcard `"*"` grants (cli-guard#159). Each tier is its
	# own standalone binary, so a withheld verb is absent at compile time, not just
	# denied at runtime: ward-kdl-read has no create/edit/delete leaf, ward-kdl-write
	# no delete leaf. Each tier lives in its own subdir under cmd/ward-kdl/ with its
	# own spec lock, so each `lock` freezes exactly its tier's slice - read the
	# get/list surface, write read + create/edit, admin the full CRUD superset.
	# write/admin `inherit` the read tier's spec/base-url/auth singletons across the
	# sibling subdirs by relative path. `gen` then writes each tier's main.go and
	# reference doc beside its guardfile; the reviewed copies live under docs/ward-kdl/.
	go run $(DRIVER) lock  --guardfile ./cmd/ward-kdl/ward-kdl-read/ward-kdl.forgejo.read.guardfile.kdl
	go run $(DRIVER) lock  --guardfile ./cmd/ward-kdl/ward-kdl-write/ward-kdl.forgejo.write.guardfile.kdl
	go run $(DRIVER) lock  --guardfile ./cmd/ward-kdl/ward-kdl-admin/ward-kdl.forgejo.admin.guardfile.kdl
	go run $(DRIVER) build --guardfile ./cmd/ward-kdl/ward-kdl-read/ward-kdl.forgejo.read.guardfile.kdl   --out bin --set-version $(KDL_VERSION)
	go run $(DRIVER) build --guardfile ./cmd/ward-kdl/ward-kdl-write/ward-kdl.forgejo.write.guardfile.kdl --out bin --set-version $(KDL_VERSION)
	go run $(DRIVER) build --guardfile ./cmd/ward-kdl/ward-kdl-admin/ward-kdl.forgejo.admin.guardfile.kdl --out bin --set-version $(KDL_VERSION)
	go run $(DRIVER) gen   --guardfile ./cmd/ward-kdl/ward-kdl-read/ward-kdl.forgejo.read.guardfile.kdl
	go run $(DRIVER) gen   --guardfile ./cmd/ward-kdl/ward-kdl-write/ward-kdl.forgejo.write.guardfile.kdl
	go run $(DRIVER) gen   --guardfile ./cmd/ward-kdl/ward-kdl-admin/ward-kdl.forgejo.admin.guardfile.kdl
	@mkdir -p docs/ward-kdl
	mv ./cmd/ward-kdl/ward-kdl-read/ward-kdl.*.guardfile.md  ./docs/ward-kdl/
	mv ./cmd/ward-kdl/ward-kdl-write/ward-kdl.*.guardfile.md ./docs/ward-kdl/
	mv ./cmd/ward-kdl/ward-kdl-admin/ward-kdl.*.guardfile.md ./docs/ward-kdl/

sync-ops-assets: ## Mirror the canonical forgejo guardfile + spec lock into cmd/ward for embedding (ward#92).
	# go:embed cannot reach a sibling dir, so `ward ops forgejo` embeds copies of
	# the ward-kdl canonical files. Re-sync after every lock; opsassets_test.go
	# fails the build on drift.
	cp ./cmd/ward-kdl/ward-kdl.forgejo.guardfile.kdl ./cmd/ward/opsassets/forgejo.guardfile.kdl
	cp ./cmd/ward-kdl/forgejo.swagger.lock.json      ./cmd/ward/opsassets/forgejo.swagger.lock.json

sync-exec-assets: ## Mirror the exec-dialect ward-kdl guardfiles into cmd/ward for embedding (ward#284).
	# `ward` auto-mounts every exec-dialect ward-kdl.*.guardfile.kdl under its own
	# wrap path (cmd/ward/wardkdl_exec.go) - the same auto-discovery the ward-kdl
	# build uses, no per-guardfile graft. go:embed can't reach the sibling dir, so
	# mirror the exec-dialect sources here; an exec guardfile is the one with an
	# `exec <bin>` node (spec-dialect ones carry `spec`/`base-url` instead).
	# execassets_test.go fails the build on drift, so re-sync after every change.
	@mkdir -p ./cmd/ward/execassets
	rm -f ./cmd/ward/execassets/*.guardfile.kdl
	@for f in ./cmd/ward-kdl/ward-kdl.*.guardfile.kdl; do \
		if grep -qE '^[[:space:]]+exec ' "$$f"; then cp "$$f" ./cmd/ward/execassets/; fi; \
	done

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
