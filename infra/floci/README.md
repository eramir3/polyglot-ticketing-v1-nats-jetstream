# Local Floci runtime

Floci is the local AWS emulator for Terraform/EKS development.
This Compose setup starts only the emulator. The [Terraform foundation](../terraform/README.md)
provisions a separate local cluster; application deployment is a subsequent step.
Existing Compose and minikube workflows remain
independent. PostgreSQL, NATS JetStream, BullMQ/Redis, Better Auth, and simulated
payments retain their current roles.

Requirements: Docker 23+ with a running daemon, Docker Compose v2, and host curl.
Run commands from the repository root; no application `.env` values are needed:

```sh
make floci-up
make floci-health
make floci-ps
make floci-logs
make floci-down
```

The AWS API is available at http://127.0.0.1:4566. The image is pinned to
`floci/floci:2.4.0`. Startup waits for `/_floci/health` with bounded retries;
on timeout the container remains available for log inspection.

The dedicated `polyglot-ticketing-floci` Compose project and network isolate
this runtime from the application stack. Metadata is persisted in the project's
`floci-data` named volume at `/app/data`; `floci-down` retains it. Removing that
volume deletes emulator state. Application reset commands do not manage it.

The Docker socket is mounted so Floci can create Docker-backed resources,
including the k3s-backed EKS cluster. This gives Floci control over the
local Docker daemon; use a trusted image and a development daemon. The socket
must be available at `/var/run/docker.sock` to the container.

If startup fails, check Docker availability, image pull errors, socket access,
and whether port 4566 is occupied. Inspect `make floci-logs` and rerun
`make floci-health`. No Redis or database proxy ports are published.

EKS real mode, host endpoint mode, and the bundled local-path StorageClass are
explicitly enabled for Terraform provisioning. Floci child containers publish
Kubernetes API ports separately and retain their own data volumes. Destroy
Terraform-managed resources while Floci is running when removing infrastructure;
ordinary restarts retain the cluster data.
The Kubernetes 1.34 runtime is pinned to `rancher/k3s:v1.34.2-k3s1` through
`FLOCI_SERVICES_EKS_IMAGE_TEMPLATE`. This overrides Floci 2.4.0's older mapping,
which can crash after Docker changes the node IP on restart. The patch contains
an upstream fix, but our controlled address-change test still failed; ordinary
startup at the original address is not proof that this case is fixed. Keep this image pin
aligned with Terraform's Kubernetes minor version. See the
[recovery procedure](../terraform/README.md#recovering-from-a-node-ip-startup-failure)
for existing clusters; changing the setting does not upgrade a surviving child
container automatically.
A healthy Floci endpoint does not imply an EKS cluster exists or validate AWS
production behavior. See the Terraform guide for access, verification, cleanup,
and emulator limitations. For application image loading, Secrets, deployment and
direct local access, use the [shared Kubernetes workflow](../k8s/README.md#choose-minikube-or-floci)
with `K8S_TARGET=floci`. Minikube remains the default target.
