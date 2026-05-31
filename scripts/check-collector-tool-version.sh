#!/usr/bin/env bash
set -euo pipefail

tool="${1:-}"
module="${2:-}"
expected="${3:-${OTEL_COLLECTOR_VERSION:-v0.153.0}}"

if [[ -z "${tool}" || -z "${module}" ]]; then
  printf 'usage: %s <tool> <go-module> [expected-version]\n' "$0" >&2
  exit 2
fi

tool_path="$(command -v "${tool}" || true)"
if [[ -z "${tool_path}" ]]; then
  printf '%s not found; run make install-tools\n' "${tool}" >&2
  exit 1
fi

version="$(go version -m "${tool_path}" | awk -v module="${module}" '$1 == "mod" && $2 == module { print $3 }')"
if [[ -z "${version}" ]]; then
  printf '%s at %s is not built from %s\n' "${tool}" "${tool_path}" "${module}" >&2
  exit 1
fi

if [[ "${version}" != "${expected}" ]]; then
  printf '%s at %s is %s, expected %s; run make install-tools\n' "${tool}" "${tool_path}" "${version}" "${expected}" >&2
  exit 1
fi

printf '%s %s (%s)\n' "${tool}" "${version}" "${tool_path}"
