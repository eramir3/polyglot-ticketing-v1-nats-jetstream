# Local Kubernetes

These manifests deploy Tickets, Identity, Orders, Payments, Expiration, and the
API Gateway to a local Kubernetes cluster. Each workload has one replica and an
internal ClusterIP Service. Use the same namespace for all six services.
PostgreSQL, NATS JetStream, Redis, SMTP, ingress, and observability backends are
supplied separately.

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

Each service directory contains a `configmap.yaml` for nonsecret configuration.
Secret templates live under `examples/`. Database users must be able to apply
migrations to their own service's database.

| Workload | External dependencies | Example host ports |
| --- | --- | --- |
| Tickets | Tickets PostgreSQL, NATS JetStream | `5433`, `4222` |
| Identity | Identity PostgreSQL, SMTP | `5432`, `1025` |
| Orders | Orders PostgreSQL, NATS JetStream | `5434`, `4222` |
| Payments | Payments PostgreSQL, NATS JetStream | `5435`, `4222` |
| Expiration | NATS JetStream, its own Redis data | `4222`, `6379` |
| API Gateway | Backend Kubernetes Services | No external persistence |

Examples use `host.docker.internal` for host dependencies on Docker Desktop.
Replace it with an address reachable from your cluster as needed; minikube also
provides `host.minikube.internal` for reaching its host. Verify connectivity for
your driver. Pod-local `localhost` cannot reach host services. PostgreSQL example
ports match Compose's published ports. Compose Mailpit exposes SMTP only inside
its network; arrange cluster-reachable SMTP separately if using it.

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

Edit the copies with reachable addresses, URL-encoded database passwords, and a
random `BETTER_AUTH_SECRET` at least 32 characters long. Do not commit credentials.
Apply the Secrets and each workload directory:

```sh
for service in tickets identity orders payments expiration; do
  kubectl apply -f "/tmp/$service-secret.yaml"
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
job scheduling. These workflow checks require all external dependencies.

To inspect another service's metrics, stop the gateway forward and run, for
example, `kubectl port-forward service/expiration 9090:9090`, then fetch
`/metrics`. Backend gRPC ports do not serve HTTP. No ingress, NodePort, or
LoadBalancer is created here.

TCP probes verify listener availability, not continued database, broker, Redis,
or SMTP health. The five newer workloads allow five minutes for startup before
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

Concert Assistant, infrastructure manifests, ingress, and observability backends
remain deferred. Existing Compose workflows remain available independently.
