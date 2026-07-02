# The toy project's Makefile. Every target ward.yaml exposes carries a
# `## <help>` comment; `ward doctor` cross-checks that text against the matching
# command's description: (the allowlist-drift guard). See ../../docs/doctor.md.

BIN := build/greet

.PHONY: help build test vet install

help: ## Print this help.
	@awk 'BEGIN{FS=":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  make %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the greet CLI.
	mkdir -p build
	cp greet.sh $(BIN)
	chmod +x $(BIN)

test: ## Run the unit tests.
	./test.sh

vet: ## Static-check the shell sources.
	sh -n greet.sh test.sh

install: ## Install greet into ./build (toy stand-in for GOBIN).
	$(MAKE) build
