#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
profile="${MINIKUBE_PROFILE:-minikube}"
services=(tickets identity orders payments expiration api-gateway)

fail() {
  printf 'Error: %s\n' "$1" >&2
  exit 1
}

for tool in docker minikube kubectl make pnpm; do
  command -v "$tool" >/dev/null 2>&1 || fail "$tool is required."
done

context="$(kubectl config current-context)"
[[ "$context" == "$profile" ]] || fail "Current kubectl context '$context' does not match minikube profile '$profile'. Select the matching context or set MINIKUBE_PROFILE."
docker info >/dev/null
minikube -p "$profile" status

make -C "$repo_root" generate-proto

for service in "${services[@]}"; do
  printf 'Building %s:dev.\n' "$service"
  docker build -f "$repo_root/apps/$service/Dockerfile.dev" -t "$service:dev" "$repo_root"
done

for service in "${services[@]}"; do
  printf 'Loading %s:dev into minikube profile %s.\n' "$service" "$profile"
  minikube -p "$profile" image load --daemon --overwrite=true "$service:dev"
done

printf 'All six application images are loaded into minikube profile %s.\n' "$profile"
