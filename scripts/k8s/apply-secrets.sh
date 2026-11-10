#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
require_tool python3
require_tool kubectl
k8s_init
python3 "$repo_root/scripts/k8s/validate-secrets.py" "$destination" "$namespace" "${kubectl_command[@]}"
k8s_kubectl create namespace "$namespace" --dry-run=client -o yaml | k8s_kubectl apply -f -
for service in "${secret_services[@]}"; do
  k8s_kubectl -n "$namespace" apply -f "$destination/$service-secret.yaml"
done
