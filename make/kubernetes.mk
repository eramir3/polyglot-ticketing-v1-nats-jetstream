.PHONY: k8s-prepare-secrets k8s-list-secrets

K8S_NAMESPACE ?= polyglot-ticketing
export K8S_NAMESPACE

export K8S_SECRETS_DIR
k8s-prepare-secrets: ## Generate local Kubernetes Secrets without overwriting existing files.
	@bash scripts/k8s/prepare-secrets.sh

k8s-list-secrets: ## List Kubernetes Secrets in the application namespace (default: polyglot-ticketing).
	@bash scripts/k8s/list-secrets.sh
