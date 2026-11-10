#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
destination="${K8S_SECRETS_DIR:-/tmp/k8s-polyglot-ticketing-secrets}"
services=(identity tickets orders payments expiration)

fail() {
  printf 'Error: %s\n' "$1" >&2
  exit 1
}

# Check every input before preparing any files.
for service in "${services[@]}"; do
  template="$repo_root/infra/k8s/$service/examples/secret.example.yaml"
  [[ -f "$template" ]] || fail "Missing template: $template"
done

[[ ! -L "$destination" ]] || fail "Destination must not be a symlink: $destination"
if [[ -e "$destination" && ! -d "$destination" ]]; then
  fail "Destination must be a directory: $destination"
fi
for service in "${services[@]}"; do
  target="$destination/$service-secret.yaml"
  [[ ! -L "$target" ]] || fail "Secret file must not be a symlink: $target"
  if [[ -e "$target" && ! -f "$target" ]]; then
    fail "Secret path must be a regular file: $target"
  fi
done

mkdir -p "$destination"
chmod 700 "$destination"
shared_secret=''
for service in "${services[@]}"; do
  target="$destination/$service-secret.yaml"
  if [[ -e "$target" ]]; then
    chmod 600 "$target"
    printf 'Skipped existing file: %s\n' "$target"
  else
    if [[ -z "$shared_secret" ]]; then
      command -v openssl >/dev/null 2>&1 || fail "openssl is required to generate credentials."
      shared_secret="$(openssl rand -hex 32)"
    fi
    # Noclobber preserves a file created since the existence check.
    (set -C; sed \
      -e "s/REPLACE_WITH_URL_ENCODED_PASSWORD/$shared_secret/g" \
      -e "s/REPLACE_WITH_PASSWORD/$shared_secret/g" \
      -e "s/REPLACE_WITH_A_RANDOM_SECRET_AT_LEAST_32_CHARACTERS_LONG/$shared_secret/g" \
      "$repo_root/infra/k8s/$service/examples/secret.example.yaml" > "$target")
    chmod 600 "$target"
    printf 'Created: %s\n' "$target"
  fi
done

printf 'New files contain generated credentials. Existing files were preserved.\n'
