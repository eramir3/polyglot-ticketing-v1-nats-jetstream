.DEFAULT_GOAL := help

include make/development.mk
include make/performance.mk
include make/dlq.mk
include make/docker.mk
include make/floci.mk
include make/kubernetes.mk

.PHONY: help
help: ## Show available commands.
	@awk 'BEGIN {FS = ":.*##"}; /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
