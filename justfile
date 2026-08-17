# Per-repo task manifest. Run `just` (or `just --list`) to see every verb.
#
# Recipes take trailing arguments directly: `just <verb> a b`, where the
# retired form was `ward exec <verb> -- a b`.
#
# One line of comment per recipe on purpose: just reads only the LAST comment
# line above a recipe, so a wrapped description silently truncates to its tail.
#
# `ward exec` is retired. `.ward/ward.yaml` survives carrying catalog metadata
# only, because the catalog hooks upstream in agentic-os pin that exact path.

set positional-arguments

# Default target: list every available recipe.
default:
    @just --list --unsorted

# Build all packages.
build *ARGS:
    @make build "$@"

# Run the unit test suite.
test *ARGS:
    @make test "$@"

# Cross-compile every test package for Windows/amd64 without running it.
test-windows-compile *ARGS:
    @make test-windows-compile "$@"

# Run Ward's main command package tests.
test-cmd *ARGS:
    @go test ./cmd/ward "$@"

# Run the focused generic-agent and direct-message framework tests.
test-agent-framework *ARGS:
    @go test ./cmd/ward -run AgentFramework "$@"

# Run the focused typed runtime configuration and role-independence tests.
test-runtime-config *ARGS:
    @go test ./cmd/ward -run RuntimeConfig "$@"

# Run the focused native-policy and external-boundary contract tests.
test-policy-boundary *ARGS:
    @go test ./cmd/ward -run PolicyBoundary "$@"

# Run the focused release-pipeline contract test.
test-release-contract *ARGS:
    @go test ./scripts -run TestReleasePipelineUsesDraftArtifacts "$@"

# Run the repository pre-commit suite across every tracked file.
pre-commit *ARGS:
    @pre-commit run --all-files "$@"

# Format every Go package.
format *ARGS:
    @go fmt ./... "$@"

# Install the ward binary into GOBIN (the Go-CLI install verb).
install *ARGS:
    @make install "$@"

# go vet across the tree.
vet *ARGS:
    @make vet "$@"

# Lint with golangci-lint.
lint *ARGS:
    @make lint "$@"

# Regenerate the code-derived ward agent flag tree.
agent-flags *ARGS:
    @make agent-flags "$@"

# Regenerate the code-derived fixed Ward workflow roster.
agent-roster *ARGS:
    @make agent-roster "$@"

# go mod tidy.
tidy *ARGS:
    @make tidy "$@"

# Unit tests with a coverage profile.
cover *ARGS:
    @make cover "$@"

# Lint issue refs in public docs (ward#446): every ref must resolve for a GitHub reader. `make lint-refs ARGS=--fix` rewrites.
lint-refs *ARGS:
    @make lint-refs "$@"

# Lint the Forgejo<->GitHub Actions mirror (ward#214): the test pair stays identical except runs-on. `make lint-workflows ARGS=--fix` regenerates the Forgejo mirror.
lint-workflows *ARGS:
    @make lint-workflows "$@"
