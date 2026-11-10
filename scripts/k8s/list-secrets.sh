#!/usr/bin/env bash
set -euo pipefail

exec kubectl get secrets -n "${K8S_NAMESPACE:-polyglot-ticketing}"
