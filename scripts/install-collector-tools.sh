#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${OTEL_COLLECTOR_VERSION:-v0.153.0}"
gobin="${GOBIN:-$(go env GOPATH)/bin}"
tmp_dir=""

cleanup() {
  if [[ -n "${tmp_dir}" ]]; then
    rm -rf "${tmp_dir}"
  fi
}
trap cleanup EXIT

mkdir -p "${gobin}"

printf 'installing Collector builder %s into %s\n' "${version}" "${gobin}"
GOBIN="${gobin}" go install "go.opentelemetry.io/collector/cmd/builder@${version}"

tmp_dir="$(mktemp -d)"
printf 'installing Collector mdatagen %s into %s\n' "${version}" "${gobin}"
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "${version}" https://github.com/open-telemetry/opentelemetry-collector.git "${tmp_dir}/opentelemetry-collector"
(
  cd "${tmp_dir}/opentelemetry-collector/cmd/mdatagen"
  GOBIN="${gobin}" go install .
)

PATH="${gobin}:${PATH}" "${repo_root}/scripts/check-collector-tool-version.sh" builder go.opentelemetry.io/collector/cmd/builder "${version}" >/dev/null
PATH="${gobin}:${PATH}" "${repo_root}/scripts/check-collector-tool-version.sh" mdatagen go.opentelemetry.io/collector/cmd/mdatagen "${version}" >/dev/null

printf 'installed Collector tools %s\n' "${version}"
