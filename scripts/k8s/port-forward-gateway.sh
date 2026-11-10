#!/usr/bin/env bash
set -euo pipefail

profile="${MINIKUBE_PROFILE:-minikube}"

command -v kubectl >/dev/null 2>&1 || { printf 'Error: kubectl is required.\n' >&2; exit 1; }
context="$(kubectl config current-context)"
if [[ "$context" != "$profile" ]]; then
  printf "Error: Current kubectl context '%s' does not match minikube profile '%s'. Select the matching context or set MINIKUBE_PROFILE.\n" "$context" "$profile" >&2
  exit 1
fi

exec kubectl -n traefik port-forward service/traefik 3000:80
