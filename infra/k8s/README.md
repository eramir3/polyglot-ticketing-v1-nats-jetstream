# Local Kubernetes

These manifests deploy Tickets, Identity, Orders, Payments, Expiration, and the
API Gateway, plus NATS JetStream and their four PostgreSQL databases, to a local Kubernetes cluster.
Each workload has one replica and an internal ClusterIP Service. Use the same
namespace for all workloads. Redis and Mailpit run in-cluster. Optional Gateway
API routing reuses a separately installed Traefik controller; observability
backends are supplied separately.

## Build and load images

Start your cluster and select its kubectl context. From the repository root,
with Docker, pnpm, and workspace dependencies installed, generate contracts and
build the existing images:

```sh
make generate-proto
for service in tickets identity orders payments expiration api-gateway; do
  docker build -f "apps/$service/Dockerfile.dev" -t "$service:dev" .
done
```

Load all six images into minikube:

```sh
for service in tickets identity orders payments expiration api-gateway; do
  minikube image load "$service:dev"
done
```

For kind, use `kind load docker-image "$service:dev"` instead (add
`--name <cluster>` for a named cluster). Docker Desktop Kubernetes can use local
images when its image store is shared with Docker. All manifests use
`imagePullPolicy: IfNotPresent`.

## Configure dependencies

Each application service directory contains a `configmap.yaml` for nonsecret
configuration. Secret templates live under `examples/`. The four database
Deployments initialize the same database and user names as Compose and read
`POSTGRES_PASSWORD` from their owning application's Secret. Applications and
their migration init containers continue reading `DATABASE_URL` from that Secret.

Redis is available at `redis:6379` and Mailpit SMTP at `mailpit:1025`.
Neither requires authentication in this local setup. Mailpit captures email;
it does not deliver it to external inboxes.

PostgreSQL connections use the same-namespace Services `identity-db`, `tickets-db`,
`orders-db`, and `payments-db`, all on port `5432`. These databases are separate
from Compose databases; no existing data is imported.

Tickets, Orders, Payments, and Expiration use `NATS_URL=nats://nats:4222`
for the same-namespace NATS Service. Set Identity's `SMTP_HOST`, `SMTP_PORT`, and
`SMTP_FROM` for its existing unauthenticated development SMTP transport. Set
Expiration's `REDIS_URL` in its Secret, including credentials if needed.

Do not run Compose and Kubernetes application instances against the same
databases, event streams, and Redis data simultaneously. Use isolated dependency
instances or stop the Compose application services when reusing local data.

Gateway gRPC destinations use same-namespace Services: `identity:50051`,
`tickets:50052`, `orders:50053`, and `payments:50054`. Identity's
`BETTER_AUTH_URL=http://localhost:3000` and
`EMAIL_VERIFICATION_URL=http://localhost:3000/api/auth/verify-email` match gateway
port-forwarding below. Verification links target the gateway; Identity remains
gRPC-only. Update both values if changing the browser-facing gateway address.
Identity and the gateway share `TICKETING_USER_APP_ORIGIN`, defaulting to
`http://localhost:3001` for the separately hosted client.

Payments defaults to deterministic success with
`PAYMENT_PROCESSOR_OUTCOME=success` and
`PAYMENT_PROCESSOR_RANDOM_FAILURES=false`. The optional gateway load-test token
is omitted.

## Create Secrets and deploy

Keep descriptive script names; the numbered steps below show setup order.

### 1. Prepare local Secret files

Generate populated Secret files outside the repository (requires `openssl`):

```sh
make k8s-prepare-secrets
```

This creates `/tmp/k8s-polyglot-ticketing-secrets` with mode `700` and five
Secret files with mode `600`. For newly created files, it runs `openssl rand -hex 32`
once and uses the same 64-character hex value for all four database passwords,
their matching database URLs, and Identity's `BETTER_AUTH_SECRET`. Hex values
need no additional URL encoding. Expiration keeps `REDIS_URL=redis://redis:6379`.
Credentials are written only to the files, not printed. No resources are applied
to the cluster.

Existing files are preserved, so rerunning the command does not overwrite
credentials or fill placeholders in existing copies. If only some files are
missing, those new files share a fresh value; existing files keep their own values.

Override the destination with `make k8s-prepare-secrets K8S_SECRETS_DIR=/your/private/path`.
The script can also be invoked from any directory with
`bash /path/to/repository/scripts/k8s/prepare-secrets.sh`; set `K8S_SECRETS_DIR`
in the environment to override its destination. Symlink destinations and Secret
files are rejected. Use the same directory in the apply commands below if overridden.

For manually edited files, keep each database Secret's `POSTGRES_PASSWORD` and
the URL-encoded password in `DATABASE_URL` consistent. Keep the database Service
names and port `5432` from the examples, Expiration's `REDIS_URL` as
`redis://redis:6379`, and Identity's `BETTER_AUTH_SECRET` at least 32 characters
long. If reusing a Secret
copy from the earlier external-database setup, add `POSTGRES_PASSWORD` and update
its `DATABASE_URL` before applying it. Do not commit credentials.

### 2. Apply Secrets to the application namespace

Use `polyglot-ticketing` for the application namespace in these examples. If it
does not exist yet, create it:

```sh
kubectl create namespace polyglot-ticketing
```

Preparing files does not create Secrets in Kubernetes. Apply the populated files
to the namespace using the current kubectl context:

```sh
for service in tickets identity orders payments expiration; do
  kubectl -n polyglot-ticketing apply -f "/tmp/k8s-polyglot-ticketing-secrets/$service-secret.yaml"
done
```

### 3. Verify cluster Secrets

```sh
make k8s-list-secrets
```

This runs `kubectl get secrets -n polyglot-ticketing`, showing the standard summary
table without Secret values. It can be repeated anytime; it does not create a
namespace or apply resources. Kubectl errors are passed through by the script.
Override the namespace with `make k8s-list-secrets K8S_NAMESPACE=another-namespace`.
For direct invocation, run `bash scripts/k8s/list-secrets.sh`; it also accepts
`K8S_NAMESPACE` from the environment and uses the current kubectl context.

### 4. Deploy dependencies and applications

Use the same namespace for dependencies and applications. Wait for databases
before applying application manifests:

```sh
kubectl -n polyglot-ticketing apply -f infra/k8s/redis
kubectl -n polyglot-ticketing apply -f infra/k8s/mailpit
kubectl -n polyglot-ticketing rollout status deployment/redis --timeout=600s
kubectl -n polyglot-ticketing rollout status deployment/mailpit --timeout=600s

kubectl -n polyglot-ticketing apply -f infra/k8s/nats
kubectl -n polyglot-ticketing rollout status deployment/nats --timeout=600s

kubectl -n polyglot-ticketing apply -f infra/k8s/identity-db
kubectl -n polyglot-ticketing apply -f infra/k8s/tickets-db
kubectl -n polyglot-ticketing apply -f infra/k8s/orders-db
kubectl -n polyglot-ticketing apply -f infra/k8s/payments-db

for database in identity-db tickets-db orders-db payments-db; do
  kubectl -n polyglot-ticketing rollout status "deployment/$database" --timeout=600s
done

kubectl -n polyglot-ticketing apply -f infra/k8s/tickets
kubectl -n polyglot-ticketing apply -f infra/k8s/identity
kubectl -n polyglot-ticketing apply -f infra/k8s/orders
kubectl -n polyglot-ticketing apply -f infra/k8s/payments
kubectl -n polyglot-ticketing apply -f infra/k8s/expiration
kubectl -n polyglot-ticketing apply -f infra/k8s/api-gateway

for service in tickets identity orders payments expiration api-gateway; do
  kubectl -n polyglot-ticketing rollout status "deployment/$service" --timeout=600s
done
```

Directory application is nonrecursive and excludes example Secrets. Avoid `-R`,
which would apply placeholders. Manifests are namespace-neutral; replace
`polyglot-ticketing` consistently if choosing another namespace. Subsequent
namespace-scoped kubectl examples omit the namespace for brevity; add
`-n polyglot-ticketing` (or your chosen namespace) when running them.

Tickets, Identity, Orders, and Payments run existing migration commands in init
containers before their application starts. Up-to-date schemas succeed;
migration failures block startup and are retried. Identity's migration receives
the same auth, SMTP, and database configuration as its runtime. The gateway and
Expiration have no migrations.

## PostgreSQL storage and checks

Each database directory contains a Deployment, Service, and a separate `1Gi`
`ReadWriteOnce` PVC named `<database>-data`. PVCs use the cluster's default storage
class; check it with `kubectl get storageclass`. Minikube's default provisioner
can satisfy these claims. If a claim remains Pending, inspect it with
`kubectl describe pvc tickets-db-data` before troubleshooting application startup.

The volume mounts at `/var/lib/postgresql/data`, with `PGDATA` set to its `pgdata`
subdirectory. Data survives pod replacement and Deployment recreation when the
PVC is retained. Deleting the PVC can delete its data, depending on the storage
class's reclaim policy; deleting the entire database directory's resources also
deletes its PVC. Keep databases at one replica with Recreate updates.

PostgreSQL initializes the configured database, user, and password only on an
empty volume. Changing `POSTGRES_PASSWORD` in the Secret does not change the
password of an initialized database. Update that database user's password in
PostgreSQL and keep the Secret's password and connection URL in sync.

Inspect database readiness and storage:

```sh
kubectl get deployments,pods,services,pvc
kubectl logs deployment/tickets-db -c postgres
kubectl get endpointslices -l kubernetes.io/service-name=tickets-db
```

After deploying, this optional check authenticates through the Tickets database
Service and verifies a marker row survives pod replacement. Run it on your local
test database; the marker table is removed at the end:

```sh
tickets_sql() {
  kubectl exec deployment/tickets-db -- sh -c \
    'PGPASSWORD="$POSTGRES_PASSWORD" psql -h tickets-db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -c "$1"' sh "$1"
}

tickets_sql 'SELECT current_database(), current_user;'
tickets_sql 'CREATE TABLE k8s_persistence_check (id integer PRIMARY KEY); INSERT INTO k8s_persistence_check VALUES (1);'
kubectl rollout restart deployment/tickets-db
kubectl rollout status deployment/tickets-db --timeout=600s
tickets_sql 'SELECT id FROM k8s_persistence_check;'
tickets_sql 'DROP TABLE k8s_persistence_check;'
```

The query after restart should return `1`. Repeat with the other database
Deployment, Service, and owning user as needed. Database probes use `pg_isready`
over TCP and allow five minutes for initialization; they do not verify passwords.

For host database tools, run `kubectl port-forward service/tickets-db 5433:5432`
and connect to `localhost:5433` with the configured credentials. Choose another
local port if it is already in use.

## Verify and operate

| Service | Internal application port | Metrics port |
| --- | --- | --- |
| Tickets | gRPC `50052` | `9090` |
| Identity | gRPC `50051` | `9090` |
| Orders | gRPC `50053` | `9090` |
| Payments | gRPC `50054` | `9090` |
| Expiration | None (worker only) | `9090` |
| API Gateway | HTTP `3000` | `9090` |

Inspect a workload and its Service endpoints (replace `identity` as needed):

```sh
kubectl get pods -l app.kubernetes.io/name=identity
kubectl logs deployment/identity -c migrate
kubectl logs deployment/identity -c identity
kubectl get endpointslices -l kubernetes.io/service-name=identity
kubectl port-forward service/api-gateway 3000:3000 9090:9090
```

For the gateway and Expiration, inspect only application container logs. While
the forward runs, use another terminal to check the gateway and Tickets:

```sh
curl http://localhost:9090/metrics
curl http://localhost:3000/api/tickets
```

Verify authentication through gateway signup, the externally delivered
verification email, and sign-in. With a verified session, order creation and
payment exercise Orders and Payments; Expiration logs and metrics show expiry
job scheduling. Check each application's `migrate` init-container logs to verify
its schema migration completed. These workflow checks also require the remaining
external dependencies.

To inspect another service's metrics, stop the gateway forward and run, for
example, `kubectl port-forward service/expiration 9090:9090`, then fetch
`/metrics`. Backend gRPC ports do not serve HTTP. The application manifests create no Ingress, NodePort, or
LoadBalancer. Optional Gateway API access is described below.

Application TCP probes verify listener availability, not continued database,
broker, Redis, or SMTP health. The five newer application workloads allow five minutes for startup before
readiness and liveness checks begin. Expiration probes its metrics listener; a
ready pod does not guarantee event processing. Use logs and metrics to verify
that behavior. Existing Tickets probes remain unchanged.

Tracing configuration is unchanged: Go services disable tracing without an
exporter endpoint; Node services default to `localhost:4318` and may report
export errors without a collector. No collector is included. To use an existing
collector, set `OTEL_EXPORTER_OTLP_ENDPOINT` to `host:port` in the relevant
ConfigMaps and restart the Deployments.

Recreate updates cause downtime and prevent overlapping application versions
during migrations. Keep this setup at one replica. Rebuild and reload changed
images, then run, for example, `kubectl rollout restart deployment/identity`.
After editing a ConfigMap or Secret, apply it and restart its Deployment to load
new values. Migrations rerun when a pod is recreated; restarting only the
application container does not rerun a completed init container.

Concert Assistant and the remaining infrastructure manifests and controller installation remain deferred. Existing
Compose workflows remain available independently.

## Optional Gateway API access

`infra/k8s/gateway` contains a Gateway and HTTPRoute using the stable v1 API.
They reuse an existing Traefik controller and accepted `traefik` GatewayClass;
they do not install or manage shared cluster infrastructure. The inspected local
minikube setup uses Traefik v3.6.2, an HTTP entry point on port 80, and Service
`traefik` in namespace `traefik`. The Gateway listener port must match that entry
point. Check prerequisites before applying:

```sh
kubectl get crd gateways.gateway.networking.k8s.io httproutes.gateway.networking.k8s.io
kubectl get gatewayclass traefik
kubectl -n traefik get deployment traefik -o yaml
kubectl -n traefik get service traefik
```

Deploy the application workloads first. Replace `<application-namespace>` with
their namespace throughout these commands; the routing resources must share the API gateway Service's namespace:

```sh
kubectl -n <application-namespace> apply --server-side --dry-run=server -k infra/k8s/gateway
kubectl -n <application-namespace> apply -k infra/k8s/gateway
kubectl -n <application-namespace> get gateway ticketing-gateway -o yaml
kubectl -n <application-namespace> get httproute ticketing-api -o yaml
```

Check Gateway `Accepted=True` and `Programmed=True`, and HTTPRoute parent
conditions `Accepted=True` and `ResolvedRefs=True`. Stop any existing forward
using local port 3000, then run:

```sh
kubectl -n traefik port-forward service/traefik 3000:80
```

This forwards to Traefik's proxy; the Gateway resource itself does not bind a
localhost port. A pending LoadBalancer external address does not prevent this
workflow. Visit `http://localhost:3000/api/tickets`. Only hostname `localhost`
and path prefix `/api` route to `api-gateway:3000`, preserving the request path.
Routes attach only from the Gateway's namespace. Backend gRPC, metrics,
databases, tools, and existing lab routes retain their existing access paths.

The existing `BETTER_AUTH_URL`, `EMAIL_VERIFICATION_URL`, and frontend-origin
settings remain valid. Verify signup, the Mailpit verification link, sign-in,
and authenticated session access through this address. Check that invalid API
requests preserve the public error format, and `/metrics` and paths outside
`/api` do not reach the application. Existing `whoami.localhost` and
`color-app.localhost` routes should continue working through Traefik.

Direct application port-forwarding remains an alternative. To remove only the
optional routing resources:

```sh
kubectl -n <application-namespace> delete -k infra/k8s/gateway
```

TLS, DNS, tunnel access, and controller installation are outside this setup.

## NATS JetStream storage and checks

NATS uses one replica with Recreate updates and `nats:2.11-alpine`, matching
Compose. JetStream stores data at `/data` on the separate `1Gi` ReadWriteOnce
`nats-data` PVC using the default storage class. Retain the PVC to preserve
messages across pod replacement; deleting it may delete the data. This local
server has no authentication or TLS and is exposed only inside the cluster.
Applications continue to create their own streams and consumers.

Startup and readiness probes require JetStream health; liveness checks server
health without checking individual streams or consumers. Startup allows five
minutes. Inspect and forward the internal ports with:

```sh
kubectl get pods,pvc -l app.kubernetes.io/name=nats
kubectl get pvc nats-data
kubectl logs deployment/nats
kubectl get endpointslices -l kubernetes.io/service-name=nats
kubectl port-forward service/nats 4222:4222 8222:8222
```

In another terminal, verify health and persistence with the NATS CLI installed:

```sh
curl --fail 'http://localhost:8222/healthz?js-enabled-only=true'
nats --server nats://localhost:4222 stream add K8S_PERSISTENCE_CHECK --subjects k8s.persistence.check --storage file --replicas 1 --defaults
nats --server nats://localhost:4222 pub k8s.persistence.check marker
kubectl rollout restart deployment/nats
kubectl rollout status deployment/nats --timeout=600s
```

Restart port-forwarding after the pod is replaced, then verify the marker and
remove the temporary stream:

```sh
nats --server nats://localhost:4222 stream get K8S_PERSISTENCE_CHECK 1
nats --server nats://localhost:4222 stream rm K8S_PERSISTENCE_CHECK --force
```

After applying the updated application ConfigMaps to an existing installation,
restart Tickets, Orders, Payments, and Expiration and check their logs for NATS
connections. No Compose broker data is imported.

## Redis and Mailpit

Redis uses one replica, Recreate updates, AOF persistence, and a `1Gi`
ReadWriteOnce PVC mounted at `/data`, using the default StorageClass. Retain
`redis-data` to preserve delayed jobs across pod replacement. Deleting the PVC
may delete scheduled jobs. Mailpit messages are ephemeral and may disappear
when its container restarts.

For existing installations, update `expiration-secret` with
`REDIS_URL=redis://redis:6379`, apply Identity's updated ConfigMap, then restart
both application Deployments to load their configuration. Keep Compose and
Kubernetes dependency instances separate.

Open the inbox at http://localhost:8025 after running:

```sh
kubectl port-forward service/mailpit 8025:8025
```

Check Redis with `kubectl exec deployment/redis -- redis-cli ping` (expect
`PONG`). To check persistence, set a temporary key, delete the Redis pod, wait
for its replacement to become ready, read the key, then delete the key.
Verify signup through the gateway and follow the verification link captured
in Mailpit. Reserve a ticket and verify that its unpaid order is canceled
after the existing 15-minute expiration delay; inspect Expiration and Orders
logs as well as Redis job processing.

## Optional observability and development tools

Build `nui:dev` with `docker build -f infra/nui/Dockerfile -t nui:dev .`
and load it into your cluster just like the application images.
After the core dependencies and Secrets are installed, run:

```sh
kubectl apply -k infra/k8s/observability
kubectl apply -k infra/k8s/development-tools
kubectl apply -k infra/k8s/overlays/observability
```

The last command applies the six application workloads with trace export to
`alloy:4318`; omit it to keep the base application configuration. Wait for
all optional Deployments to roll out. Use the same namespace for every command.
Alloy discovers pods only in its own namespace through its ServiceAccount and
Role, collecting application runtime logs without host mounts or Docker access.
Logs retain `service` and `environment=local` labels plus pod/container labels.
Prometheus scrapes all six application Services; Alloy sends traces to Tempo.

Port-forward Services with `kubectl port-forward service/<name> <local>:<remote>`:
Grafana `3002:3000`, Prometheus `9090:9090`, NUI `31311:31311`, and Redis Insight
`5540:5540`. Grafana retains its standard login (initially admin/admin).
Its Loki, Prometheus, Tempo datasources and Ticketing k6 dashboard are provisioned.
The dashboard needs separately supplied k6 data; no load generator is installed.
In NUI connect to `nats://nats:4222`; in Redis Insight connect to `redis:6379`.

Each stateful tool has a separate 1Gi PVC using the default StorageClass; retain
these claims to preserve data across pod replacement. Telemetry retention is
seven days and may need larger claims for heavy local use. Alloy is stateless.
After ConfigMap changes restart the affected tool Deployment. To remove trace
export, reapply each application's base with `kubectl apply -k infra/k8s/<service>`
and explicitly remove the overlay-added environment variable with
`kubectl set env deployment/<service> OTEL_EXPORTER_OTLP_ENDPOINT-` before stopping
Alloy. Deleting an optional group also deletes its PVCs and may destroy its data.

Verify all six Prometheus targets are UP, query application logs in Grafana's
Loki Explore, and generate gateway requests to verify traces in Tempo. Check
NUI and Redis Insight connections and retained tool data after pod replacement.
