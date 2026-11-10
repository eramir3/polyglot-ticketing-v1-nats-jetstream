FLOCI_COMPOSE := docker compose -f infra/floci/docker-compose.yml
FLOCI_TERRAFORM := terraform -chdir=infra/terraform/environments/local

.PHONY: floci-up floci-down floci-logs floci-ps floci-health

floci-up: ## Start the standalone Floci runtime and wait up to 60 seconds for health.
	$(FLOCI_COMPOSE) up -d
	@for attempt in $$(seq 1 30); do \
		if curl --fail --silent --max-time 2 http://127.0.0.1:4566/_floci/health > /dev/null; then \
			echo 'Floci is healthy at http://127.0.0.1:4566'; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo 'Floci did not become healthy; inspect make floci-logs.' >&2; exit 1

floci-down: ## Stop Floci and retain its persistent data volume.
	$(FLOCI_COMPOSE) down

floci-logs: ## Follow standalone Floci logs.
	$(FLOCI_COMPOSE) logs -f

floci-ps: ## Show standalone Floci status.
	$(FLOCI_COMPOSE) ps

floci-health: ## Check the local Floci health endpoint.
	curl --fail --silent --show-error --max-time 5 http://127.0.0.1:4566/_floci/health

.PHONY: floci-eks-init floci-eks-plan floci-eks-apply floci-eks-kubeconfig floci-eks-verify floci-eks-destroy

floci-eks-init: ## Initialize the pinned Terraform provider for local Floci.
	$(FLOCI_TERRAFORM) init

floci-eks-plan: ## Review local EKS infrastructure changes against Floci.
	$(FLOCI_TERRAFORM) plan

floci-eks-apply: ## Provision the local EKS foundation through Floci.
	$(FLOCI_TERRAFORM) apply

floci-eks-kubeconfig: ## Generate isolated local Kubernetes credentials from Terraform outputs.
	python3 scripts/floci/configure-kubeconfig.py

floci-eks-verify: ## Verify the local node, DNS, PVC binding, and data persistence.
	python3 scripts/floci/verify-cluster.py

floci-eks-destroy: ## Destroy Terraform-managed local EKS resources; retain Floci and retained data volumes.
	$(FLOCI_TERRAFORM) destroy
