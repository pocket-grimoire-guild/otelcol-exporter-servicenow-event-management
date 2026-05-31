#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
if [[ -d "${repo_root}/.tools/bin" ]]; then
  export PATH="${repo_root}/.tools/bin:${PATH}"
fi
otel_collector_version="${OTEL_COLLECTOR_VERSION:-v0.153.0}"

coverage_tmp=""
cleanup() {
  if [[ -n "${coverage_tmp}" ]]; then
    rm -f "${coverage_tmp}"
  fi
}
trap cleanup EXIT

log() {
  printf '==> %s\n' "$*"
}

skip() {
  printf 'skip: %s\n' "$*"
}

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

if command -v yq >/dev/null 2>&1; then
  log "validate yaml files"
  yq . builder-config.yaml .github/ISSUE_TEMPLATE/*.yml .github/workflows/*.yml docs/otel-registry-entry.yml >/dev/null
  yq . examples/*.yaml >/dev/null
else
  skip "yq not installed"
fi

log "validate shell scripts"
for script in scripts/*.sh; do
  bash -n "${script}"
done

if command -v shellcheck >/dev/null 2>&1; then
  log "shellcheck scripts"
  shellcheck scripts/*.sh
else
  skip "shellcheck not installed"
fi

./scripts/check-docs.sh

log "check required public files"
required_files=(
  ARCHITECTURE.md
  CHANGELOG.md
  CONTRIBUTING.md
  LICENSE
  README.md
  SECURITY.md
  SUPPORT.md
  docs/compatibility.md
  docs/configuration.md
  docs/evals.md
  docs/load-testing.md
  docs/mid-business-rules.md
  docs/production-hardening.md
  docs/release.md
  docs/servicenow-real-instance-testing.md
  docs/troubleshooting.md
  docs/validation-evidence/README.md
  docs/validation-evidence/2026-05-pdi-validation.md
  docs/product-specs/servicenow-event-management-exporter.md
  docs/otel-registry-entry.yml
  docs/references/source-map.md
  builder-config.yaml
  examples/collector-builder.yaml
  examples/collector-builder-metrics-event.yaml
  examples/servicenow-event-management-exporter-mid.yaml
  examples/servicenow-event-management-metrics-event-oauth.yaml
  scripts/check-docs.sh
)
for file in "${required_files[@]}"; do
  [[ -f "${file}" ]] || {
    printf 'missing required file: %s\n' "${file}" >&2
    exit 1
  }
done

log "check focused repository context"
if git grep -n -E -- 'jupyter|streamlit|reverse-engineer|git-lfs|libreoffice|pandoc|tesseract|wkhtmltopdf' \
  .gitignore Makefile scripts ':(exclude)scripts/verify.sh' >/tmp/servicenow-event-management-stale-check.txt; then
  cat /tmp/servicenow-event-management-stale-check.txt >&2
  printf 'unexpected inherited tooling reference found\n' >&2
  exit 1
fi
rm -f /tmp/servicenow-event-management-stale-check.txt

log "check public module path"
prototype_module_pattern='create-otel-snow''-em-exporter'
if git grep -n -E -- "${prototype_module_pattern}" \
  . ':(exclude)otelcol-servicenow-event-management-dev/**' ':(exclude)go.sum' >/tmp/servicenow-event-management-module-path-check.txt; then
  cat /tmp/servicenow-event-management-module-path-check.txt >&2
  printf 'prototype module path found\n' >&2
  exit 1
fi
rm -f /tmp/servicenow-event-management-module-path-check.txt

log "check Go license headers"
missing_license=()
while IFS= read -r file; do
  if ! head -n 5 "${file}" | grep -q 'Copyright 2026 Pocket Grimoire Guild contributors'; then
    missing_license+=("${file} (copyright)")
  fi
  if ! head -n 5 "${file}" | grep -q 'SPDX-License-Identifier: Apache-2.0'; then
    missing_license+=("${file} (spdx)")
  fi
done < <(git ls-files '*.go')
if (( ${#missing_license[@]} > 0 )); then
  printf 'Go files missing required license headers:\n' >&2
  printf '  %s\n' "${missing_license[@]}" >&2
  exit 1
fi

if find . -name metadata.yaml -print -quit | grep -q .; then
  log "check generated metadata"
  ./scripts/check-collector-tool-version.sh mdatagen go.opentelemetry.io/collector/cmd/mdatagen "${otel_collector_version}" >/dev/null
  go generate ./...
  if ! git diff --quiet -- \
    exporter/servicenoweventmanagementexporter/README.md \
    exporter/servicenoweventmanagementexporter/generated_component_test.go \
    exporter/servicenoweventmanagementexporter/generated_package_test.go \
    exporter/servicenoweventmanagementexporter/internal/metadata/generated_status.go; then
    git diff -- \
      exporter/servicenoweventmanagementexporter/README.md \
      exporter/servicenoweventmanagementexporter/generated_component_test.go \
      exporter/servicenoweventmanagementexporter/generated_package_test.go \
      exporter/servicenoweventmanagementexporter/internal/metadata/generated_status.go >&2
    fail "generated metadata is stale; run make generate"
  fi
fi

if command -v podman >/dev/null 2>&1; then
  log "podman is available"
  podman --version
else
  skip "podman not installed in current environment"
fi

if [[ -f go.mod ]]; then
  log "go module path"
  [[ "$(go list -m)" == "github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management" ]] || {
    printf 'unexpected module path: %s\n' "$(go list -m)" >&2
    exit 1
  }

  log "go test"
  go test ./...

  log "go coverage"
  coverage_tmp="$(mktemp)"
  go test ./exporter/servicenoweventmanagementexporter -coverprofile="${coverage_tmp}" >/dev/null
  coverage_total="$(go tool cover -func="${coverage_tmp}" | awk '/^total:/ { gsub(/%/, "", $3); print $3 }')"
  coverage_threshold="${SERVICENOW_EVENT_MANAGEMENT_COVERAGE_THRESHOLD:-90.0}"
  awk -v coverage="${coverage_total}" -v threshold="${coverage_threshold}" 'BEGIN { exit !(coverage + 0 >= threshold + 0) }' || {
    printf 'coverage %.1f%% is below required %.1f%%\n' "${coverage_total}" "${coverage_threshold}" >&2
    exit 1
  }
  printf 'coverage %.1f%% >= %.1f%%\n' "${coverage_total}" "${coverage_threshold}"

  log "go race test"
  go test -race ./...

  log "go vet"
  go vet ./...

  if command -v golangci-lint >/dev/null 2>&1; then
    log "golangci-lint"
    golangci-lint run ./...
  else
    skip "golangci-lint not installed"
  fi
else
  skip "go.mod not found"
fi

collector_built=false
if command -v builder >/dev/null 2>&1; then
  ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${otel_collector_version}" >/dev/null
  log "build collector with builder"
  builder --config builder-config.yaml
  collector_built=true
elif command -v ocb >/dev/null 2>&1; then
  ./scripts/check-collector-tool-version.sh ocb go.opentelemetry.io/collector/cmd/builder "${otel_collector_version}" >/dev/null
  log "build collector with ocb"
  ocb --config builder-config.yaml
  collector_built=true
else
  fail "collector builder not installed; run make install-tools"
fi

if [[ "${collector_built}" == "true" && -x ./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev ]]; then
  log "validate collector examples"
  SERVICENOW_INSTANCE_URL=https://example.service-now.com \
    SERVICENOW_USERNAME=user \
    SERVICENOW_PASSWORD=pass \
    ./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev validate --config examples/servicenow-event-management-exporter.yaml
  SERVICENOW_INSTANCE_URL=https://example.service-now.com \
    SERVICENOW_USERNAME=user \
    SERVICENOW_PASSWORD=pass \
    ./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev validate --config examples/servicenow-event-management-exporter-business-rules.yaml
  SERVICENOW_INSTANCE_URL=https://example.service-now.com \
    SERVICENOW_BEARER_TOKEN=token \
    ./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev validate --config examples/servicenow-event-management-exporter-bearer.yaml
  SERVICENOW_INSTANCE_URL=https://example.service-now.com \
    SERVICENOW_TOKEN_URL=https://example.service-now.com/oauth_token.do \
    SERVICENOW_CLIENT_ID=id \
    SERVICENOW_CLIENT_SECRET=secret \
    ./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev validate --config examples/servicenow-event-management-exporter-oauth.yaml
  SERVICENOW_MID_URL=https://mid.example.net:8443 \
    SERVICENOW_MID_API_KEY=key \
    ./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev validate --config examples/servicenow-event-management-exporter-mid.yaml
else
  skip "collector binary not available for example validation"
fi

log "verify complete"
