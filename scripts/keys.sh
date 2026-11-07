#!/usr/bin/env bash
# Creates .env from .env.example and fills in missing secrets.
# Existing non-empty values are never overwritten and values are never printed.
set -euo pipefail

env_file="${1:-.env}"
example_file="${2:-.env.example}"
secrets=(TOTP_ENC_KEY PASSWORD_PEPPER)
# Filled only when the file lists them empty. Hex, so a connection string can hold them unescaped.
hex_secrets=(POSTGRES_PASSWORD)

command -v openssl >/dev/null 2>&1 || {
  echo "keys: openssl is required" >&2
  exit 1
}

if [[ ! -e "$env_file" ]]; then
  (umask 077 && cp "$example_file" "$env_file")
  echo "keys: created $env_file from $example_file"
fi

current_value() {
  awk -v key="$1" 'index($0, key "=") == 1 { value = substr($0, length(key) + 2) } END { print value }' "$env_file"
}

set_value() {
  local key="$1" value="$2" tmp
  tmp="$(mktemp "$env_file.XXXXXX")"
  trap 'rm -f "$tmp"' EXIT
  chmod 600 "$tmp"
  VALUE="$value" awk -v key="$key" '
    index($0, key "=") == 1 { if (!done) print key "=" ENVIRON["VALUE"]; done = 1; next }
    { print }
    END { if (!done) print key "=" ENVIRON["VALUE"] }
  ' "$env_file" >"$tmp"
  mv "$tmp" "$env_file"
  trap - EXIT
}

for key in "${secrets[@]}"; do
  if [[ -n "$(current_value "$key")" ]]; then
    echo "keys: $key already set, keeping it"
  else
    set_value "$key" "$(openssl rand -base64 32)"
    echo "keys: generated $key"
  fi
done

for key in "${hex_secrets[@]}"; do
  if ! grep -q "^$key=" "$env_file"; then
    continue
  elif [[ -n "$(current_value "$key")" ]]; then
    echo "keys: $key already set, keeping it"
  else
    set_value "$key" "$(openssl rand -hex 32)"
    echo "keys: generated $key"
  fi
done
