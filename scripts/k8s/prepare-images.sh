#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

for tool in docker make pnpm; do require_tool "$tool"; done
k8s_init
docker info >/dev/null
if [[ "$target" == minikube ]]; then
  require_tool minikube
  minikube -p "$profile" status
else
  floci_find_container
  architecture="$(k8s_kubectl get nodes -o jsonpath='{.items[*].status.nodeInfo.architecture}')"
  [[ "$architecture" == amd64 || "$architecture" == arm64 ]] || fail 'Expected one Floci node with amd64 or arm64 architecture.'
fi

make -C "$repo_root" generate-proto
for service in "${services[@]}"; do
  printf 'Building %s:dev.\n' "$service"
  if [[ "$target" == floci ]]; then
    docker build --platform "linux/$architecture" -f "$repo_root/apps/$service/Dockerfile.dev" -t "$service:dev" "$repo_root"
  else
    docker build -f "$repo_root/apps/$service/Dockerfile.dev" -t "$service:dev" "$repo_root"
  fi
done

# Load only after all builds succeed. Pipefail propagates either save or import failures.
for service in "${services[@]}"; do
  if [[ "$target" == minikube ]]; then
    printf 'Loading %s:dev into minikube profile %s.\n' "$service" "$profile"
    minikube -p "$profile" image load --daemon --overwrite=true "$service:dev"
  else
    built_arch="$(docker image inspect "$service:dev" --format '{{.Architecture}}')"
    [[ "$built_arch" == "$architecture" ]] || fail "Image $service:dev architecture does not match the Floci node."
    printf 'Importing %s:dev into Floci containerd.\n' "$service"
    docker save "$service:dev" | docker exec -i "$floci_container" ctr --namespace k8s.io images import --platform "linux/$architecture" -
  fi
done
if [[ "$target" == floci ]]; then
  imported="$(docker exec "$floci_container" ctr --namespace k8s.io images list -q)"
  for service in "${services[@]}"; do
    printf '%s\n' "$imported" | grep -Fx "docker.io/library/$service:dev" >/dev/null || fail "Imported image $service:dev was not found in containerd."
  done
fi
printf 'All six application images are available in %s.\n' "$target"
