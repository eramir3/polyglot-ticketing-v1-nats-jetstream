#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
[[ "$target" == minikube ]] || fail 'Traefik routing is Minikube-only; use k8s-port-forward-api K8S_TARGET=floci.'
k8s_init

exec "${kubectl_command[@]}" -n traefik port-forward service/traefik 3000:80
