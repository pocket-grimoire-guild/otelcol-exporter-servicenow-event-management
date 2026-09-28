#!/usr/bin/env bash
set -euo pipefail

mode="${SERVICENOW_MODE:-instance}"
api="${SERVICENOW_API:-jsonv2}"
instance_url="${SERVICENOW_INSTANCE_URL:-https://example.service-now.com}"
send="${SERVICENOW_SEND:-0}"

case "${api}" in
  jsonv2 | business_rules)
    ;;
  *)
    printf 'SERVICENOW_API must be jsonv2 or business_rules, got %s\n' "${api}" >&2
    exit 1
    ;;
esac

case "${mode}:${api}" in
  instance:jsonv2)
    endpoint="${instance_url%/}/api/global/em/jsonv2"
    ;;
  instance:business_rules)
    endpoint="${instance_url%/}/em_event.do?JSONv2&sysparm_action=insertMultiple"
    ;;
  mid:jsonv2 | mid:business_rules)
    endpoint="${instance_url%/}/api/mid/em/jsonv2"
    ;;
  *)
    printf 'SERVICENOW_MODE must be instance or mid, got %s\n' "${mode}" >&2
    exit 1
    ;;
esac

if [[ "${send}" == "1" ]]; then
  dry_run=false
else
  dry_run=true
fi

payload="$(
  jq -n \
    --arg now "$(date -u '+%Y-%m-%d %H:%M:%S')" \
    --arg source "${SERVICENOW_SOURCE:-opentelemetry-smoke}" \
    --arg node "${SERVICENOW_NODE:-otel-devcontainer}" \
    --arg mode "${mode}" \
    --arg api "${api}" \
    --argjson dry_run "${dry_run}" \
    '{
      records: [
        {
          source: $source,
          event_class: "otel-servicenow-event-management-exporter",
          node: $node,
          resource: "dev-harness",
          metric_name: "smoke_test",
          type: "collector_exporter",
          message_key: ("otel-servicenow-event-management-exporter:" + $node + ":smoke_test"),
          severity: "5",
          description: "OpenTelemetry ServiceNow Event Management exporter smoke test",
          additional_info: ({
            "otel.signal": "logs",
            "harness": "servicenow-event-management-jsonv2",
            "endpoint_mode": $mode,
            "endpoint_api": $api,
            "dry_run": $dry_run
          } | with_entries(.value |= tostring) | tostring),
          time_of_event: $now
        }
      ]
    }'
)"

printf '%s\n' "${payload}" | jq .

if [[ "${send}" != "1" ]]; then
  if [[ "${mode}" == "mid" && "${api}" == "business_rules" ]]; then
    printf 'MID business_rules mode still posts to the MID JSON v2 listener; configure the MID Server upstream endpoint property for insertMultiple.\n'
  fi
  printf 'dry run only. Set SERVICENOW_SEND=1 to POST to %s\n' "${endpoint}"
  exit 0
fi

if [[ "${mode}" == "mid" && "${api}" == "business_rules" ]]; then
  printf 'MID business_rules mode still posts to the MID JSON v2 listener; configure the MID Server upstream endpoint property for insertMultiple.\n'
fi

curl_args=(
  --fail-with-body
  --show-error
  --silent
  --request POST
  --header 'Accept: application/json'
  --header 'Content-Type: application/json'
  --data "${payload}"
)

if [[ -n "${SERVICENOW_BEARER_TOKEN:-}" ]]; then
  curl_args+=(--header "Authorization: Bearer ${SERVICENOW_BEARER_TOKEN}")
elif [[ "${mode}" == "mid" && -n "${SERVICENOW_MID_API_KEY:-}" ]]; then
  curl_args+=(--header "Authorization: key ${SERVICENOW_MID_API_KEY}")
elif [[ -n "${SERVICENOW_USERNAME:-}" && -n "${SERVICENOW_PASSWORD:-}" ]]; then
  curl_args+=(--user "${SERVICENOW_USERNAME}:${SERVICENOW_PASSWORD}")
else
  printf 'SERVICENOW_SEND=1 requires SERVICENOW_BEARER_TOKEN, SERVICENOW_MID_API_KEY, or SERVICENOW_USERNAME/SERVICENOW_PASSWORD\n' >&2
  exit 1
fi

response_file="$(mktemp)"
cleanup() {
  rm -f "${response_file}"
}
trap cleanup EXIT

curl_status=0
curl_meta="$(
  curl "${curl_args[@]}" \
    --output "${response_file}" \
    --write-out 'http_code=%{http_code}
content_type=%{content_type}
' \
    "${endpoint}"
)" || curl_status=$?

http_code="$(printf '%s\n' "${curl_meta}" | awk -F= '/^http_code=/ { print $2 }')"
content_type="$(printf '%s\n' "${curl_meta}" | awk -F= '/^content_type=/ { print $2 }')"
content_type_lc="${content_type,,}"

if [[ ! -s "${response_file}" ]]; then
  if (( curl_status != 0 )); then
    printf 'ServiceNow returned an empty error response (HTTP %s).\n' "${http_code:-unknown}" >&2
    exit "${curl_status}"
  fi
  if [[ "${mode}" != "mid" ]]; then
    printf 'unexpected empty ServiceNow response for mode=%s (HTTP %s); direct instance JSON v2 is expected to return JSON.\n' "${mode}" "${http_code:-unknown}" >&2
    exit 1
  fi
  jq -n --arg http_status "${http_code:-unknown}" '{
    result: {
      endpoint_mode: "mid",
      status: "request accepted with empty successful response",
      http_status: $http_status
    }
  }'
  exit 0
fi

if [[ "${content_type_lc}" != *json* ]]; then
  printf 'unexpected ServiceNow response content-type: %s (HTTP %s)\n' "${content_type:-unknown}" "${http_code:-unknown}" >&2
  if grep -qi 'instance is hibernating' "${response_file}" || grep -qi 'Your instance is hibernating' "${response_file}"; then
    printf 'ServiceNow instance appears to be hibernating; wake it before running live exporter tests.\n' >&2
  fi
  exit 1
fi

if ! jq empty "${response_file}" >/dev/null; then
  printf 'ServiceNow response advertised JSON but could not be parsed (HTTP %s).\n' "${http_code:-unknown}" >&2
  exit 1
fi

jq . "${response_file}"

if (( curl_status != 0 )); then
  exit "${curl_status}"
fi
