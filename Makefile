.PHONY: help build test vet lint tidy cover install ward-kdl install-tmp lock skew sync-ops-assets sync-exec-assets build-ward-kdl build-ward-kdl-tiers build-ward-kdl-forgejo-tiers workspace

SPECVERB_GEN := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen

REF ?= v0.48.0

# DRIVER is the specverb-gen invocation `make build-ward-kdl` runs. By default it
# pins the published cli-guard module version (`$(SPECVERB_GEN)@$(REF)`) - the
# cross-module release dance (ward#326): a cli-guard change is only usable here
# after a tag + `go get` bump + REF bump. When a gitignored go.work exists (see
# the `workspace` target) drop the `@$(REF)`, so `go run` resolves specverb-gen
# from the sibling working tree through the workspace - no tag, no `go get`, no
# REF bump. go.work is absent in CI and the warded engineer-carry clone, so single-repo
# builds always take the pinned-version branch and resolve from the module pin.
ifeq ($(wildcard go.work),)
DRIVER := $(SPECVERB_GEN)@$(REF)
else
DRIVER := $(SPECVERB_GEN)
endif

# Go directive for a generated go.work, kept in lockstep with go.mod's `go` line.
GO_VERSION := $(shell awk '/^go [0-9]/ {print $$2; exit}' go.mod)

export GOPRIVATE = forgejo.coilysiren.me

# ward-kdl reports the ward release tag via its --version. A dev `make` build
# stamps the git-described version; the brew formula passes --set-version too.
KDL_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

help: ## Print this help.
	@awk 'BEGIN{FS=":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  make %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build all packages.
	go build ./...

workspace: ## Write a gitignored go.work resolving cli-guard from a sibling ../cli-guard checkout (ward#326 - kills the cross-module release dance for local dev).
	@test -d ../cli-guard || { \
		echo "make workspace: sibling checkout ../cli-guard not found." >&2; \
		echo "  Clone cli-guard beside ward first, e.g.:" >&2; \
		echo "    git clone https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard ../cli-guard" >&2; \
		exit 1; \
	}
	@printf 'go %s\n\nuse (\n\t.\n\t../cli-guard\n)\n' '$(GO_VERSION)' > go.work
	@echo "wrote ./go.work -> use (. ../cli-guard)"
	@echo "cli-guard now resolves from the local working tree: no tag, no 'go get', no REF bump."
	@echo "go.work + go.work.sum are gitignored; delete go.work to return to the pinned module version ($(REF))."

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
	$(MAKE) build-ward-kdl-tiers
	$(MAKE) sync-ops-assets
	$(MAKE) sync-exec-assets

build-ward-kdl-tiers: ## build the read/write/admin tier binaries, discovering every area dropped into each tier subdir (ward#240, ward#338).
	@mkdir -p bin docs/ward-kdl
	# Permission tiers: read ⊂ write ⊂ admin, composed by `inherit` (cli-guard#160)
	# over per-area grants (forgejo's wildcard `"*"`, cli-guard#159; signoz's
	# explicit per-resource leaves). Each tier is its own standalone binary under
	# cmd/ward-kdl/ward-kdl-<tier>/, so a withheld verb is absent at compile time,
	# not just denied at runtime: ward-kdl-read has no create/edit/delete leaf,
	# ward-kdl-write no delete leaf. The driver merges EVERY ward-kdl.*.guardfile.kdl
	# in a tier subdir that shares the `wrap ward-kdl-<tier>` binary name into one
	# binary, keeping each area's spec lock + reference doc separate - so one tier
	# binary carries every area dropped into its subdir (ward#338 proves this on
	# forgejo+signoz). write/admin `inherit` the read tier's spec/base-url/auth
	# singletons across the sibling subdirs by relative path.
	#
	# This loop is area-discovering, not a hardcoded triplet: it globs each tier
	# subdir, so adding an area is dropping its three tier guardfiles - no target
	# edit. A vendored-spec area (signoz) names a `spec` file that exists beside the
	# canonical base guardfile; that file is copied into each tier subdir before
	# locking so `lock` reads it locally and the inheriting members resolve the same
	# spec relative to their own dir. A remote-spec area (forgejo) names a spec with
	# no base-dir file, so nothing is copied and `lock` fetches it upstream.
	@set -e; \
	for gf in cmd/ward-kdl/ward-kdl-read/ward-kdl.*.guardfile.kdl; do \
		spec=$$(awk '/^[[:space:]]*spec /{print $$2; exit}' "$$gf"); \
		[ -n "$$spec" ] && [ -f "cmd/ward-kdl/$$spec" ] || continue; \
		for tier in read write admin; do cp "cmd/ward-kdl/$$spec" "cmd/ward-kdl/ward-kdl-$$tier/$$spec"; done; \
	done; \
	for tier in read write admin; do \
		dir=cmd/ward-kdl/ward-kdl-$$tier; \
		for gf in $$dir/ward-kdl.*.guardfile.kdl; do \
			go run $(DRIVER) lock  --guardfile "$$gf"; \
			go run $(DRIVER) build --guardfile "$$gf" --out bin --set-version $(KDL_VERSION); \
			go run $(DRIVER) gen   --guardfile "$$gf"; \
		done; \
		mv $$dir/ward-kdl.*.guardfile.md docs/ward-kdl/; \
	done

build-ward-kdl-forgejo-tiers: build-ward-kdl-tiers ## Back-compat alias for build-ward-kdl-tiers (ward#338 generalized it past forgejo).

sync-ops-assets: ## Mirror the canonical forgejo guardfile + spec lock into cmd/ward for embedding (ward#92).
	# go:embed cannot reach a sibling dir, so `ward ops forgejo` embeds copies of
	# the ward-kdl canonical files. The `.generated.` infix marks each copy as
	# derived, not hand-edited (ward#270); see cmd/ward/opsassets/README.md.
	# Re-sync after every lock; opsassets_test.go fails the build on drift.
	cp ./cmd/ward-kdl/ward-kdl.forgejo.guardfile.kdl ./cmd/ward/opsassets/forgejo.guardfile.generated.kdl
	cp ./cmd/ward-kdl/forgejo.swagger.lock.json      ./cmd/ward/opsassets/forgejo.swagger.lock.generated.json

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
