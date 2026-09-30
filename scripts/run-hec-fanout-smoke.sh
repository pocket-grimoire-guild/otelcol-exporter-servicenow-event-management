#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

env_file="${SERVICENOW_HEC_FANOUT_ENV_FILE:-.env.local}"
if [[ "${SERVICENOW_HEC_FANOUT_SOURCE_ENV:-1}" == "1" && -f "${env_file}" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "${env_file}"
  set +a
fi

dry_run="${SERVICENOW_HEC_FANOUT_DRY_RUN:-0}"
keep_work_dir="${SERVICENOW_HEC_FANOUT_KEEP_WORK_DIR:-0}"
work_dir="${SERVICENOW_HEC_FANOUT_WORK_DIR:-}"
created_work_dir=""
if [[ -z "${work_dir}" ]]; then
  work_dir="$(mktemp -d -t servicenow-event-management-hec-fanout.XXXXXX)"
  created_work_dir="${work_dir}"
fi
mkdir -p "${work_dir}"

run_id="${SERVICENOW_HEC_FANOUT_RUN_ID:-hec-fanout-$(date -u +%Y%m%d%H%M%S)}"
auth="${SERVICENOW_HEC_FANOUT_AUTH:-oauth2client}"
total_events="${SERVICENOW_HEC_FANOUT_TOTAL_EVENTS:-50}"
pass_events="${SERVICENOW_HEC_FANOUT_PASS_EVENTS:-10}"
threshold="${SERVICENOW_HEC_FANOUT_THRESHOLD:-90}"
sourcetype="${SERVICENOW_HEC_FANOUT_SOURCETYPE:-otel:servicenow-hec-fanout}"
receiver_port="${SERVICENOW_HEC_FANOUT_RECEIVER_PORT:-18088}"
health_port="${SERVICENOW_HEC_FANOUT_HEALTH_PORT:-13250}"
batch_size="${SERVICENOW_HEC_FANOUT_BATCH_SIZE:-10}"
splunk_index="${SERVICENOW_HEC_FANOUT_SPLUNK_INDEX:-main}"
splunk_image="${SERVICENOW_HEC_FANOUT_SPLUNK_IMAGE:-docker.io/splunk/splunk:latest}"
start_splunk="${SERVICENOW_HEC_FANOUT_START_SPLUNK:-1}"
accept_splunk_license="${SERVICENOW_HEC_FANOUT_ACCEPT_SPLUNK_LICENSE:-0}"
servicenow_readback="${SERVICENOW_HEC_FANOUT_SERVICENOW_READBACK:-1}"
splunk_readback="${SERVICENOW_HEC_FANOUT_SPLUNK_READBACK:-1}"
splunk_index_wait="${SERVICENOW_HEC_FANOUT_SPLUNK_INDEX_WAIT_SECONDS:-60}"
readback_timeout="${SERVICENOW_HEC_FANOUT_READBACK_TIMEOUT_SECONDS:-180}"
poll_interval="${SERVICENOW_HEC_FANOUT_POLL_INTERVAL_SECONDS:-5}"

builder_config="${work_dir}/builder-config.yaml"
collector_config="${work_dir}/collector.yaml"
collector_log="${work_dir}/collector.log"
sent_tsv="${work_dir}/${run_id}-sent.tsv"
servicenow_tsv="${work_dir}/${run_id}-servicenow.tsv"
splunk_tsv="${work_dir}/${run_id}-splunk.tsv"
splunk_raw="${work_dir}/${run_id}-splunk-search.jsonl"

collector_pid=""
splunk_container=""
created_splunk_container=""
builder_command=""

cleanup() {
  if [[ -n "${collector_pid}" ]]; then
    kill "${collector_pid}" >/dev/null 2>&1 || true
    wait "${collector_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${created_splunk_container}" && "${SERVICENOW_HEC_FANOUT_KEEP_SPLUNK:-0}" != "1" ]]; then
    "${runtime}" rm -f "${created_splunk_container}" >/dev/null 2>&1 || true
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

select_builder() {
  local expected_version="${OTEL_COLLECTOR_VERSION:-v0.153.0}"
  if [[ -x "${repo_root}/.tools/bin/builder" ]]; then
    PATH="${repo_root}/.tools/bin:${PATH}" ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${expected_version}" >/dev/null
    builder_command="${repo_root}/.tools/bin/builder"
  elif command -v builder >/dev/null 2>&1; then
    ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "${expected_version}" >/dev/null
    builder_command="builder"
  elif command -v ocb >/dev/null 2>&1; then
    ./scripts/check-collector-tool-version.sh ocb go.opentelemetry.io/collector/cmd/builder "${expected_version}" >/dev/null
    builder_command="ocb"
  else
    fail "Collector builder ${expected_version} is required; run make install-tools"
  fi
}

require_positive_int() {
  local name="$1"
  local value="$2"
  if ! [[ "${value}" =~ ^[0-9]+$ ]] || (( value <= 0 )); then
    fail "${name} must be a positive integer, got ${value}"
  fi
}

require_non_negative_int() {
  local name="$1"
  local value="$2"
  if ! [[ "${value}" =~ ^[0-9]+$ ]]; then
    fail "${name} must be a non-negative integer, got ${value}"
  fi
}

write_builder_config() {
  cat >"${builder_config}" <<YAML
dist:
  name: otelcol-servicenow-event-management-hec-fanout
  description: Temporary Collector for the ServiceNow EM HEC fan-out smoke
  output_path: ${work_dir}/otelcol-servicenow-event-management-hec-fanout
  version: 0.1.0

exporters:
  - gomod:
      github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management v0.0.0
    import: github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management
    name: servicenoweventmanagementexporter
    path: ${repo_root}
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/exporter/splunkhecexporter v0.153.0
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
      github.com/open-telemetry/opentelemetry-collector-contrib/receiver/splunkhecreceiver v0.153.0

extensions:
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/extension/basicauthextension v0.153.0
  - gomod:
      github.com/open-telemetry/opentelemetry-collector-contrib/extension/bearertokenauthextension v0.153.0
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

authenticator_id() {
  case "${auth}" in
    oauth2client) printf 'oauth2client/servicenow' ;;
    basicauth) printf 'basicauth/servicenow' ;;
    bearertokenauth) printf 'bearertokenauth/servicenow' ;;
    *) fail "SERVICENOW_HEC_FANOUT_AUTH must be oauth2client, basicauth, or bearertokenauth; got ${auth}" ;;
  esac
}

write_auth_extension() {
  case "${auth}" in
    oauth2client)
      cat <<'YAML'
  oauth2client/servicenow:
    client_id: ${env:SERVICENOW_CLIENT_ID}
    client_secret: ${env:SERVICENOW_CLIENT_SECRET}
    token_url: ${env:SERVICENOW_TOKEN_URL}
    grant_type: client_credentials
YAML
      ;;
    basicauth)
      cat <<'YAML'
  basicauth/servicenow:
    client_auth:
      username: ${env:SERVICENOW_USERNAME}
      password: ${env:SERVICENOW_PASSWORD}
YAML
      ;;
    bearertokenauth)
      cat <<'YAML'
  bearertokenauth/servicenow:
    token: ${env:SERVICENOW_BEARER_TOKEN}
YAML
      ;;
  esac
}

write_collector_config() {
  local authenticator
  authenticator="$(authenticator_id)"
  {
    cat <<YAML
receivers:
  splunk_hec:
    endpoint: 127.0.0.1:${receiver_port}
    hec_metadata_to_otel_attrs:
      source: splunk.source
      sourcetype: splunk.sourcetype
      index: splunk.index
      host: host.name

processors:
  transform/hec_fanout_enrich:
    error_mode: propagate
    log_statements:
      - context: log
        statements:
          - set(attributes["servicenow.source"], "splunk-hec-fanout-smoke")
          - set(attributes["servicenow.type"], "splunk-hec-fanout-threshold")
          - set(attributes["servicenow.event_class"], "otel-hec-fanout")
          - set(attributes["servicenow.metric_name"], "synthetic.metric_value")
          - set(attributes["servicenow.severity"], 3)
          - set(attributes["servicenow.description"], body)
          - set(attributes["test.pipeline.name"], "servicenow-hec-fanout")
          - set(attributes["test.pipeline.threshold"], ${threshold})
          - set(attributes["test.pipeline.sourcetype"], "${sourcetype}")
  filter/servicenow_threshold:
    error_mode: propagate
    logs:
      log_record:
        - 'attributes["metric_value"] <= ${threshold}'
  batch/servicenow:
    timeout: 5s
    send_batch_size: ${batch_size}
    send_batch_max_size: ${batch_size}
  batch/splunk:
    timeout: 5s
    send_batch_size: ${batch_size}
    send_batch_max_size: ${batch_size}

exporters:
  servicenow_event_management:
    endpoint: \${env:SERVICENOW_INSTANCE_URL}
    mode: instance
    auth:
      authenticator: ${authenticator}
    source: splunk-hec-fanout-smoke
    type: splunk-hec-fanout-threshold
    severity:
      from_attribute: servicenow.severity
      default: 5
      clear_resolution_state: Closing
    field_limits:
      source: 200
      event_class: 100
      node: 100
      resource: 100
      metric_name: 1024
      type: 100
      message_key: 1024
      description: 4000
      additional_info: 4000
      resolution_state: 40
    additional_info:
      max_attributes: 128
      max_value_length: 4000
    timeout: 30s
    sending_queue:
      enabled: true
      queue_size: 1000
      batch:
        sizer: items
        min_size: ${batch_size}
        max_size: ${batch_size}
        flush_timeout: 200ms
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 300s
  splunk_hec/all_logs:
    endpoint: \${env:SPLUNK_HEC_ENDPOINT}
    token: \${env:SPLUNK_HEC_TOKEN}
    index: ${splunk_index}
    source: otel-hec-fanout
    sourcetype: "${sourcetype}"
    disable_compression: true
    health_check_enabled: false
    tls:
      insecure_skip_verify: true
    sending_queue:
      enabled: true
      queue_size: 1000
      batch:
        sizer: items
        min_size: ${batch_size}
        max_size: ${batch_size}
        flush_timeout: 200ms

extensions:
YAML
    write_auth_extension
    cat <<YAML
  health_check:
    endpoint: 127.0.0.1:${health_port}

service:
  extensions: [${authenticator}, health_check]
  pipelines:
    logs/servicenow_threshold:
      receivers: [splunk_hec]
      processors: [transform/hec_fanout_enrich, filter/servicenow_threshold, batch/servicenow]
      exporters: [servicenow_event_management]
    logs/splunk_all:
      receivers: [splunk_hec]
      processors: [transform/hec_fanout_enrich, batch/splunk]
      exporters: [splunk_hec/all_logs]
YAML
  } >"${collector_config}"
}

write_sent_table() {
  local fail_events=$((total_events - pass_events))
  local case_id metric_value expected message_key body
  printf 'case_id\tmetric_value\texpected_servicenow\tmessage_key\tbody\n' >"${sent_tsv}"
  for ((i = 1; i <= total_events; i++)); do
    printf -v case_id '%03d' "${i}"
    if (( i <= fail_events )); then
      metric_value=$((10 + i))
      expected=false
    else
      metric_value=$((threshold + i - fail_events))
      expected=true
    fi
    message_key="${run_id}-${case_id}"
    body="HEC fan-out smoke ${message_key} run_id=${run_id} metric_value=${metric_value} sourcetype=${sourcetype}"
    printf '%s\t%s\t%s\t%s\t%s\n' "${case_id}" "${metric_value}" "${expected}" "${message_key}" "${body}" >>"${sent_tsv}"
  done
}

write_dry_run_outputs() {
  write_sent_table
  {
    printf 'message_key\tseverity\tsource\tmetric_value\tdescription\n'
    awk -F '\t' 'NR > 1 && $3 == "true" { printf "%s\t3\tsplunk-hec-fanout-smoke\t%s\t%s\n", $4, $2, $5 }' "${sent_tsv}"
  } >"${servicenow_tsv}"
  {
    printf 'time\thost\tsource\tsourcetype\traw\n'
    awk -F '\t' -v sourcetype="${sourcetype}" 'NR > 1 { printf "dry-run\thec-node-%s\tsplunk-hec-fanout-input\t%s\t%s\n", $1, sourcetype, $5 }' "${sent_tsv}"
  } >"${splunk_tsv}"
}

runtime=""
select_runtime() {
  if [[ -n "${SERVICENOW_HEC_FANOUT_CONTAINER_RUNTIME:-}" ]]; then
    runtime="${SERVICENOW_HEC_FANOUT_CONTAINER_RUNTIME}"
  elif command -v podman >/dev/null 2>&1; then
    runtime="podman"
  elif command -v docker >/dev/null 2>&1; then
    runtime="docker"
  else
    fail "podman or docker is required when SERVICENOW_HEC_FANOUT_START_SPLUNK=1"
  fi
}

random_secret() {
  openssl rand -hex 24
}

ensure_live_requirements() {
  require_command curl
  require_command jq
  require_command openssl
  select_builder
  if [[ "${start_splunk}" == "1" ]]; then
    select_runtime
  fi

  [[ -n "${SERVICENOW_INSTANCE_URL:-}" ]] || fail "SERVICENOW_INSTANCE_URL is required"
  if [[ "${auth}" == "oauth2client" ]]; then
    [[ -n "${SERVICENOW_CLIENT_ID:-}" ]] || fail "SERVICENOW_CLIENT_ID is required for OAuth"
    [[ -n "${SERVICENOW_CLIENT_SECRET:-}" ]] || fail "SERVICENOW_CLIENT_SECRET is required for OAuth"
    if [[ -z "${SERVICENOW_TOKEN_URL:-}" ]]; then
      export SERVICENOW_TOKEN_URL="${SERVICENOW_INSTANCE_URL%/}/oauth_token.do"
    fi
  elif [[ "${auth}" == "basicauth" ]]; then
    [[ -n "${SERVICENOW_USERNAME:-}" && -n "${SERVICENOW_PASSWORD:-}" ]] || fail "SERVICENOW_USERNAME and SERVICENOW_PASSWORD are required for Basic auth"
  elif [[ "${auth}" == "bearertokenauth" ]]; then
    [[ -n "${SERVICENOW_BEARER_TOKEN:-}" ]] || fail "SERVICENOW_BEARER_TOKEN is required for bearer token auth"
  else
    fail "unsupported auth mode: ${auth}"
  fi

  if [[ "${servicenow_readback}" == "1" ]]; then
    [[ -n "${SERVICENOW_READBACK_USERNAME:-${SERVICENOW_ADMIN:-${SERVICENOW_USERNAME:-}}}" ]] || fail "ServiceNow readback requires SERVICENOW_READBACK_USERNAME, SERVICENOW_ADMIN, or SERVICENOW_USERNAME"
    [[ -n "${SERVICENOW_READBACK_PASSWORD:-${SERVICENOW_ADMIN_PASSWORD:-${SERVICENOW_PASSWORD:-}}}" ]] || fail "ServiceNow readback requires SERVICENOW_READBACK_PASSWORD, SERVICENOW_ADMIN_PASSWORD, or SERVICENOW_PASSWORD"
  fi
}

start_splunk_container() {
  if [[ "${accept_splunk_license}" != "1" ]]; then
    fail "set SERVICENOW_HEC_FANOUT_ACCEPT_SPLUNK_LICENSE=1 to start a temporary Splunk container"
  fi

  export SPLUNK_HEC_TOKEN="${SPLUNK_HEC_TOKEN:-$(random_secret)}"
  export SPLUNK_PASSWORD="${SPLUNK_PASSWORD:-SuperSmoke-$(random_secret)!}"
  splunk_container="${SERVICENOW_HEC_FANOUT_SPLUNK_CONTAINER:-servicenow-event-management-hec-fanout-${run_id}}"
  log "starting temporary Splunk container ${splunk_container}"
  "${runtime}" run -d \
    --name "${splunk_container}" \
    -p 127.0.0.1::8088 \
    -p 127.0.0.1::8089 \
    -e SPLUNK_START_ARGS=--accept-license \
    -e SPLUNK_GENERAL_TERMS=--accept-sgt-current-at-splunk-com \
    -e "SPLUNK_PASSWORD=${SPLUNK_PASSWORD}" \
    -e "SPLUNK_HEC_TOKEN=${SPLUNK_HEC_TOKEN}" \
    "${splunk_image}" >/dev/null
  created_splunk_container="${splunk_container}"

  local hec_port mgmt_port
  hec_port="$("${runtime}" port "${splunk_container}" 8088/tcp | awk -F: 'END {print $NF}')"
  mgmt_port="$("${runtime}" port "${splunk_container}" 8089/tcp | awk -F: 'END {print $NF}')"
  export SPLUNK_HEC_ENDPOINT="https://127.0.0.1:${hec_port}/services/collector"
  export SPLUNK_MANAGEMENT_URL="https://127.0.0.1:${mgmt_port}"
}

wait_for_splunk() {
  local deadline code
  deadline=$(($(date -u +%s) + ${SERVICENOW_HEC_FANOUT_SPLUNK_READY_TIMEOUT_SECONDS:-300}))
  while :; do
    code="$(curl --silent --show-error --insecure \
      --header "Authorization: Splunk ${SPLUNK_HEC_TOKEN}" \
      --output /dev/null \
      --write-out '%{http_code}' \
      "${SPLUNK_HEC_ENDPOINT}/health" || true)"
    if [[ "${code}" == "200" ]]; then
      break
    fi
    if (( $(date -u +%s) >= deadline )); then
      fail "Splunk HEC did not become healthy; last HTTP status ${code}"
    fi
    sleep 5
  done
}

build_and_validate_collector() {
  log "building temporary Collector"
  [[ -n "${builder_command}" ]] || select_builder
  "${builder_command}" --config "${builder_config}" >/dev/null
  collector="${work_dir}/otelcol-servicenow-event-management-hec-fanout/otelcol-servicenow-event-management-hec-fanout"
  [[ -x "${collector}" ]] || fail "collector binary not found after OCB build: ${collector}"
  log "validating generated Collector config"
  "${collector}" validate --config "${collector_config}" >/dev/null
}

start_collector() {
  log "starting Collector"
  "${collector}" --config "${collector_config}" >"${collector_log}" 2>&1 &
  collector_pid="$!"
  local deadline
  deadline=$(($(date -u +%s) + 60))
  while :; do
    if ! kill -0 "${collector_pid}" >/dev/null 2>&1; then
      fail "Collector exited before becoming healthy; see ${collector_log}"
    fi
    if curl --fail --silent --show-error "http://127.0.0.1:${health_port}/" >/dev/null 2>&1; then
      break
    fi
    if (( $(date -u +%s) >= deadline )); then
      fail "Collector did not become healthy at http://127.0.0.1:${health_port}/; see ${collector_log}"
    fi
    sleep 1
  done
}

send_hec_messages() {
  write_sent_table
  local non_200=0
  local payload_file response_file code case_id metric_value expected message_key body host payload
  payload_file="${work_dir}/hec-payload.json"
  response_file="${work_dir}/hec-response.json"
  while IFS=$'\t' read -r case_id metric_value expected message_key body; do
    [[ "${case_id}" != "case_id" ]] || continue
    host="hec-node-${case_id}"
    payload="$(
      jq -n \
        --arg body "${body}" \
        --arg host "${host}" \
        --arg source "splunk-hec-fanout-input" \
        --arg sourcetype "${sourcetype}" \
        --arg run_id "${run_id}" \
        --arg case_id "${case_id}" \
        --arg message_key "${message_key}" \
        --arg expected "${expected}" \
        --argjson metric_value "${metric_value}" \
        '{
          event: $body,
          host: $host,
          source: $source,
          sourcetype: $sourcetype,
          fields: {
            "test.run.id": $run_id,
            "test.case.id": $case_id,
            "metric_value": $metric_value,
            "servicenow.message_key": $message_key,
            "servicenow.node": $host,
            "servicenow.resource": "checkout-api",
            "servicenow.resolution_state": "New",
            "pipeline.expected_servicenow": $expected
          }
        }'
    )"
    printf '%s\n' "${payload}" >"${payload_file}"
    code="$(curl --silent --show-error \
      --header "Authorization: Splunk smoke-token" \
      --header "Content-Type: application/json" \
      --data @"${payload_file}" \
      --output "${response_file}" \
      --write-out '%{http_code}' \
      "http://127.0.0.1:${receiver_port}/services/collector/event" || true)"
    if [[ "${code}" != "200" ]]; then
      non_200=$((non_200 + 1))
    fi
  done <"${sent_tsv}"
  printf 'receiver_non_200=%s\n' "${non_200}"
  if (( non_200 > 0 )); then
    fail "one or more HEC messages failed at the Collector receiver; see ${response_file}"
  fi
}

poll_servicenow_readback() {
  local read_user read_pass query fields deadline code count readback_file
  read_user="${SERVICENOW_READBACK_USERNAME:-${SERVICENOW_ADMIN:-${SERVICENOW_USERNAME:-}}}"
  read_pass="${SERVICENOW_READBACK_PASSWORD:-${SERVICENOW_ADMIN_PASSWORD:-${SERVICENOW_PASSWORD:-}}}"
  readback_file="${work_dir}/${run_id}-servicenow-readback.json"
  query="message_keySTARTSWITH${run_id}"
  fields="message_key,severity,source,metric_value,description"
  deadline=$(($(date -u +%s) + readback_timeout))
  while :; do
    code="$(curl --silent --show-error --location --get \
      --user "${read_user}:${read_pass}" \
      --header 'Accept: application/json' \
      --data-urlencode "sysparm_query=${query}^ORDERBYmessage_key" \
      --data-urlencode "sysparm_fields=${fields}" \
      --data-urlencode "sysparm_limit=${pass_events}" \
      "${SERVICENOW_INSTANCE_URL%/}/api/now/table/em_event" \
      --output "${readback_file}" \
      --write-out '%{http_code}' || true)"
    if [[ "${code}" != "200" ]]; then
      fail "ServiceNow readback returned HTTP ${code}; see ${readback_file}"
    fi
    count="$(jq '.result | length' "${readback_file}")"
    if (( count >= pass_events )); then
      break
    fi
    if (( $(date -u +%s) >= deadline )); then
      break
    fi
    printf 'servicenow_readback_pending count=%s expected=%s\n' "${count}" "${pass_events}"
    sleep "${poll_interval}"
  done
  jq -r '
    (["message_key", "severity", "source", "metric_value", "description"] | @tsv),
    (.result[] | [.message_key, .severity, .source, .metric_value, .description] | @tsv)
  ' "${readback_file}" >"${servicenow_tsv}"
  if (( count < pass_events )); then
    fail "ServiceNow readback incomplete: got ${count} of ${pass_events} events"
  fi
}

splunk_search() {
  local search="$1"
  curl --silent --show-error --insecure \
    --user "admin:${SPLUNK_PASSWORD}" \
    --data-urlencode "search=${search}" \
    --data-urlencode "output_mode=json" \
    "${SPLUNK_MANAGEMENT_URL}/services/search/jobs/export"
}

poll_splunk_readback() {
  local deadline count search
  log "waiting ${splunk_index_wait}s for Splunk indexing"
  sleep "${splunk_index_wait}"
  deadline=$(($(date -u +%s) + readback_timeout))
  search="search index=${splunk_index} \"${run_id}\" sourcetype=\"${sourcetype}\" | stats count as count"
  while :; do
    count="$(splunk_search "${search}" | jq -sr '[.[] | select(.result) | .result.count | tonumber] | max // 0')"
    if (( count >= total_events )); then
      break
    fi
    if (( $(date -u +%s) >= deadline )); then
      break
    fi
    printf 'splunk_readback_pending count=%s expected=%s\n' "${count}" "${total_events}"
    sleep "${poll_interval}"
  done

  search="search index=${splunk_index} \"${run_id}\" sourcetype=\"${sourcetype}\" | sort 0 _time | table _time host source sourcetype _raw"
  splunk_search "${search}" >"${splunk_raw}"
  jq -r '
    (["time", "host", "source", "sourcetype", "raw"] | @tsv),
    (select(.result) | .result | [._time, .host, .source, .sourcetype, ._raw] | @tsv)
  ' "${splunk_raw}" >"${splunk_tsv}"
  if (( count < total_events )); then
    fail "Splunk readback incomplete: got ${count} of ${total_events} events"
  fi
}

print_summary() {
  local sent_count snow_count splunk_count
  sent_count=$(( $(wc -l <"${sent_tsv}") - 1 ))
  snow_count=0
  splunk_count=0
  [[ -f "${servicenow_tsv}" ]] && snow_count=$(( $(wc -l <"${servicenow_tsv}") - 1 ))
  [[ -f "${splunk_tsv}" ]] && splunk_count=$(( $(wc -l <"${splunk_tsv}") - 1 ))
  cat <<EOF
run_id=${run_id}
auth=${auth}
sourcetype=${sourcetype}
threshold=${threshold}
sent_events=${sent_count}
expected_servicenow_events=${pass_events}
servicenow_events=${snow_count}
splunk_events=${splunk_count}
work_dir=${work_dir}
sent_tsv=${sent_tsv}
servicenow_tsv=${servicenow_tsv}
splunk_tsv=${splunk_tsv}
collector_config=${collector_config}
builder_config=${builder_config}
EOF
}

require_positive_int SERVICENOW_HEC_FANOUT_TOTAL_EVENTS "${total_events}"
require_non_negative_int SERVICENOW_HEC_FANOUT_PASS_EVENTS "${pass_events}"
require_positive_int SERVICENOW_HEC_FANOUT_THRESHOLD "${threshold}"
require_positive_int SERVICENOW_HEC_FANOUT_BATCH_SIZE "${batch_size}"
require_positive_int SERVICENOW_HEC_FANOUT_READBACK_TIMEOUT_SECONDS "${readback_timeout}"
require_positive_int SERVICENOW_HEC_FANOUT_POLL_INTERVAL_SECONDS "${poll_interval}"
if (( pass_events > total_events )); then
  fail "SERVICENOW_HEC_FANOUT_PASS_EVENTS cannot exceed SERVICENOW_HEC_FANOUT_TOTAL_EVENTS"
fi

write_builder_config
write_collector_config

if [[ "${dry_run}" == "1" ]]; then
  log "dry-run: generated configs and expected output tables"
  write_dry_run_outputs
  print_summary
  exit 0
fi

ensure_live_requirements
if [[ "${start_splunk}" == "1" ]]; then
  start_splunk_container
  wait_for_splunk
else
  [[ -n "${SPLUNK_HEC_ENDPOINT:-}" && -n "${SPLUNK_HEC_TOKEN:-}" ]] || fail "SPLUNK_HEC_ENDPOINT and SPLUNK_HEC_TOKEN are required when SERVICENOW_HEC_FANOUT_START_SPLUNK=0"
  if [[ "${splunk_readback}" == "1" ]]; then
    [[ -n "${SPLUNK_MANAGEMENT_URL:-}" && -n "${SPLUNK_PASSWORD:-}" ]] || fail "SPLUNK_MANAGEMENT_URL and SPLUNK_PASSWORD are required for Splunk readback"
  fi
fi

build_and_validate_collector
start_collector
send_hec_messages

if [[ "${servicenow_readback}" == "1" ]]; then
  poll_servicenow_readback
else
  printf 'message_key\tseverity\tsource\tmetric_value\tdescription\n' >"${servicenow_tsv}"
fi

if [[ "${splunk_readback}" == "1" ]]; then
  poll_splunk_readback
else
  printf 'time\thost\tsource\tsourcetype\traw\n' >"${splunk_tsv}"
fi

print_summary
