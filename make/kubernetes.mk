.PHONY: k8s-prepare-secrets k8s-list-secrets k8s-deploy-dependencies k8s-prepare-images k8s-deploy-apps k8s-deploy-gateway k8s-port-forward-gateway

K8S_TARGET ?= minikube
export K8S_TARGET

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

k8s-prepare-images: ## Generate contracts, build six application images, and load them into the selected cluster.
	@bash scripts/k8s/prepare-images.sh

k8s-deploy-apps: ## Deploy the six applications to the selected cluster and wait for readiness.
	@bash scripts/k8s/deploy-apps.sh

k8s-deploy-gateway: ## Deploy Gateway API routing and wait for controller reconciliation.
	@bash scripts/k8s/deploy-gateway.sh

k8s-port-forward-gateway: ## Forward localhost:3000 to Traefik in the foreground (Ctrl+C to stop).
	@bash scripts/k8s/port-forward-gateway.sh

.PHONY: k8s-apply-secrets k8s-port-forward-api k8s-test-scripts

k8s-apply-secrets: ## Validate five local Secret files, create the namespace, and apply Secrets.
	@bash scripts/k8s/apply-secrets.sh

k8s-port-forward-api: ## Forward localhost:3000 directly to the selected cluster's API gateway.
	@bash scripts/k8s/port-forward-api.sh

k8s-test-scripts: ## Test Kubernetes deployment scripts without contacting Docker or Kubernetes.
	@python3 -m unittest discover -s scripts/k8s/tests -p 'test_*.py'
