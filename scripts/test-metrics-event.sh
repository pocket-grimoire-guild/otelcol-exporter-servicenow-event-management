#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

export GOMAXPROCS="${GOMAXPROCS:-3}"
export GOFLAGS="${GOFLAGS:--p=2}"
collector_builder_version="${OTEL_COLLECTOR_VERSION:-v0.153.0}"
unset METRICS_EVENT_TEST_CONFIG_TEMPLATE METRICS_EVENT_TEST_ALLOW_CONFIG_OVERRIDE

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

require_command go
require_command yq
if [[ -x "${repo_root}/.tools/bin/builder" ]]; then
  PATH="${repo_root}/.tools/bin:${PATH}" ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${collector_builder_version}" >/dev/null
  builder=("${repo_root}/.tools/bin/builder")
elif command -v builder >/dev/null 2>&1; then
  ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${collector_builder_version}" >/dev/null
  builder=(builder)
elif command -v ocb >/dev/null 2>&1; then
  ./scripts/check-collector-tool-version.sh ocb go.opentelemetry.io/collector/cmd/builder "${collector_builder_version}" >/dev/null
  builder=(ocb)
else
  fail "Collector builder v0.153.0 is required; run make install-tools"
fi

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/servicenow-metrics-event-test.XXXXXX")"
cleanup() {
  if [[ "${METRICS_EVENT_KEEP_BUILD_DIR:-0}" == "1" ]]; then
    printf 'kept metrics example build directory: %s\n' "${work_dir}"
  else
    rm -rf "${work_dir}"
  fi
}
trap cleanup EXIT

builder_config="${work_dir}/builder-config.yaml"
build_output="${work_dir}/otelcol-servicenow-event-management-metrics-event"
cp examples/collector-builder-metrics-event.yaml "${builder_config}"

yq_help="$(yq --help 2>&1 || true)"
if [[ "${yq_help}" == *"jq filter"* ]]; then
  METRICS_EVENT_BUILD_OUTPUT="${build_output}" \
    METRICS_EVENT_REPO_ROOT="${repo_root}" \
    yq --yaml-output --in-place \
      '.dist.output_path = env.METRICS_EVENT_BUILD_OUTPUT | .exporters[0].gomod = "github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management v0.0.0" | .exporters[0].path = env.METRICS_EVENT_REPO_ROOT' \
      "${builder_config}"
else
  METRICS_EVENT_BUILD_OUTPUT="${build_output}" \
    METRICS_EVENT_REPO_ROOT="${repo_root}" \
    yq -i \
      '.dist.output_path = strenv(METRICS_EVENT_BUILD_OUTPUT) | .exporters[0].gomod = "github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management v0.0.0" | .exporters[0].path = strenv(METRICS_EVENT_REPO_ROOT)' \
      "${builder_config}"
fi

printf '==> build maintained metrics event example with %s\n' "${builder[0]}"
"${builder[@]}" --config "${builder_config}"

collector_binary="${build_output}/otelcol-servicenow-event-management-metrics-event"
[[ -x "${collector_binary}" ]] || fail "metrics example Collector binary was not produced at ${collector_binary}"

export METRICS_EVENT_COLLECTOR_BINARY="${collector_binary}"
printf '==> run uncached metrics event integration test with race detection\n'
go test -count=1 -race -tags=integration -v ./tests/metricsevent
