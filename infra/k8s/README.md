# Local Kubernetes

These manifests deploy Tickets, Identity, Orders, Payments, Expiration, and the
API Gateway, plus their four PostgreSQL databases, to a local Kubernetes cluster.
Each workload has one replica and an internal ClusterIP Service. Use the same
namespace for all workloads. NATS JetStream, Redis, SMTP, ingress, and
observability backends are supplied separately.

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

| Workload | External dependencies | Example host ports |
| --- | --- | --- |
| Tickets | NATS JetStream | `4222` |
| Identity | SMTP | `1025` |
| Orders | NATS JetStream | `4222` |
| Payments | NATS JetStream | `4222` |
| Expiration | NATS JetStream, its own Redis data | `4222`, `6379` |
| API Gateway | Backend Kubernetes Services | No external persistence |

Examples use `host.docker.internal` for host dependencies on Docker Desktop.
Replace it with an address reachable from your cluster as needed; minikube also
provides `host.minikube.internal` for reaching its host. Verify connectivity for
your driver. Pod-local `localhost` cannot reach host services. Compose Mailpit
exposes SMTP only inside its network; arrange cluster-reachable SMTP separately
if using it.

PostgreSQL connections use the same-namespace Services `identity-db`, `tickets-db`,
`orders-db`, and `payments-db`, all on port `5432`. These databases are separate
from Compose databases; no existing data is imported.

Configure `NATS_URL` in Tickets, Orders, Payments, and Expiration for a
JetStream-enabled server. Set Identity's `SMTP_HOST`, `SMTP_PORT`, and
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

Copy templates outside the repository:

```sh
for service in tickets identity orders payments expiration; do
  cp "infra/k8s/$service/examples/secret.example.yaml" "/tmp/$service-secret.yaml"
done
```

Set each database Secret's `POSTGRES_PASSWORD` to the raw password and put the
same password, URL-encoded, into `DATABASE_URL`. Keep the database Service names
and port `5432` from the examples. Configure Expiration's reachable Redis address
and a random `BETTER_AUTH_SECRET` at least 32 characters long. If reusing a Secret
copy from the earlier external-database setup, add `POSTGRES_PASSWORD` and update
its `DATABASE_URL` before applying it. Do not commit credentials.

Apply Secrets first, then the database manifests, and wait for all databases
before applying application manifests:

```sh
for service in tickets identity orders payments expiration; do
  kubectl apply -f "/tmp/$service-secret.yaml"
done

kubectl apply -f infra/k8s/identity-db
kubectl apply -f infra/k8s/tickets-db
kubectl apply -f infra/k8s/orders-db
kubectl apply -f infra/k8s/payments-db

for database in identity-db tickets-db orders-db payments-db; do
  kubectl rollout status "deployment/$database" --timeout=600s
done

kubectl apply -f infra/k8s/tickets
kubectl apply -f infra/k8s/identity
kubectl apply -f infra/k8s/orders
kubectl apply -f infra/k8s/payments
kubectl apply -f infra/k8s/expiration
kubectl apply -f infra/k8s/api-gateway

for service in tickets identity orders payments expiration api-gateway; do
  kubectl rollout status "deployment/$service" --timeout=600s
done
```

Directory application is nonrecursive and excludes example Secrets. Avoid `-R`,
which would apply placeholders. Manifests are namespace-neutral; use the same
`-n <namespace>` on every command if selecting a namespace other than the current
one.

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
`/metrics`. Backend gRPC ports do not serve HTTP. No ingress, NodePort, or
LoadBalancer is created here.

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

Concert Assistant and the remaining infrastructure manifests (including NATS,
Redis, SMTP, ingress, and observability backends) remain deferred. Existing
Compose workflows remain available independently.
