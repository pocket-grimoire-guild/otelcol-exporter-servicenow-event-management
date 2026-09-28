#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
collector_builder_version="${OTEL_COLLECTOR_VERSION:-v0.153.0}"

env_file="${SERVICENOW_METRICS_EVENT_ENV_FILE:-.env.local}"
if [[ "${SERVICENOW_METRICS_EVENT_SOURCE_ENV:-1}" == "1" && -f "${env_file}" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "${env_file}"
  set +a
fi

if [[ -z "${SERVICENOW_TOKEN_URL:-}" && -n "${SERVICENOW_INSTANCE_URL:-}" ]]; then
  export SERVICENOW_TOKEN_URL="${SERVICENOW_INSTANCE_URL%/}/oauth_token.do"
fi

dry_run="${SERVICENOW_METRICS_EVENT_DRY_RUN:-0}"
keep_work_dir="${SERVICENOW_METRICS_EVENT_KEEP_WORK_DIR:-0}"
work_dir="${SERVICENOW_METRICS_EVENT_WORK_DIR:-}"
created_work_dir=""
if [[ -z "${work_dir}" ]]; then
  work_dir="$(mktemp -d -t servicenow-event-management-metrics-event.XXXXXX)"
  created_work_dir="${work_dir}"
fi
mkdir -p "${work_dir}"

run_id="${SERVICENOW_METRICS_EVENT_RUN_ID:-metrics-event-$(date -u +%Y%m%d%H%M%S)}"
threshold="${SERVICENOW_METRICS_EVENT_THRESHOLD:-90}"
receiver_port="${SERVICENOW_METRICS_EVENT_RECEIVER_PORT:-14318}"
health_port="${SERVICENOW_METRICS_EVENT_HEALTH_PORT:-13260}"
readback="${SERVICENOW_METRICS_EVENT_READBACK:-1}"
readback_timeout="${SERVICENOW_METRICS_EVENT_READBACK_TIMEOUT_SECONDS:-180}"
poll_interval="${SERVICENOW_METRICS_EVENT_POLL_INTERVAL_SECONDS:-5}"

builder_config="${work_dir}/builder-config.yaml"
collector_config="${work_dir}/collector.yaml"
collector_log="${work_dir}/collector.log"
payload_file="${work_dir}/${run_id}-metrics.json"
expected_tsv="${work_dir}/${run_id}-expected-servicenow.tsv"
readback_file="${work_dir}/${run_id}-servicenow-readback.json"

collector_pid=""
cleanup() {
  if [[ -n "${collector_pid}" ]]; then
    kill "${collector_pid}" >/dev/null 2>&1 || true
    wait "${collector_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${created_work_dir}" && "${keep_work_dir}" != "1" ]]; then
    rm -rf "${created_work_dir}"
  fi
}
trap cleanup EXIT

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

require_positive_int() {
  local name="$1"
  local value="$2"
  if ! [[ "${value}" =~ ^[0-9]+$ ]] || (( value <= 0 )); then
    fail "${name} must be a positive integer, got ${value}"
  fi
}

write_builder_config() {
  cat >"${builder_config}" <<YAML
dist:
  name: otelcol-servicenow-event-management-metrics-event
  description: Temporary Collector for ServiceNow Event Management metrics-derived event smoke
  output_path: ${work_dir}/otelcol-servicenow-event-management-metrics-event
  version: 0.1.0

exporters:
  - gomod:
      github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management v0.0.0
    import: github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management/exporter/servicenoweventmanagementexporter
    path: ${repo_root}
  - gomod:
      go.opentelemetry.io/collector/exporter/debugexporter v0.153.0

processors:
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor v0.153.0
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor v0.153.0
  - gomod:
      go.opentelemetry.io/collector/processor/batchprocessor v0.153.0

receivers:
  - gomod:
      go.opentelemetry.io/collector/receiver/otlpreceiver v0.153.0

connectors:
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/connector/metricsaslogsconnector v0.153.0

extensions:
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/extension/healthcheckextension v0.153.0
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/extension/oauth2clientauthextension v0.153.0

providers:
  - gomod:
      go.opentelemetry.io/collector/confmap/provider/envprovider v1.59.0
  - gomod:
      go.opentelemetry.io/collector/confmap/provider/fileprovider v1.59.0
  - gomod:
      go.opentelemetry.io/collector/confmap/provider/yamlprovider v1.59.0
YAML
}

write_collector_config() {
  cat >"${collector_config}" <<YAML
receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:${receiver_port}

processors:
  filter/metric_threshold:
    error_mode: propagate
    metric_conditions:
      - 'metric.name != "demo.checkout.error_rate"'
      - 'datapoint.value_double <= ${threshold}'

  transform/servicenow_metric_event:
    error_mode: propagate
    log_statements:
      - context: log
        statements:
          - set(log.attributes["event.name"], "metric.threshold.breach")
          - set(log.attributes["test.run.id"], "${run_id}")
          - set(log.attributes["alert.threshold"], "${threshold}")
          - set(log.attributes["servicenow.source"], "opentelemetry-metrics-threshold")
          - set(log.attributes["servicenow.event_class"], "otel-metric-threshold")
          - set(log.attributes["servicenow.type"], "metric-threshold")
          - set(log.attributes["servicenow.resource"], resource.attributes["service.name"])
          - set(log.attributes["servicenow.node"], resource.attributes["host.name"])
          - set(log.attributes["servicenow.metric_name"], log.attributes["metric.name"])
          - set(log.attributes["servicenow.severity"], 3)
          - set(log.attributes["servicenow.resolution_state"], "New")
          - set(log.attributes["servicenow.description"], Concat(["Metric threshold breach", log.attributes["metric.name"], log.attributes["gauge.value"]], " "))
          - set(log.attributes["servicenow.message_key"], Concat([log.attributes["test.run.id"], resource.attributes["service.name"], resource.attributes["host.name"], log.attributes["metric.name"], log.attributes["route"]], "|"))
          - set(log.body, log.attributes["servicenow.description"])

  batch/servicenow:
    timeout: 5s
    send_batch_size: 10
    send_batch_max_size: 10

connectors:
  metricsaslogs:
    include_resource_attributes: true
    include_scope_info: true

exporters:
  debug:
    verbosity: basic

  servicenow_event_management:
    endpoint: \${env:SERVICENOW_INSTANCE_URL}
    mode: instance
    api: jsonv2
    auth:
      authenticator: oauth2client/servicenow
    source: opentelemetry-metrics-threshold
    type: metric-threshold
    additional_info:
      max_attributes: 128
      max_value_length: 4000
    timeout: 30s
    sending_queue:
      enabled: true
      queue_size: 1000
      batch:
        sizer: items
        min_size: 10
        max_size: 10
        flush_timeout: 200ms
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s

extensions:
  oauth2client/servicenow:
    client_id: \${env:SERVICENOW_CLIENT_ID}
    client_secret: \${env:SERVICENOW_CLIENT_SECRET}
    token_url: \${env:SERVICENOW_TOKEN_URL}
    grant_type: client_credentials
  health_check:
    endpoint: 127.0.0.1:${health_port}

service:
  extensions: [oauth2client/servicenow, health_check]
  pipelines:
    metrics/threshold:
      receivers: [otlp]
      processors: [filter/metric_threshold]
      exporters: [metricsaslogs]
    logs/servicenow:
      receivers: [metricsaslogs]
      processors: [transform/servicenow_metric_event, batch/servicenow]
      exporters: [servicenow_event_management, debug]
YAML
}

write_payload() {
  local now_nano start_nano below_value first_value second_value
  now_nano="$(date -u +%s%N)"
  start_nano="$((now_nano - 60000000000))"
  below_value="$((threshold - 12))"
  first_value="$((threshold + 3))"
  second_value="$((threshold + 8))"

  jq -n \
    --arg run_id "${run_id}" \
    --arg start_nano "${start_nano}" \
    --arg now_nano "${now_nano}" \
    --arg below_value "${below_value}.5" \
    --arg first_value "${first_value}.2" \
    --arg second_value "${second_value}.4" \
    '{
      resourceMetrics: [
        {
          resource: {
            attributes: [
              {key: "service.name", value: {stringValue: "checkout-api"}},
              {key: "host.name", value: {stringValue: ("metrics-node-" + $run_id)}},
              {key: "deployment.environment", value: {stringValue: "pdi-smoke"}}
            ]
          },
          scopeMetrics: [
            {
              scope: {name: "servicenow-event-management.metrics-event-smoke", version: "0.1.0"},
              metrics: [
                {
                  name: "demo.checkout.error_rate",
                  description: "Synthetic checkout error rate percentage for ServiceNow Event Management threshold example",
                  unit: "%",
                  gauge: {
                    dataPoints: [
                      {
                        attributes: [
                          {key: "route", value: {stringValue: "/cart"}},
                          {key: "test.run.id", value: {stringValue: $run_id}},
                          {key: "test.expected_servicenow", value: {stringValue: "false"}}
                        ],
                        startTimeUnixNano: $start_nano,
                        timeUnixNano: $now_nano,
                        asDouble: ($below_value | tonumber)
                      },
                      {
                        attributes: [
                          {key: "route", value: {stringValue: "/checkout"}},
                          {key: "test.run.id", value: {stringValue: $run_id}},
                          {key: "test.expected_servicenow", value: {stringValue: "true"}}
                        ],
                        startTimeUnixNano: $start_nano,
                        timeUnixNano: $now_nano,
                        asDouble: ($first_value | tonumber)
                      },
                      {
                        attributes: [
                          {key: "route", value: {stringValue: "/payments"}},
                          {key: "test.run.id", value: {stringValue: $run_id}},
                          {key: "test.expected_servicenow", value: {stringValue: "true"}}
                        ],
                        startTimeUnixNano: $start_nano,
                        timeUnixNano: $now_nano,
                        asDouble: ($second_value | tonumber)
                      }
                    ]
                  }
                }
              ]
            }
          ]
        }
      ]
    }' >"${payload_file}"

  {
    printf 'message_key\tseverity\tsource\tmetric_name\tvalue\troute\n'
    printf '%s|checkout-api|metrics-node-%s|demo.checkout.error_rate|/checkout\t3\topentelemetry-metrics-threshold\tdemo.checkout.error_rate\t%s\t/checkout\n' "${run_id}" "${run_id}" "${first_value}.2"
    printf '%s|checkout-api|metrics-node-%s|demo.checkout.error_rate|/payments\t3\topentelemetry-metrics-threshold\tdemo.checkout.error_rate\t%s\t/payments\n' "${run_id}" "${run_id}" "${second_value}.4"
  } >"${expected_tsv}"
}

build_collector() {
  if [[ -x "${repo_root}/.tools/bin/builder" ]]; then
    PATH="${repo_root}/.tools/bin:${PATH}" ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${collector_builder_version}" >/dev/null
    "${repo_root}/.tools/bin/builder" --config "${builder_config}"
  elif command -v builder >/dev/null 2>&1; then
    ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${collector_builder_version}" >/dev/null
    builder --config "${builder_config}"
  elif command -v ocb >/dev/null 2>&1; then
    ./scripts/check-collector-tool-version.sh ocb go.opentelemetry.io/collector/cmd/builder "${collector_builder_version}" >/dev/null
    ocb --config "${builder_config}"
  else
    fail "Collector builder v0.153.0 is required to build the temporary metrics-event Collector"
  fi
}

start_collector() {
  local collector="${work_dir}/otelcol-servicenow-event-management-metrics-event/otelcol-servicenow-event-management-metrics-event"
  [[ -x "${collector}" ]] || fail "collector binary not found after build: ${collector}"

  "${collector}" --config "${collector_config}" >"${collector_log}" 2>&1 &
  collector_pid="$!"

  for _ in $(seq 1 45); do
    if ! kill -0 "${collector_pid}" >/dev/null 2>&1; then
      fail "collector exited before becoming healthy; see ${collector_log}"
    fi
    if curl --fail --silent --show-error "http://127.0.0.1:${health_port}/" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done

  fail "collector did not become healthy on port ${health_port}; see ${collector_log}"
}

send_metrics() {
  local code response_file
  response_file="${work_dir}/${run_id}-otlp-response.txt"
  code="$(
    curl --silent --show-error \
      --request POST "http://127.0.0.1:${receiver_port}/v1/metrics" \
      --header 'Content-Type: application/json' \
      --data @"${payload_file}" \
      --output "${response_file}" \
      --write-out '%{http_code}'
  )"
  if [[ "${code}" != "200" ]]; then
    printf 'otlp_http_code=%s\n' "${code}" >&2
    cat "${response_file}" >&2 || true
    exit 1
  fi
}

readback_events() {
  local instance_url read_user read_pass deadline fields query code count
  instance_url="${SERVICENOW_INSTANCE_URL:-}"
  read_user="${SERVICENOW_READBACK_USERNAME:-${SERVICENOW_ADMIN:-${SERVICENOW_USERNAME:-}}}"
  read_pass="${SERVICENOW_READBACK_PASSWORD:-${SERVICENOW_ADMIN_PASSWORD:-${SERVICENOW_PASSWORD:-}}}"
  [[ -n "${instance_url}" && -n "${read_user}" && -n "${read_pass}" ]] || fail "readback requires SERVICENOW_INSTANCE_URL and read credentials"

  fields="source,event_class,node,resource,metric_name,type,severity,resolution_state,message_key,description,additional_info,sys_created_on"
  query="message_keySTARTSWITH${run_id}"
  deadline=$(($(date -u +%s) + readback_timeout))
  count=0

  while :; do
    code="$(
      curl --silent --show-error --location --get \
        --user "${read_user}:${read_pass}" \
        --header 'Accept: application/json' \
        --data-urlencode "sysparm_query=${query}^ORDERBYmessage_key" \
        --data-urlencode "sysparm_fields=${fields}" \
        --data-urlencode "sysparm_limit=10" \
        "${instance_url%/}/api/now/table/em_event" \
        --output "${readback_file}" \
        --write-out '%{http_code}'
    )"
    if [[ "${code}" != "200" ]]; then
      printf 'readback_http_code=%s\n' "${code}" >&2
      jq -r '.error.message? // .error.detail? // . | tostring' "${readback_file}" >&2 || true
      exit 1
    fi

    count="$(jq '.result | length' "${readback_file}")"
    if (( count >= 2 )); then
      break
    fi
    if (( $(date -u +%s) >= deadline )); then
      break
    fi
    printf 'readback_pending count=%s expected=2\n' "${count}"
    sleep "${poll_interval}"
  done

  jq -r \
    --arg run_id "${run_id}" \
    '{
      run_id: $run_id,
      expected_events: 2,
      readback_events: (.result | length),
      message_keys: [.result[].message_key],
      sources: ([.result[].source] | unique),
      metric_names: ([.result[].metric_name] | unique),
      severities: ([.result[].severity] | unique),
      resolution_states: ([.result[].resolution_state] | unique),
      additional_info_parseable: ([.result[].additional_info | try fromjson catch null | select(. != null)] | length),
      first_created_on: ([.result[].sys_created_on] | min // ""),
      last_created_on: ([.result[].sys_created_on] | max // "")
    }' "${readback_file}"

  if (( count < 2 )); then
    fail "readback incomplete: got ${count} of 2 expected events before timeout"
  fi
}

require_command jq
require_command curl
require_positive_int SERVICENOW_METRICS_EVENT_THRESHOLD "${threshold}"
require_positive_int SERVICENOW_METRICS_EVENT_RECEIVER_PORT "${receiver_port}"
require_positive_int SERVICENOW_METRICS_EVENT_HEALTH_PORT "${health_port}"
require_positive_int SERVICENOW_METRICS_EVENT_READBACK_TIMEOUT_SECONDS "${readback_timeout}"
require_positive_int SERVICENOW_METRICS_EVENT_POLL_INTERVAL_SECONDS "${poll_interval}"

write_builder_config
write_collector_config
write_payload

printf 'run_id=%s\n' "${run_id}"
printf 'threshold=%s\n' "${threshold}"
printf 'work_dir=%s\n' "${work_dir}"
printf 'builder_config=%s\n' "${builder_config}"
printf 'collector_config=%s\n' "${collector_config}"
printf 'payload=%s\n' "${payload_file}"
printf 'expected=%s\n' "${expected_tsv}"

if [[ "${dry_run}" == "1" ]]; then
  printf 'dry_run=true\n'
  exit 0
fi

[[ -n "${SERVICENOW_INSTANCE_URL:-}" ]] || fail "SERVICENOW_INSTANCE_URL is required"
[[ -n "${SERVICENOW_CLIENT_ID:-}" ]] || fail "SERVICENOW_CLIENT_ID is required"
[[ -n "${SERVICENOW_CLIENT_SECRET:-}" ]] || fail "SERVICENOW_CLIENT_SECRET is required"
[[ -n "${SERVICENOW_TOKEN_URL:-}" ]] || fail "SERVICENOW_TOKEN_URL is required"

log "build temporary Collector"
build_collector
log "start Collector"
start_collector
log "send OTLP metrics"
send_metrics

if [[ "${readback}" == "1" ]]; then
  log "read back ServiceNow events"
  readback_events
else
  printf 'readback=skipped\n'
fi
