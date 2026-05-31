#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

copyright='Copyright 2026 Pocket Grimoire Guild contributors'
spdx='SPDX-License-Identifier: Apache-2.0'

while IFS= read -r file; do
  existing_header="$(head -n 5 "${file}")"
  if grep -q "${copyright}" <<<"${existing_header}" && grep -q "${spdx}" <<<"${existing_header}"; then
    continue
  fi

  tmp_file="$(mktemp)"
  {
    printf '// %s\n' "${copyright}"
    printf '// %s\n\n' "${spdx}"
    cat "${file}"
  } >"${tmp_file}"
  mv "${tmp_file}" "${file}"
done < <(git ls-files '*.go')
