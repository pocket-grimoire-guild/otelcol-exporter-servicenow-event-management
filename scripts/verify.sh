#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
if [[ -d "${repo_root}/.tools/bin" ]]; then
  export PATH="${repo_root}/.tools/bin:${PATH}"
fi
otel_collector_version="${OTEL_COLLECTOR_VERSION:-v0.153.0}"

smoke_tmp=""
coverage_tmp=""
generated_tmp=""
cleanup() {
  if [[ -n "${smoke_tmp}" ]]; then
    rm -rf "${smoke_tmp}"
  fi
  if [[ -n "${coverage_tmp}" ]]; then
    rm -f "${coverage_tmp}"
  fi
  if [[ -n "${generated_tmp}" ]]; then
    rm -rf "${generated_tmp}"
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

log "smoke generator dry-run"
smoke_tmp="$(mktemp -d)"
OTEL_SMOKE_DRY_RUN_DIR="${smoke_tmp}" \
  OTEL_SMOKE_RUN_ID="verify-field-coverage" \
  OTEL_SMOKE_BATCHES=4 \
  OTEL_SMOKE_TOTAL_EVENTS=15 \
  OTEL_SMOKE_BATCH_DELAY=0 \
  OTEL_SMOKE_TIME_WINDOW_SECONDS=1800 \
  ./scripts/send-otel-rich-log-batches.sh >/dev/null
jq -s -e '
  def has_attr($k): [.attributes[] | select(.key == $k)] | length == 1;
  def scalar($v):
    if $v.stringValue? != null then $v.stringValue
    elif $v.intValue? != null then $v.intValue
    elif $v.doubleValue? != null then ($v.doubleValue | tostring)
    elif $v.boolValue? != null then ($v.boolValue | tostring)
    else "" end;
  def attr_value($k): ([.attributes[] | select(.key == $k) | scalar(.value)] | first // "");
  def values($k): [.[].resourceLogs[0].scopeLogs[0].logRecords[] | attr_value($k)] | unique;
  [.[].resourceLogs[0].scopeLogs[0].logRecords | length] == [4, 4, 4, 3]
  and all(.[].resourceLogs[0].scopeLogs[0].logRecords[];
    has_attr("servicenow.source")
    and has_attr("servicenow.event_class")
    and has_attr("servicenow.node")
    and has_attr("servicenow.resource")
    and has_attr("servicenow.metric_name")
    and has_attr("servicenow.type")
    and has_attr("servicenow.message_key")
    and has_attr("servicenow.severity")
    and has_attr("servicenow.description")
    and has_attr("servicenow.resolution_state")
    and has_attr("test.run.id")
    and has_attr("test.scenario")
    and has_attr("test.auth.path")
    and has_attr("test.ui.state_hint")
    and has_attr("host.name")
    and has_attr("http.route")
    and has_attr("http.status_code")
    and has_attr("db.system")
    and has_attr("messaging.system")
    and has_attr("alert.priority")
    and has_attr("test.payload.kind")
    and has_attr("test.context")
    and has_attr("test.labels")
    and has_attr("test.bytes.marker")
  )
  and (values("servicenow.source") | length >= 8)
  and (values("servicenow.event_class") | length >= 10)
  and (values("servicenow.node") | length >= 15)
  and (values("servicenow.resource") | length >= 12)
  and (values("servicenow.metric_name") | length >= 15)
  and (values("servicenow.type") | length >= 12)
  and (values("servicenow.severity") | sort == ["0", "1", "2", "3", "4", "5"])
  and (values("servicenow.resolution_state") | sort == ["Closing", "New"])
  and (values("http.status_code") | length >= 13)
' "${smoke_tmp}"/batch-*.json >/dev/null
rm -rf "${smoke_tmp}"
smoke_tmp=""

log "HEC fan-out smoke dry-run"
smoke_tmp="$(mktemp -d)"
SERVICENOW_HEC_FANOUT_DRY_RUN=1 \
  SERVICENOW_HEC_FANOUT_WORK_DIR="${smoke_tmp}" \
  SERVICENOW_HEC_FANOUT_RUN_ID="verify-hec-fanout" \
  SERVICENOW_HEC_FANOUT_TOTAL_EVENTS=50 \
  SERVICENOW_HEC_FANOUT_PASS_EVENTS=10 \
  SERVICENOW_HEC_FANOUT_THRESHOLD=90 \
  ./scripts/run-hec-fanout-smoke.sh >"${smoke_tmp}/hec-fanout-dry-run.txt"
[[ -f "${smoke_tmp}/collector.yaml" ]]
[[ -f "${smoke_tmp}/builder-config.yaml" ]]
[[ -f "${smoke_tmp}/verify-hec-fanout-sent.tsv" ]]
[[ -f "${smoke_tmp}/verify-hec-fanout-servicenow.tsv" ]]
[[ -f "${smoke_tmp}/verify-hec-fanout-splunk.tsv" ]]
[[ "$(( $(wc -l <"${smoke_tmp}/verify-hec-fanout-sent.tsv") - 1 ))" == "50" ]]
[[ "$(( $(wc -l <"${smoke_tmp}/verify-hec-fanout-servicenow.tsv") - 1 ))" == "10" ]]
[[ "$(( $(wc -l <"${smoke_tmp}/verify-hec-fanout-splunk.tsv") - 1 ))" == "50" ]]
for pattern in 'splunk_hec' 'filter/servicenow_threshold' 'oauth2client/servicenow' 'sourcetype: "otel:servicenow-hec-fanout"'; do
  grep -n -F -- "${pattern}" "${smoke_tmp}/collector.yaml" >/dev/null
done
rm -rf "${smoke_tmp}"
smoke_tmp=""

log "metrics-derived event smoke dry-run"
smoke_tmp="$(mktemp -d)"
SERVICENOW_METRICS_EVENT_DRY_RUN=1 \
  SERVICENOW_METRICS_EVENT_WORK_DIR="${smoke_tmp}" \
  SERVICENOW_METRICS_EVENT_RUN_ID="verify-metrics-event" \
  ./scripts/run-metrics-event-smoke.sh >"${smoke_tmp}/metrics-event-dry-run.txt"
[[ -f "${smoke_tmp}/collector.yaml" ]]
[[ -f "${smoke_tmp}/builder-config.yaml" ]]
[[ -f "${smoke_tmp}/verify-metrics-event-metrics.json" ]]
[[ -f "${smoke_tmp}/verify-metrics-event-expected-servicenow.tsv" ]]
[[ "$(( $(wc -l <"${smoke_tmp}/verify-metrics-event-expected-servicenow.tsv") - 1 ))" == "2" ]]
for pattern in 'metricsaslogs' 'filter/metric_threshold' 'transform/servicenow_metric_event' 'oauth2client/servicenow' 'demo.checkout.error_rate'; do
  grep -n -F -- "${pattern}" "${smoke_tmp}/collector.yaml" >/dev/null
done
jq -e '.resourceMetrics[0].scopeMetrics[0].metrics[0].gauge.dataPoints | length == 3' "${smoke_tmp}/verify-metrics-event-metrics.json" >/dev/null
rm -rf "${smoke_tmp}"
smoke_tmp=""

log "check required public files"
required_files=(
  ARCHITECTURE.md
  CHANGELOG.md
  CONTRIBUTING.md
  LICENSE
  README.md
  SECURITY.md
  SUPPORT.md
  docs/contrib-readiness.md
  docs/compatibility.md
  docs/configuration.md
  docs/exporter-behavior.md
  docs/evals.md
  docs/load-testing.md
  docs/mid-business-rules.md
  docs/production-hardening.md
  docs/release.md
  docs/servicenow-real-instance-testing.md
  docs/troubleshooting.md
  docs/validation-evidence/README.md
  docs/validation-evidence/2026-05-pdi-validation.md
  docs/validation-evidence/2026-06-compression-validation.md
  docs/product-specs/servicenow-event-management-exporter.md
  docs/otel-registry-entry.yml
  docs/references/source-map.md
  builder-config.yaml
  examples/collector-builder.yaml
  examples/collector-builder-metrics-event.yaml
  examples/collector-builder-trace-exception.yaml
  examples/servicenow-event-management-log-context.yaml
  examples/servicenow-event-management-exporter-mid.yaml
  examples/servicenow-event-management-metrics-event-oauth.yaml
  examples/servicenow-event-management-trace-exception-oauth.yaml
  documentation.md
  doc.go
  metadata.yaml
  scripts/add-go-license-headers.sh
  scripts/check-docs.sh
  scripts/check-collector-tool-version.sh
  scripts/generate-metadata.sh
  scripts/run-mdatagen.sh
  scripts/run-hec-fanout-smoke.sh
  scripts/run-metrics-event-smoke.sh
  scripts/test-metrics-event.sh
  scripts/test-trace-exception.sh
  tests/metricsevent/example_test.go
  tests/traceexception/example_test.go
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

if [[ -e exporter/servicenoweventmanagementexporter ]]; then
  fail "former nested exporter package still exists; the v0.3.0 package lives at the module root"
fi

log "check Go license headers"
missing_license=()
while IFS= read -r file; do
  if ! head -n 5 "${file}" | grep -q 'Copyright 2026 Pocket Grimoire Guild contributors'; then
    missing_license+=("${file} (copyright)")
  fi
  if ! head -n 5 "${file}" | grep -q 'SPDX-License-Identifier: Apache-2.0'; then
    missing_license+=("${file} (spdx)")
  fi
done < <(git ls-files --cached --others --exclude-standard -- '*.go')
if (( ${#missing_license[@]} > 0 )); then
  printf 'Go files missing required license headers:\n' >&2
  printf '  %s\n' "${missing_license[@]}" >&2
  exit 1
fi

if [[ -f metadata.yaml ]]; then
  log "check generated metadata"
  ./scripts/check-collector-tool-version.sh mdatagen go.opentelemetry.io/collector/cmd/mdatagen "${otel_collector_version}" >/dev/null
  generated_tmp="$(mktemp -d)"
  mkdir -p "${generated_tmp}/before"
  {
    find . -maxdepth 1 -type f \( -name 'generated_*.go' -o -name documentation.md -o -name README.md \) -print0
    find internal -type f -name 'generated_*.go' -print0
  } | sort -z > "${generated_tmp}/before-paths.nul"
  mapfile -d '' -t generated_paths < "${generated_tmp}/before-paths.nul"
  printf '%s\n' "${generated_paths[@]}" > "${generated_tmp}/before-paths.txt"
  for file in "${generated_paths[@]}"; do
    cp --parents -p "${file}" "${generated_tmp}/before"
  done
  go generate ./...
  {
    find . -maxdepth 1 -type f \( -name 'generated_*.go' -o -name documentation.md -o -name README.md \) -print0
    find internal -type f -name 'generated_*.go' -print0
  } | sort -z > "${generated_tmp}/after-paths.nul"
  mapfile -d '' -t generated_paths_after < "${generated_tmp}/after-paths.nul"
  printf '%s\n' "${generated_paths_after[@]}" > "${generated_tmp}/after-paths.txt"
  if ! diff -u "${generated_tmp}/before-paths.txt" "${generated_tmp}/after-paths.txt"; then
    fail "generated metadata, documentation, telemetry, or metadata tests are stale; run make generate"
  fi
  for file in "${generated_paths_after[@]}"; do
    if [[ ! -f "${generated_tmp}/before/${file}" ]] || ! cmp -s "${generated_tmp}/before/${file}" "${file}"; then
      diff -u "${generated_tmp}/before/${file}" "${file}" || true
      fail "generated metadata, documentation, telemetry, or metadata tests are stale; run make generate"
    fi
  done
  rm -rf "${generated_tmp}"
  generated_tmp=""
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
  go test . -coverprofile="${coverage_tmp}" >/dev/null
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
if [[ -x "${repo_root}/.tools/bin/builder" ]]; then
  ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${otel_collector_version}" >/dev/null
  log "build collector with .tools/bin/builder"
  "${repo_root}/.tools/bin/builder" --config builder-config.yaml
  collector_built=true
elif command -v builder >/dev/null 2>&1; then
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

if [[ "${collector_built}" == "true" ]]; then
  log "test maintained metrics event example against local OAuth and JSON v2 fakes"
  ./scripts/test-metrics-event.sh
else
  skip "metrics event integration test requires ocb/builder"
fi

if [[ "${collector_built}" == "true" ]]; then
  log "test maintained trace exception example against local OAuth and JSON v2 fakes"
  ./scripts/test-trace-exception.sh
else
  skip "trace exception integration test requires ocb/builder"
fi

log "verify complete"
