#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
k8s_init
k8s_kubectl -n "$namespace" rollout status deployment/api-gateway --timeout=600s
exec "${kubectl_command[@]}" -n "$namespace" port-forward --address localhost service/api-gateway 3000:3000
