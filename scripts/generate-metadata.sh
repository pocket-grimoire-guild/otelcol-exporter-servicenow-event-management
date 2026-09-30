#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

normalize_generated_go() {
  local file temp_file
  for file in \
    "${repo_root}"/generated_*.go \
    "${repo_root}"/internal/metadata/generated_*.go \
    "${repo_root}"/internal/metadatatest/generated_*.go; do
    [[ -f "${file}" ]] || continue
    if ! head -n 5 "${file}" | grep -q 'Copyright 2026 Pocket Grimoire Guild contributors' || \
      ! head -n 5 "${file}" | grep -q 'SPDX-License-Identifier: Apache-2.0'; then
      temp_file="${file}.header.tmp"
      {
        printf '%s\n' '// Copyright 2026 Pocket Grimoire Guild contributors' '// SPDX-License-Identifier: Apache-2.0' ''
        cat "${file}"
      } > "${temp_file}"
      mv "${temp_file}" "${file}"
    fi
    gofmt -w "${file}"
  done
}

if [[ "$#" -ne 1 || "$1" != "normalize" ]]; then
  echo "usage: $0 normalize (run go generate through scripts/run-mdatagen.sh)" >&2
  exit 2
fi

normalize_generated_go
