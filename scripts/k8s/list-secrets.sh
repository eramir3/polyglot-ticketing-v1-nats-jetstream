#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
k8s_init
k8s_kubectl get secrets -n "$namespace"
