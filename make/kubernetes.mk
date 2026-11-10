.PHONY: k8s-prepare-secrets k8s-list-secrets k8s-deploy-dependencies k8s-prepare-images k8s-deploy-apps k8s-deploy-gateway k8s-port-forward-gateway

K8S_NAMESPACE ?= polyglot-ticketing
export K8S_NAMESPACE

MINIKUBE_PROFILE ?= minikube
export MINIKUBE_PROFILE

export K8S_SECRETS_DIR
k8s-prepare-secrets: ## Generate local Kubernetes Secrets without overwriting existing files.
	@bash scripts/k8s/prepare-secrets.sh

k8s-list-secrets: ## List Kubernetes Secrets in the application namespace (default: polyglot-ticketing).
	@bash scripts/k8s/list-secrets.sh

k8s-deploy-dependencies: ## Deploy Redis, Mailpit, NATS, and four PostgreSQL databases and wait for readiness.
	@bash scripts/k8s/deploy-dependencies.sh

k8s-prepare-images: ## Generate contracts, build six application images, and load them into minikube.
	@bash scripts/k8s/prepare-images.sh

k8s-deploy-apps: ## Deploy the six applications to minikube and wait for readiness.
	@bash scripts/k8s/deploy-apps.sh

k8s-deploy-gateway: ## Deploy Gateway API routing and wait for controller reconciliation.
	@bash scripts/k8s/deploy-gateway.sh

k8s-port-forward-gateway: ## Forward localhost:3000 to Traefik in the foreground (Ctrl+C to stop).
	@bash scripts/k8s/port-forward-gateway.sh
