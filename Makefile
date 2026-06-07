.PHONY: help build test vet lint tidy cover ward-kdl

SPECVERB_GEN := forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cmd/specverb-gen

help: ## Print this help.
	@awk 'BEGIN{FS=":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  make %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build all packages.
	go build ./...

ward-kdl: ## Generate (gitignored main.go) + build the isolated ward-kdl proving module.
	cd cmd/ward-kdl && go run $(SPECVERB_GEN) --guardfile forgejo.guardfile.kdl --out main.go
	cd cmd/ward-kdl && go build -o ../../bin/ward-kdl .

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
