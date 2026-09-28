#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

env_file="${SERVICENOW_THROUGHPUT_ENV_FILE:-.env.local}"
if [[ "${SERVICENOW_THROUGHPUT_SOURCE_ENV:-1}" == "1" && -f "${env_file}" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "${env_file}"
  set +a
fi

collector="${SERVICENOW_THROUGHPUT_COLLECTOR:-./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev}"
config="${SERVICENOW_THROUGHPUT_CONFIG:-examples/servicenow-event-management-exporter.yaml}"
start_collector="${SERVICENOW_THROUGHPUT_START_COLLECTOR:-1}"
readback="${SERVICENOW_THROUGHPUT_READBACK:-1}"
readback_timeout="${SERVICENOW_THROUGHPUT_READBACK_TIMEOUT_SECONDS:-180}"
poll_interval="${SERVICENOW_THROUGHPUT_POLL_INTERVAL_SECONDS:-5}"
health_url="${SERVICENOW_THROUGHPUT_HEALTH_URL:-http://127.0.0.1:13133/}"
collector_log="${SERVICENOW_THROUGHPUT_COLLECTOR_LOG:-}"

export OTEL_SMOKE_TOTAL_EVENTS="${OTEL_SMOKE_TOTAL_EVENTS:-500}"
export OTEL_SMOKE_BATCHES="${OTEL_SMOKE_BATCHES:-10}"
export OTEL_SMOKE_BATCH_DELAY="${OTEL_SMOKE_BATCH_DELAY:-0.05}"
export OTEL_SMOKE_AUTH_LABEL="${OTEL_SMOKE_AUTH_LABEL:-basicauth}"
export OTEL_SMOKE_SOURCE_PREFIX="${OTEL_SMOKE_SOURCE_PREFIX:-otel-throughput}"
export OTEL_SMOKE_SCENARIO="${OTEL_SMOKE_SCENARIO:-throughput}"
export OTEL_SMOKE_TIME_WINDOW_SECONDS="${OTEL_SMOKE_TIME_WINDOW_SECONDS:-7200}"
export OTEL_SMOKE_RUN_ID="${OTEL_SMOKE_RUN_ID:-throughput-$(date -u +%Y%m%d%H%M%S)}"

collector_pid=""
created_log=""
cleanup() {
  if [[ -n "${collector_pid}" ]]; then
    kill "${collector_pid}" >/dev/null 2>&1 || true
    wait "${collector_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${created_log}" && "${SERVICENOW_THROUGHPUT_KEEP_LOG:-0}" != "1" ]]; then
    rm -f "${created_log}"
  fi
}
trap cleanup EXIT

require_positive_int() {
  local name="$1"
  local value="$2"
  if ! [[ "${value}" =~ ^[0-9]+$ ]] || (( value <= 0 )); then
    printf '%s must be a positive integer, got %s\n' "${name}" "${value}" >&2
    exit 1
  fi
}

require_positive_int OTEL_SMOKE_TOTAL_EVENTS "${OTEL_SMOKE_TOTAL_EVENTS}"
require_positive_int OTEL_SMOKE_BATCHES "${OTEL_SMOKE_BATCHES}"
require_positive_int SERVICENOW_THROUGHPUT_READBACK_TIMEOUT_SECONDS "${readback_timeout}"
require_positive_int SERVICENOW_THROUGHPUT_POLL_INTERVAL_SECONDS "${poll_interval}"

printf 'run_id=%s\n' "${OTEL_SMOKE_RUN_ID}"
printf 'config=%s\n' "${config}"
printf 'total_events=%s batches=%s batch_delay=%s\n' "${OTEL_SMOKE_TOTAL_EVENTS}" "${OTEL_SMOKE_BATCHES}" "${OTEL_SMOKE_BATCH_DELAY}"
printf 'scenario=%s auth=%s source_prefix=%s\n' "${OTEL_SMOKE_SCENARIO}" "${OTEL_SMOKE_AUTH_LABEL}" "${OTEL_SMOKE_SOURCE_PREFIX}"

if [[ "${start_collector}" == "1" ]]; then
  if [[ ! -x "${collector}" ]]; then
    printf 'collector binary not found or not executable: %s\n' "${collector}" >&2
    printf 'Run make build-collector first, or set SERVICENOW_THROUGHPUT_COLLECTOR.\n' >&2
    exit 1
  fi
  if [[ ! -f "${config}" ]]; then
    printf 'collector config not found: %s\n' "${config}" >&2
    exit 1
  fi

  if [[ -z "${collector_log}" ]]; then
    created_log="$(mktemp -t servicenow-event-management-throughput-collector.XXXXXX.log)"
    collector_log="${created_log}"
  fi
  printf 'collector_log=%s\n' "${collector_log}"
  "${collector}" --config "${config}" >"${collector_log}" 2>&1 &
  collector_pid="$!"

  for _ in $(seq 1 30); do
    if ! kill -0 "${collector_pid}" >/dev/null 2>&1; then
      printf 'collector exited before becoming healthy; see %s\n' "${collector_log}" >&2
      exit 1
    fi
    if curl --fail --silent --show-error "${health_url}" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  if ! curl --fail --silent --show-error "${health_url}" >/dev/null 2>&1; then
    printf 'collector did not become healthy at %s; see %s\n' "${health_url}" "${collector_log}" >&2
    exit 1
  fi
else
  printf 'collector_start=skipped\n'
fi

send_start_s="$(date -u +%s)"
./scripts/send-otel-rich-log-batches.sh
send_end_s="$(date -u +%s)"
send_duration_s=$((send_end_s - send_start_s))
if (( send_duration_s == 0 )); then
  send_duration_s=1
fi
printf 'send_duration_seconds=%s\n' "${send_duration_s}"
printf 'send_rate_events_per_second=%s\n' "$((OTEL_SMOKE_TOTAL_EVENTS / send_duration_s))"

if [[ "${readback}" != "1" ]]; then
  printf 'readback=skipped\n'
  exit 0
fi

instance_url="${SERVICENOW_INSTANCE_URL:-}"
read_user="${SERVICENOW_READBACK_USERNAME:-${SERVICENOW_ADMIN:-${SERVICENOW_USERNAME:-}}}"
read_pass="${SERVICENOW_READBACK_PASSWORD:-${SERVICENOW_ADMIN_PASSWORD:-${SERVICENOW_PASSWORD:-}}}"

if [[ -z "${instance_url}" || -z "${read_user}" || -z "${read_pass}" ]]; then
  printf 'readback requires SERVICENOW_INSTANCE_URL and read credentials; set SERVICENOW_THROUGHPUT_READBACK=0 to skip.\n' >&2
  exit 1
fi

query="message_keySTARTSWITH${OTEL_SMOKE_RUN_ID}"
fields="source,event_class,node,resource,metric_name,type,severity,resolution_state,message_key,sys_created_on,additional_info"
deadline=$((send_end_s + readback_timeout))
last_count=0
readback_file="$(mktemp)"
trap 'rm -f "${readback_file}"; cleanup' EXIT

while :; do
  code="$(
    curl --silent --show-error --location --get \
      --user "${read_user}:${read_pass}" \
      --header 'Accept: application/json' \
      --data-urlencode "sysparm_query=${query}^ORDERBYmessage_key" \
      --data-urlencode "sysparm_fields=${fields}" \
      --data-urlencode "sysparm_limit=${OTEL_SMOKE_TOTAL_EVENTS}" \
      "${instance_url%/}/api/now/table/em_event" \
      --output "${readback_file}" \
      --write-out '%{http_code}'
  )"
  if [[ "${code}" != "200" ]]; then
    printf 'readback_http_code=%s\n' "${code}" >&2
    jq -r '.error.message? // .error.detail? // . | tostring' "${readback_file}" >&2 || true
    exit 1
  fi

  last_count="$(jq '.result | length' "${readback_file}")"
  if (( last_count >= OTEL_SMOKE_TOTAL_EVENTS )); then
    break
  fi
  if (( $(date -u +%s) >= deadline )); then
    break
  fi
  printf 'readback_pending count=%s expected=%s\n' "${last_count}" "${OTEL_SMOKE_TOTAL_EVENTS}"
  sleep "${poll_interval}"
done

readback_end_s="$(date -u +%s)"
jq -r \
  --arg run_id "${OTEL_SMOKE_RUN_ID}" \
  --argjson expected "${OTEL_SMOKE_TOTAL_EVENTS}" \
  --argjson send_duration "${send_duration_s}" \
  --argjson readback_duration "$((readback_end_s - send_end_s))" \
  '
  def values($field): [.result[] | .[$field] | select(. != null and . != "")] | unique;
  def parsed_info:
    [.result[].additional_info | try fromjson catch null | select(. != null)];
  {
    run_id: $run_id,
    expected_events: $expected,
    readback_events: (.result | length),
    send_duration_seconds: $send_duration,
    readback_duration_seconds: $readback_duration,
    unique_sources: (values("source") | length),
    unique_event_classes: (values("event_class") | length),
    unique_nodes: (values("node") | length),
    unique_resources: (values("resource") | length),
    unique_metric_names: (values("metric_name") | length),
    unique_types: (values("type") | length),
    severities: values("severity"),
    resolution_states: values("resolution_state"),
    additional_info_parseable: (parsed_info | length),
    first_created_on: ([.result[].sys_created_on] | min // ""),
    last_created_on: ([.result[].sys_created_on] | max // "")
  }' "${readback_file}"

if (( last_count < OTEL_SMOKE_TOTAL_EVENTS )); then
  printf 'readback incomplete: got %s of %s events before timeout\n' "${last_count}" "${OTEL_SMOKE_TOTAL_EVENTS}" >&2
  exit 1
fi
