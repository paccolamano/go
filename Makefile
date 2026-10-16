# Repository tooling entry point. The logic lives in scripts/, shared with the
# GitHub workflows; see CONTRIBUTING.md. Compatible with GNU Make 3.81.

SHELL := bash
include versions.env

.DEFAULT_GOAL := help
.PHONY: help lint test check work scripts-test

# MODULE=<root> restricts lint, test and check to one module.
TARGETS = $(if $(MODULE),$(MODULE),--all)

help: ## List the available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

lint: ## Run golangci-lint on every module, or MODULE=<root>
	@scripts/lint.sh $(TARGETS)

test: ## Run race+shuffle tests with the coverage gate, or MODULE=<root>
	@scripts/test.sh $(TARGETS)

check: ## Run the local gates: lint, then test
	@scripts/lint.sh $(TARGETS)
	@scripts/test.sh $(TARGETS)

work: ## Regenerate go.work from the discovered modules
	@scripts/work-sync.sh

scripts-test: ## Run the tooling self-tests and shellcheck (tooling development only)
	@scripts/tests/run.sh
	@command -v shellcheck >/dev/null 2>&1 || { echo "error: install shellcheck (needed only for tooling development)" >&2; exit 2; }
	@shellcheck scripts/*.sh scripts/tests/*.sh
