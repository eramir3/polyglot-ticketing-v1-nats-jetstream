FLOCI_COMPOSE := docker compose -f infra/floci/docker-compose.yml

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
