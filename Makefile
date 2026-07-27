.PHONY: help build test vet lint lint-refs lint-workflows tidy cover install sync-fleet-assets sync-topology-assets sync-defaults-assets sync-role-assets workspace agent-roster agent-flags demo-image

# Go directive for a generated go.work, kept in lockstep with go.mod's `go` line.
GO_VERSION := $(shell awk '/^go [0-9]/ {print $$2; exit}' go.mod)

export GOPRIVATE = forgejo.coilysiren.me

help: ## Print this help.
	@awk 'BEGIN{FS=":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  make %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: sync-defaults-assets ## Build all packages.
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
	@echo "cli-guard now resolves from the local working tree instead of the pinned module."
	@echo "go.work + go.work.sum are gitignored; delete go.work to return to the module-pinned dependency."

sync-fleet-assets: ## Mirror the typed fleet policy into cmd/ward for embedding (ward#415).
	# The fleet config is dialect 2 (fleetconfig, not a guardfile): it names the
	# agent roster + launch shape, never a permission. go:embed can't reach the
	# sibling .ward/policy/ dir, so mirror the one canonical source here as
	# fleet.generated.kdl (the `.generated.` infix marks it derived, ward#270).
	# fleetassets_test.go fails the build on drift, so re-sync after every change.
	@mkdir -p ./cmd/ward/fleetassets
	cp ./.ward/policy/fleet.kdl ./cmd/ward/fleetassets/fleet.generated.kdl

sync-defaults-assets: ## Mirror the canonical smart-defaults KDL into cmd/ward for embedding (ward#679).
	# The smart-defaults bundle carries baked native runtime policy knobs. go:embed
	# can't reach the sibling .ward/policy/ dir, so mirror the canonical source
	# here as an ignored build artifact. defaultsassets_test.go fails the build on drift.
	@mkdir -p ./cmd/ward/defaultsassets
	cp ./.ward/policy/defaults.kdl ./cmd/ward/defaultsassets/defaults.generated.kdl

sync-role-assets: ## Mirror the shipped role-definition KDL into cmd/ward for embedding.
	# The shipped agent role presets are product defaults, not a fleet overlay.
	# go:embed can't reach the sibling .ward/policy/ dir, so mirror the canonical
	# source here as role-definitions.generated.kdl. roleassets_test.go fails the build on drift.
	@mkdir -p ./cmd/ward/roleassets
	cp ./.ward/policy/roles.kdl ./cmd/ward/roleassets/role-definitions.generated.kdl

sync-topology-assets: ## Mirror the container topology bundle into cmd/ward for embedding (ward#655).
	# The container-topology overlay is bundle data, not code: go:embed can't
	# reach the sibling .ward/policy/ dir, so mirror the canonical source here
	# as topology.generated.kdl. topologyassets_test.go fails the build on drift.
	@mkdir -p ./cmd/ward/topologyassets
	cp ./.ward/policy/topology.kdl ./cmd/ward/topologyassets/topology.generated.kdl

agent-roster: ## Regenerate docs/agent-roster.md from the code roster - the binary describing its own roles (ward#348).
	# The flat agent-role list is generated, never hand-edited: `ward agent roster`
	# walks the roles agentCommand() registers and prints the committed doc body.
	# agent_roster_test.go fails the build on drift, so re-run after touching the roster.
	go run ./cmd/ward agent roster --markdown > docs/agent-roster.md

agent-flags: ## Regenerate docs/agent-flags.md from the code flag tree - the binary describing its own flags (ward#1116).
	# The agent flag tree is generated, never hand-edited: `ward agent flags`
	# walks the agent subtree and prints the committed doc body.
	# agent_flags_test.go fails the build on drift, so re-run after touching the tree.
	go run ./cmd/ward agent flags --markdown > docs/agent-flags.md

demo-image: ## Build the public demo image that runs simple workspace + substrate demos against neutral OSS defaults.
	docker build --tag ward-demo:dev --file docker/demo/Dockerfile .

test: sync-defaults-assets ## Run the unit test suite.
	go test ./...

install: sync-defaults-assets ## Install the ward binary into GOBIN (the Go-CLI install verb).
	go install ./...

vet: sync-defaults-assets ## go vet across the tree.
	go vet ./...

lint: sync-defaults-assets ## Lint with golangci-lint.
	golangci-lint run --timeout=15m ./...

tidy: ## go mod tidy.
	go mod tidy

cover: sync-defaults-assets ## Unit tests with a coverage profile.
	go test -coverprofile=coverage.out ./...

lint-refs: ## Lint issue refs in public docs (ward#446): every ref must resolve for a GitHub reader. `make lint-refs ARGS=--fix` rewrites.
	python3 scripts/lint_issue_refs.py $(ARGS)

lint-workflows: ## Lint the Forgejo<->GitHub Actions mirror (ward#214): the test pair stays identical except runs-on. `make lint-workflows ARGS=--fix` regenerates the Forgejo mirror.
	python3 scripts/check_workflow_mirror.py $(ARGS)
