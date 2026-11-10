#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
[[ "$target" == minikube ]] || fail 'Traefik routing is Minikube-only; use k8s-port-forward-api K8S_TARGET=floci.'
k8s_init

k8s_kubectl get namespace "$namespace" -o name
k8s_kubectl get crd gatewayclasses.gateway.networking.k8s.io gateways.gateway.networking.k8s.io httproutes.gateway.networking.k8s.io -o name
k8s_kubectl wait --for=condition=Accepted gatewayclass/traefik --timeout=600s
k8s_kubectl -n traefik get service traefik -o name
k8s_kubectl -n "$namespace" get service api-gateway -o name
k8s_kubectl -n traefik rollout status deployment/traefik --timeout=600s
k8s_kubectl -n "$namespace" rollout status deployment/api-gateway --timeout=600s

k8s_kubectl -n "$namespace" apply --server-side --dry-run=server -k "$repo_root/infra/k8s/gateway"
k8s_kubectl -n "$namespace" apply -k "$repo_root/infra/k8s/gateway"

gateway_ready() {
  local snapshot="$1" generation type status observed
  local accepted=false programmed=false
  generation="${snapshot%%$'\n'*}"
  [[ -n "$generation" ]] || return 1
  while IFS='|' read -r type status observed; do
    if [[ "$status" == True && "$observed" == "$generation" ]]; then
      case "$type" in
        Accepted) accepted=true ;;
        Programmed) programmed=true ;;
      esac
    fi
  done <<< "$snapshot"
  [[ "$accepted" == true && "$programmed" == true ]]
}

route_ready() {
  local snapshot="$1" generation type value observed parent_namespace section
  local valid_parent=false accepted=false resolved=false
  generation="${snapshot%%$'\n'*}"
  [[ -n "$generation" ]] || return 1
  while IFS='|' read -r type value observed parent_namespace section; do
    if [[ "$type" == parent ]]; then
      # Each parent has its own conditions; never combine different parents.
      valid_parent=false
      accepted=false
      resolved=false
      if [[ "$value" == traefik.io/gateway-controller &&
            "$observed" == ticketing-gateway &&
            ( -z "$parent_namespace" || "$parent_namespace" == "$namespace" ) &&
            "$section" == http ]]; then
        valid_parent=true
      fi
      continue
    fi
    if [[ "$valid_parent" == true && "$value" == True && "$observed" == "$generation" ]]; then
      case "$type" in
        Accepted) accepted=true ;;
        ResolvedRefs) resolved=true ;;
      esac
    fi
    if [[ "$accepted" == true && "$resolved" == true ]]; then
      return 0
    fi
  done <<< "$snapshot"
  return 1
}

deadline=$(( $(date +%s) + 600 ))
while true; do
  # A snapshot pairs each resource's generation with its controller conditions.
  gateway_snapshot="$(k8s_kubectl -n "$namespace" get gateway ticketing-gateway -o 'jsonpath={.metadata.generation}{"\n"}{range .status.conditions[*]}{.type}{"|"}{.status}{"|"}{.observedGeneration}{"\n"}{end}')"
  route_snapshot="$(k8s_kubectl -n "$namespace" get httproute ticketing-api -o 'jsonpath={.metadata.generation}{"\n"}{range .status.parents[*]}{"parent|"}{.controllerName}{"|"}{.parentRef.name}{"|"}{.parentRef.namespace}{"|"}{.parentRef.sectionName}{"\n"}{range .conditions[*]}{.type}{"|"}{.status}{"|"}{.observedGeneration}{"\n"}{end}{end}')"
  if gateway_ready "$gateway_snapshot" && route_ready "$route_snapshot"; then
    break
  fi
  if (( $(date +%s) >= deadline )); then
    printf 'Error: Gateway and HTTPRoute did not reconcile within 600 seconds.\n' >&2
    k8s_kubectl -n "$namespace" get gateway ticketing-gateway -o yaml >&2 || true
    k8s_kubectl -n "$namespace" get httproute ticketing-api -o yaml >&2 || true
    exit 1
  fi
  sleep 2
done

printf 'Gateway and HTTPRoute reconciled in namespace %s.\n' "$namespace"
