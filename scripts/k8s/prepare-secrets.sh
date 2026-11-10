#!/usr/bin/env bash
set -euo pipefail
umask 077

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

# Check every input before preparing any files.
for service in "${secret_services[@]}"; do
  template="$repo_root/infra/k8s/$service/examples/secret.example.yaml"
  [[ -f "$template" ]] || fail "Missing template: $template"
done

[[ ! -L "$destination" ]] || fail "Destination must not be a symlink: $destination"
if [[ -e "$destination" && ! -d "$destination" ]]; then
  fail "Destination must be a directory: $destination"
fi
for service in "${secret_services[@]}"; do
  secret_path="$destination/$service-secret.yaml"
  [[ ! -L "$secret_path" ]] || fail "Secret file must not be a symlink: $secret_path"
  if [[ -e "$secret_path" && ! -f "$secret_path" ]]; then
    fail "Secret path must be a regular file: $secret_path"
  fi
done

mkdir -p "$destination"
chmod 700 "$destination"
shared_secret=''
for service in "${secret_services[@]}"; do
  secret_path="$destination/$service-secret.yaml"
  if [[ -e "$secret_path" ]]; then
    chmod 600 "$secret_path"
    printf 'Skipped existing file: %s\n' "$secret_path"
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
      "$repo_root/infra/k8s/$service/examples/secret.example.yaml" > "$secret_path")
    chmod 600 "$secret_path"
    printf 'Created: %s\n' "$secret_path"
  fi
done

printf 'New files contain generated credentials. Existing files were preserved.\n'
