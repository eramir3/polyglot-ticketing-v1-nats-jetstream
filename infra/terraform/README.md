# Local Terraform/EKS foundation

This Terraform root provisions infrastructure exclusively through Floci 2.4.0.
It does not provision real AWS resources or deploy application workloads. The
existing Minikube scripts and application manifests remain independent.

## Requirements and first run

Use Docker 23+, Compose v2, Terraform 1.6–1.x, AWS CLI v2, Python 3, curl, and
kubectl compatible with Kubernetes 1.34. Docker must allow privileged containers.
Terraform declares Kubernetes minor version `1.34`; the Floci Compose configuration
overrides its image mapping with `rancher/k3s:v1.34.2-k3s1`. This patch contains an
upstream network-policy startup fix, but our changed-IP regression still fails
(see the recovery limitations below). Keep the runtime image
pin and Terraform minor version aligned. The default `m5.large`
capacity represents 2 CPUs and 8 GiB before Floci's Docker-host limits and
Kubernetes reservations; allow memory for the emulator and other local workloads.

Run from the repository root:

```sh
make floci-up
make floci-eks-init
make floci-eks-plan
make floci-eks-apply
make floci-eks-kubeconfig
make floci-eks-verify
```

Apply and destroy prompt for Terraform confirmation. For a saved, reviewable plan:

```sh
terraform -chdir=infra/terraform/environments/local plan -out=local.tfplan
terraform -chdir=infra/terraform/environments/local apply local.tfplan
```

State stays in `environments/local/terraform.tfstate`. Keep it and its backup
private: they contain the local operator's access key. State, plans, `.terraform`,
local variable files, and generated kubeconfig are ignored by Git. Commit the
provider lockfile. The provider is pinned to AWS 6.33.0; there is no remote backend
or Kubernetes provider.

## Infrastructure and files

| File in `environments/local` | Purpose |
| --- | --- |
| `versions.tf` | Terraform constraint and exact provider version |
| `providers.tf` | Literal localhost EC2, EKS, IAM, STS endpoints and dummy credentials |
| `variables.tf` | Cluster name, emulated region, Kubernetes version, VPC range |
| `networking.tf` | Dedicated VPC, two subnets, route table and associations |
| `iam.tf` | Cluster and worker roles, policy attachments, local operator user/key |
| `eks.tf` | EKS cluster and one managed node-group declaration |
| `outputs.tf` | Access/network metadata and sensitive operator credentials |
| `terraform.tfvars.example` | Optional local override example |

Defaults are cluster `polyglot-ticketing-local`, region `us-east-1`, VPC
`10.60.0.0/16`, and subnets `10.60.1.0/24` and `10.60.2.0/24` in two emulated
zones. Service CIDR is `10.100.0.0/16`; k3s uses pod CIDR `10.42.0.0/16`.
Check these against Docker networks, host routes, and VPNs before applying:

```sh
docker network inspect $(docker network ls -q) --format '{{.Name}} {{json .IPAM.Config}}'
netstat -rn
```

If needed, copy the example to `terraform.tfvars` and adjust the VPC range.
Subnets derive from it with eight additional prefix bits. Preserve nonoverlap
with service and pod ranges. This root is for local use only; do not repurpose
its endpoints or credentials for AWS.

VPC/subnets and IAM cluster policy attachment precede cluster creation; worker
policy attachments and the cluster precede node-group creation. Floci creates
and deletes its cluster security group. Terraform does not separately manage
that group. There are no standalone EC2 instances, internet/NAT gateways,
load balancers, AWS-managed databases, caches, or application AWS identities.
Docker supplies image-pull and outbound connectivity, so this does not validate
AWS private-subnet egress or security isolation.

## Kubernetes access and storage

`make floci-eks-kubeconfig` writes a mode-0600 JSON kubeconfig in the Terraform
local directory without changing `~/.kube/config` or the current Minikube context.
It contains local operator credentials and uses AWS CLI exec authentication with
the Floci endpoint. Certificate verification remains enabled. Commands are explicit:

```sh
kubectl --kubeconfig=infra/terraform/environments/local/kubeconfig.json get nodes
kubectl --kubeconfig=infra/terraform/environments/local/kubeconfig.json get storageclass
```

The operator key must be issued by Floci: its EKS webhook rejects the public
`test/test` and `floci/floci` pairs. Terraform uses `test/test` only for emulator
management. Ordinary IAM callers receive Floci's compatibility cluster-admin
mapping; the local operator has no attached AWS permissions policy. This is not
a least-privilege or real EKS authorization test.
Inherited AWS profile selection is removed by the kubeconfig exec command, and
AWS credential/config files are disabled for that command.

Floci is configured for real k3s mode, host endpoint mode, and the bundled
`local-path` default StorageClass. The Kubernetes API is published by Floci's
child container on an available host port in 6500–6599; no Compose mapping for
that range is needed. Inspect Docker's published address before using this on
a shared host; advertising `localhost` is not a firewall rule.

The executable verification waits up to three minutes for the authenticated
Kubernetes API to become ready after startup, then creates a unique temporary
namespace, checks node
readiness and DNS, and writes then reads a marker across two pods sharing one
PVC. It exercises default StorageClass selection and cleans up its namespace.
The test pulls `busybox:1.37.0` and requires outbound registry access. Data
persistence here means pod replacement, not cross-node recovery or high availability.

## Convergence, cleanup, and limitations

After verification, `make floci-eks-plan` must report no changes. To exercise
cleanup and recreation, run:

```sh
make floci-eks-destroy
make floci-eks-plan
make floci-eks-apply
make floci-eks-kubeconfig
make floci-eks-verify
make floci-eks-plan
```

Keep Floci running during destroy. Floci metadata persists in its Compose volume;
k3s uses a separate Docker data volume. Floci defaults to retaining child volumes
after cluster deletion. Inspect `docker volume ls` and volume labels before any
manual deletion; do not delete Minikube or application volumes. The default
cluster's retained volume is `floci-aws-eks-polyglot-ticketing-local`; it has no
ownership labels in the tested image, so confirm its name against the cluster
container's mounts before removal. Recreating the
same cluster name may reuse retained cluster data. Generated kubeconfig is stale
after destroy; regenerate it after apply because credentials/CA/ports may change.

Floci models one shared k3s node even with node-group desired/min/max all set to
one. Node-group declarations do not prove EC2 worker provisioning or multi-AZ
availability. Cluster/node-group update APIs are documented as unsupported;
avoid in-place version/scaling changes and review any replacement plan carefully.
Add-on declarations are metadata, not controller installation. Access policies
and security settings do not establish AWS-equivalent enforcement. IAM policy
enforcement is disabled in the default runtime.

Application deployment is separate from Terraform. The shared Kubernetes workflow
supports `K8S_TARGET=floci` while retaining Minikube as its default. It imports local
images into k3s containerd, applies existing manifests and uses direct gateway
port-forwarding. PostgreSQL, Redis, NATS JetStream, Mailpit and all six services
remain self-hosted. See the [application deployment guide](../k8s/README.md#choose-minikube-or-floci).
Optional routing and observability remain separate.

## Validation record

### Original foundation validation

Validated locally on 2026-11-10 with Terraform 1.16.1, AWS provider 6.33.0,
Floci 2.4.0, and Kubernetes/kubectl 1.34.1:

- Provider initialization, Terraform validation/format checks, and Compose validation passed.
- All 16 resources were created and a subsequent plan returned no changes.
- Issued IAM credentials authenticated with certificate verification; the default
  kubectl context remained `minikube` and generated kubeconfig permissions were 0600.
- Node readiness, in-pod DNS, default PVC binding, and marker persistence across
  pod replacement passed; temporary test namespaces were removed.
- All 16 resources were destroyed; the cluster container and VPC Docker network
  were removed, while `floci-aws-eks-polyglot-ticketing-local` was retained.
- Recreating the same configuration reused that volume. The Kubernetes checks
  passed again, and a final plan returned no changes. The recreated cluster is
  left running for the next phase.

These checks do not validate real AWS networking, least-privilege IAM,
multi-node scheduling, or application deployment.

### Recovery validation (2026-11-10)

- Took and verified offline archives of the original cluster and Floci metadata
  volumes, plus private copies of Terraform state and kubeconfig.
- Restored the same cluster data volume with `v1.34.2+k3s1` and regenerated the
  isolated mode-0600 kubeconfig. Added bounded API readiness polling to verification.
- Ordinary child-container restart and full Floci restart passed node readiness,
  in-pod DNS, default PVC binding, and data persistence across pod replacement.
- All temporary verification namespaces and IP-reservation containers were removed.
- All 16 Terraform resource IDs were preserved and the final detailed-exitcode
  plan returned 0 (no changes); the default kubectl context remained `minikube`.
- Forced Docker IP reassignment still failed on both `v1.34.2+k3s1` and
  `v1.34.13+k3s1`. The final runtime is `v1.34.2+k3s1` at its original address.
  This recovery is not a complete fix for the address-change case.

## Recovering from a node-IP startup failure

The original `v1.34.1+k3s1` runtime could fail after Docker reassigned the node IP:

```text
Failed to start networking: unable to initialize network policy controller:
error getting node subnet: failed to find interface with specified node ip
```

Floci can remain healthy while its child Kubernetes container has exited. Inspect
the child explicitly with `docker ps -a` and
`docker logs --tail 30 floci-aws-eks-polyglot-ticketing-local`.
The [k3s v1.34.2 release notes](https://docs.k3s.io/release-notes/v1.34.X#release-v1342k3s1)
include a fix for a fatal network-policy error when the node IP changes. However,
our local regression still reproduced this failure with both `v1.34.2+k3s1` and
`v1.34.13+k3s1`. The image upgrade alone is not a complete fix in this setup.

For the default cluster, the following procedure patches the runtime and restores
its data. It does not guarantee recovery if Docker reassigns the node IP:

1. Stop Floci with `docker compose -f infra/floci/docker-compose.yml stop floci`.
   Its shutdown normally removes the child container and retains its volume.
2. Take private offline backups of `floci-aws-eks-polyglot-ticketing-local`,
   `polyglot-ticketing-floci_floci-data`, and local Terraform state before upgrading.
   They contain cluster data and local credentials. Confirm no running containers
   use either volume before archiving them; verify the archives before proceeding.
3. Ensure Compose pins `FLOCI_SERVICES_EKS_IMAGE_TEMPLATE` to
   `rancher/k3s:v1.34.2-k3s1`. If the failed child container still exists, remove
   only that stopped container with `docker rm floci-aws-eks-polyglot-ticketing-local`.
   Retain all volumes; do not use `-v`, destroy Terraform resources, or reset data.
4. Run `make floci-up`. Floci restores the persisted cluster record and creates a
   new k3s container using the retained data volume and patched image.
5. Wait for the restored cluster to be ACTIVE and its API reachable, then run
   `make floci-eks-kubeconfig`, `make floci-eks-verify`, and `make floci-eks-plan`.
   Floci's own health check can pass before Kubernetes finishes starting.

Confirm the child image with
`docker inspect --type container floci-aws-eks-polyglot-ticketing-local --format '{{.Config.Image}}'`.
Floci may record an image digest here; confirm the runtime version with
`docker exec floci-aws-eks-polyglot-ticketing-local k3s --version`.
Keep network-policy enforcement enabled. A surviving child container is adopted
as-is by Floci, so changing the image override alone does not patch it. Terraform
does not track this emulator runtime image and need not replace the EKS resource.

After recovery, validate both restart paths separately:

```sh
docker restart floci-aws-eks-polyglot-ticketing-local
# Wait for Kubernetes API readiness, then:
make floci-eks-verify

docker compose -f infra/floci/docker-compose.yml restart floci
# Wait for Floci and Kubernetes readiness, then:
make floci-eks-kubeconfig
make floci-eks-verify
make floci-eks-plan
```

The node must remain Ready, DNS and PVC persistence checks must pass, and the
final Terraform plan must report no changes. These restarts leave the default
Minikube context and all application manifests untouched.

### Remaining restart limitation

During the controlled regression, a temporary container reserved the previous
bridge address so the node changed from `172.17.0.3` to `172.17.0.4`. Both tested
patch versions briefly exposed the Kubernetes API and then shut down with the
same network-policy interface error. A brief API response or a retained Ready
condition is insufficient evidence of successful recovery. Temporary reservation
containers were removed after each test.

Runtime replacement also exposed a k3s node-password mismatch. The local node
registration and its password secret were backed up and re-created without
deleting PVCs or datastore volumes. This cleared the password warning for that
container, but did not fix the changed-IP failure. See
[k3s node registration](https://docs.k3s.io/architecture#how-agent-node-registration-works).
Do not automate node deletion as a general fix for this networking error.
The password warning returned after the final full Floci restart, which replaced
the child container again. Node readiness, DNS, and PVC checks passed despite
that warning; persistence of the node password across replacement remains a
separate compatibility gap.

The default remains the originally planned `v1.34.2+k3s1` runtime. Stable node
networking or a verified upstream fix remains necessary before claiming resilience
to Docker address reassignment. Network-policy enforcement remains enabled;
Terraform resources, application manifests, and Minikube scripts are unchanged.

Sources: [Floci 2.4.0 EKS](https://github.com/floci-io/floci/blob/2.4.0/docs/services/eks.md),
[IAM](https://github.com/floci-io/floci/blob/2.4.0/docs/services/iam.md),
[EC2](https://github.com/floci-io/floci/blob/2.4.0/docs/services/ec2.md), and
[AWS provider custom endpoints](https://github.com/hashicorp/terraform-provider-aws/blob/v6.33.0/website/docs/guides/custom-service-endpoints.html.markdown).
