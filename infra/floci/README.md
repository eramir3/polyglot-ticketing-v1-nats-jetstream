# Local Floci runtime

Floci is the local AWS emulator for the planned Terraform/EKS deployment.
This setup starts only the emulator; Terraform, cluster creation, and application
deployment are subsequent steps. Existing Compose and minikube workflows remain
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
including the future k3s-backed EKS cluster. This gives Floci control over the
local Docker daemon; use a trusted image and a development daemon. The socket
must be available at `/var/run/docker.sock` to the container.

If startup fails, check Docker availability, image pull errors, socket access,
and whether port 4566 is occupied. Inspect `make floci-logs` and rerun
`make floci-health`. No Redis or database proxy ports are published.

Terraform/EKS will be added separately, including cluster endpoint networking,
image loading, storage, and deployment validation. A healthy Floci endpoint does
not imply an EKS cluster exists or validate AWS production behavior.
