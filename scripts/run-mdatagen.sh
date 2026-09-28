#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${repo_root}/scripts/check-collector-tool-version.sh" mdatagen go.opentelemetry.io/collector/cmd/mdatagen
mdatagen "$@"
"${repo_root}/scripts/add-go-license-headers.sh"
"${repo_root}/scripts/generate-metadata.sh" normalize
