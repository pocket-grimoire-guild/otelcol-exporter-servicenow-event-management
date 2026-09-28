#!/usr/bin/env bash
set -euo pipefail

otlp_endpoint="${OTLP_ENDPOINT:-http://127.0.0.1:4318/v1/logs}"
batches="${OTEL_SMOKE_BATCHES:-10}"
events_per_batch="${OTEL_SMOKE_EVENTS_PER_BATCH:-5}"
total_events="${OTEL_SMOKE_TOTAL_EVENTS:-$((batches * events_per_batch))}"
batch_delay="${OTEL_SMOKE_BATCH_DELAY:-0.2}"
run_id="${OTEL_SMOKE_RUN_ID:-oauth-rich-$(date -u +%Y%m%d%H%M%S)}"
now_s="${OTEL_SMOKE_NOW_S:-$(date -u +%s)}"
time_window_s="${OTEL_SMOKE_TIME_WINDOW_SECONDS:-3600}"
auth_label="${OTEL_SMOKE_AUTH_LABEL:-oauth2client}"
auth_scope="${OTEL_SMOKE_AUTH_SCOPE:-useraccount}"
scenario="${OTEL_SMOKE_SCENARIO:-oauth-rich-smoke}"
source_prefix="${OTEL_SMOKE_SOURCE_PREFIX:-otel-oauth-rich-smoke}"
service_namespace="${OTEL_SMOKE_SERVICE_NAMESPACE:-${scenario}}"
scope_name="${OTEL_SMOKE_SCOPE_NAME:-servicenow-event-management.${scenario}}"
dry_run_dir="${OTEL_SMOKE_DRY_RUN_DIR:-}"

if ((batches <= 0)); then
  printf 'OTEL_SMOKE_BATCHES must be greater than zero\n' >&2
  exit 1
fi

if ((total_events <= 0)); then
  printf 'OTEL_SMOKE_TOTAL_EVENTS must be greater than zero\n' >&2
  exit 1
fi

printf 'run_id=%s\n' "${run_id}"
printf 'otlp_endpoint=%s\n' "${otlp_endpoint}"
printf 'batches=%s total_events=%s default_events_per_batch=%s\n' "${batches}" "${total_events}" "${events_per_batch}"
printf 'scenario=%s auth=%s source_prefix=%s\n' "${scenario}" "${auth_label}" "${source_prefix}"

if [[ -n "${dry_run_dir}" ]]; then
  mkdir -p "${dry_run_dir}"
  printf 'dry_run_dir=%s\n' "${dry_run_dir}"
fi

base_records_per_batch=$((total_events / batches))
extra_records=$((total_events % batches))

for ((batch = 0; batch < batches; batch++)); do
  records_in_batch="${base_records_per_batch}"
  if ((batch < extra_records)); then
    records_in_batch=$((records_in_batch + 1))
  fi

  if ((records_in_batch == 0)); then
    printf 'skip batch=%d records=0\n' "$((batch + 1))"
    continue
  fi

  prior_extra_records="${batch}"
  if ((prior_extra_records > extra_records)); then
    prior_extra_records="${extra_records}"
  fi
  start_index=$((batch * base_records_per_batch + prior_extra_records))

  payload="$(
    jq -n \
      --arg run_id "${run_id}" \
      --arg auth_label "${auth_label}" \
      --arg auth_scope "${auth_scope}" \
      --arg scenario "${scenario}" \
      --arg source_prefix "${source_prefix}" \
      --arg service_namespace "${service_namespace}" \
      --arg scope_name "${scope_name}" \
      --argjson batch "${batch}" \
      --argjson batches "${batches}" \
      --argjson records_in_batch "${records_in_batch}" \
      --argjson start_index "${start_index}" \
      --argjson total_events "${total_events}" \
      --argjson now_s "${now_s}" \
      --argjson time_window_s "${time_window_s}" \
      '
      def pick($items; $i): $items[$i % ($items | length)];

      [
        "checkout-api", "payments-worker", "inventory-sync", "edge-gateway",
        "catalog-search", "fulfillment-orchestrator", "fraud-scorer",
        "notification-router", "billing-ledger", "customer-profile",
        "orders-api", "returns-worker", "pricing-engine", "tax-calculator",
        "warehouse-adapter", "recommendations", "identity-gateway",
        "session-store", "email-delivery", "mobile-bff"
      ] as $services |
      [
        "us-east-1", "us-east-2", "us-west-2", "eu-west-1",
        "eu-central-1", "ap-southeast-2", "ap-northeast-1"
      ] as $regions |
      ["az-a", "az-b", "az-c", "az-d"] as $zones |
      ["pdi-smoke", "load-canary", "preprod", "resilience-drill"] as $environments |
      [
        "api-001", "api-002", "api-003", "worker-001", "worker-002",
        "worker-003", "edge-001", "edge-002", "db-001", "db-002",
        "queue-001", "queue-002", "cache-001", "cache-002", "batch-001",
        "batch-002", "search-001", "search-002", "ml-001", "ml-002"
      ] as $nodeSuffixes |
      [
        "cpu.utilization", "memory.pressure", "checkout.down", "queue.backlog",
        "disk.latency", "db.connections", "cache.hit_ratio", "worker.lag",
        "error.budget", "cert.expiry", "request.latency", "http.5xx_rate",
        "topic.lag", "batch.duration", "replica.health", "dependency.timeout",
        "saturation.score", "pod.restarts", "synthetic.availability", "payload.size"
      ] as $metricSuffixes |
      [
        "resource_saturation", "availability", "latency", "throughput",
        "dependency", "security", "capacity", "correctness", "slo", "maintenance",
        "change_risk", "data_freshness", "certificate", "batch_processing",
        "customer_experience"
      ] as $classes |
      [
        "collector_field_saturation", "collector_field_availability",
        "collector_field_latency", "collector_field_throughput",
        "collector_field_dependency", "collector_field_security",
        "collector_field_capacity", "collector_field_correctness",
        "collector_field_slo", "collector_field_maintenance",
        "collector_field_change", "collector_field_batch"
      ] as $types |
      [
        "checkout", "payments", "inventory", "edge", "identity",
        "search", "fulfillment", "billing", "mobile", "platform"
      ] as $routes |
      ["agent", "synthetic", "mid", "api", "rule", "canary", "batch", "probe"] as $sourceVariants |
      [
        "cart-db-primary", "payments-topic", "inventory-replica",
        "edge-listener", "search-index", "fulfillment-queue",
        "fraud-model", "notification-topic", "ledger-partition",
        "profile-cache", "orders-table", "returns-workflow"
      ] as $resources |
      ["GET", "POST", "PUT", "PATCH", "DELETE"] as $methods |
      [200, 202, 204, 301, 400, 401, 403, 404, 409, 429, 500, 502, 503] as $statusCodes |
      [0, 1, 2, 3, 4, 5] as $snowSeverities |
      [1, 5, 9, 13, 17, 21] as $otelSeverities |
      ["TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"] as $severityTexts |
      {
        resourceLogs: [
          {
            resource: {
              attributes: [
                {key: "service.name", value: {stringValue: ("otel-servicenow-event-management-" + pick($services; $batch))}},
                {key: "service.namespace", value: {stringValue: $service_namespace}},
                {key: "deployment.environment", value: {stringValue: pick($environments; $batch)}},
                {key: "cloud.provider", value: {stringValue: "local-dev"}},
                {key: "cloud.region", value: {stringValue: pick($regions; $batch)}},
                {key: "cloud.availability_zone", value: {stringValue: pick($zones; $batch)}},
                {key: "k8s.cluster.name", value: {stringValue: ("pdi-smoke-cluster-" + (($batch % 4) + 1 | tostring))}},
                {key: "k8s.namespace.name", value: {stringValue: ("commerce-" + pick($routes; $batch))}}
              ]
            },
            scopeLogs: [
              {
                scope: {
                  name: $scope_name,
                  version: "0.1.0"
                },
                logRecords: [
                  range(0; $records_in_batch) as $i |
                  ($start_index + $i) as $idx |
                  pick($snowSeverities; $idx) as $snowSeverity |
                  (if $snowSeverity == 0 then "Closing" else "New" end) as $resolutionState |
                  {
                    timeUnixNano: (((
                      $now_s
                      - (((($total_events - $idx) * $time_window_s) / ($total_events + 1)) | floor)
                    ) * 1000000000) | tostring),
                    observedTimeUnixNano: ((($now_s * 1000000000) | tostring)),
                    severityNumber: pick($otelSeverities; $idx),
                    severityText: pick($severityTexts; $idx),
                    body: {
                      stringValue: (
                        ($auth_label + " ServiceNow Event Management field coverage event ")
                        + (($idx + 1) | tostring)
                        + " in batch "
                        + (($batch + 1) | tostring)
                      )
                    },
                    attributes: [
                      {key: "event.name", value: {stringValue: ($scenario + "." + pick($metricSuffixes; $idx))}},
                      {key: "event.domain", value: {stringValue: pick($classes; $idx)}},
                      {key: "event.description", value: {stringValue: ("Fallback description for event " + (($idx + 1) | tostring))}},
                      {key: "servicenow.source", value: {stringValue: ($source_prefix + "-" + pick($sourceVariants; $idx))}},
                      {key: "servicenow.event_class", value: {stringValue: pick($classes; $idx)}},
                      {key: "servicenow.node", value: {stringValue: ($scenario + "-" + pick($nodeSuffixes; $idx))}},
                      {key: "servicenow.resource", value: {stringValue: pick($resources; $idx)}},
                      {key: "servicenow.metric_name", value: {stringValue: ($scenario + "." + pick($metricSuffixes; $idx))}},
                      {key: "servicenow.type", value: {stringValue: pick($types; $idx)}},
                      {key: "servicenow.message_key", value: {stringValue: ($run_id + "-" + (($idx + 1) | tostring))}},
                      {key: "servicenow.severity", value: {intValue: ($snowSeverity | tostring)}},
                      {key: "servicenow.description", value: {stringValue: (
                        $auth_label + " field coverage event " + (($idx + 1) | tostring)
                        + ": " + $scenario + "." + pick($metricSuffixes; $idx)
                        + " on " + pick($services; $idx)
                        + " via " + pick($routes; $idx)
                        + " in " + pick($regions; ($idx + $batch))
                      )}},
                      {key: "servicenow.resolution_state", value: {stringValue: $resolutionState}},
                      {key: "auth.method", value: {stringValue: $auth_label}},
                      {key: "oauth.scope", value: {stringValue: $auth_scope}},
                      {key: "host.name", value: {stringValue: ($scenario + "-" + pick($nodeSuffixes; $idx))}},
                      {key: "service.instance.id", value: {stringValue: (pick($services; $idx) + "-" + (($idx % 97) + 1 | tostring))}},
                      {key: "container.name", value: {stringValue: ("container-" + pick($routes; ($idx + $batch)))}},
                      {key: "k8s.pod.name", value: {stringValue: (pick($services; $idx) + "-pod-" + (($idx % 31) + 1 | tostring))}},
                      {key: "http.method", value: {stringValue: pick($methods; $idx)}},
                      {key: "http.route", value: {stringValue: ("/" + pick($routes; $idx) + "/v" + (($idx % 3) + 1 | tostring) + "/" + pick($metricSuffixes; $idx))}},
                      {key: "http.status_code", value: {intValue: (pick($statusCodes; $idx) | tostring)}},
                      {key: "db.system", value: {stringValue: pick(["postgresql", "mysql", "redis", "elasticsearch"]; $idx)}},
                      {key: "messaging.system", value: {stringValue: pick(["kafka", "sqs", "pubsub", "rabbitmq"]; $idx)}},
                      {key: "net.peer.name", value: {stringValue: (pick($services; ($idx + 3)) + ".service.local")}},
                      {key: "test.run.id", value: {stringValue: $run_id}},
                      {key: "test.scenario", value: {stringValue: $scenario}},
                      {key: "test.auth.path", value: {stringValue: $auth_label}},
                      {key: "test.batch.count", value: {intValue: ($batches | tostring)}},
                      {key: "test.batch.index", value: {intValue: (($batch + 1) | tostring)}},
                      {key: "test.event.index", value: {intValue: (($idx + 1) | tostring)}},
                      {key: "test.event.in_batch", value: {intValue: (($i + 1) | tostring)}},
                      {key: "test.ui.state_hint", value: {stringValue: (if $resolutionState == "Closing" then "closing-clear-event" else "new-active-event" end)}},
                      {key: "test.boolean.flip", value: {boolValue: (($idx % 2) == 0)}},
                      {key: "test.ratio", value: {doubleValue: ((($idx % 13) / 10) + 0.25)}},
                      {key: "test.load.bucket", value: {stringValue: ("bucket-" + (($idx % 10) | tostring))}},
                      {key: "test.payload.kind", value: {stringValue: pick(["synthetic", "threshold", "heartbeat", "correlation", "clear"]; $idx)}},
                      {key: "alert.routing_hint", value: {stringValue: pick($routes; $idx)}},
                      {key: "alert.priority", value: {intValue: ((($idx % 5) + 1) | tostring)}},
                      {key: "alert.suppressed", value: {boolValue: (($idx % 11) == 0)}},
                      {key: "alert.runbook_url", value: {stringValue: ("https://example.invalid/runbooks/servicenow-field-coverage/" + (($idx % 10) + 1 | tostring))}},
                      {key: "ci.candidate", value: {stringValue: (($scenario + "-" + pick($nodeSuffixes; $idx)) + "::" + pick($resources; $idx))}},
                      {key: "business.service", value: {stringValue: ("Commerce " + pick($routes; $idx))}},
                      {key: "test.labels", value: {arrayValue: {values: [
                        {stringValue: pick($routes; $idx)},
                        {stringValue: pick($resources; $idx)},
                        {stringValue: ($scenario + "." + pick($metricSuffixes; $idx))}
                      ]}}},
                      {key: "test.context", value: {kvlistValue: {values: [
                        {key: "batch", value: {intValue: (($batch + 1) | tostring)}},
                        {key: "event", value: {intValue: (($idx + 1) | tostring)}},
                        {key: "route", value: {stringValue: pick($routes; $idx)}},
                        {key: "region", value: {stringValue: pick($regions; ($idx + $batch))}},
                        {key: "resource", value: {stringValue: pick($resources; $idx)}},
                        {key: "clear_event", value: {boolValue: ($snowSeverity == 0)}}
                      ]}}},
                      {key: "test.bytes.marker", value: {bytesValue: "b3RlbC1zZXJ2aWNlbm93ZW0="}}
                    ]
                  }
                ]
              }
            ]
          }
        ]
      }
      '
  )"

  if [[ -n "${dry_run_dir}" ]]; then
    printf '%s\n' "${payload}" >"${dry_run_dir}/batch-$((batch + 1)).json"
  else
    printf '%s\n' "${payload}" | curl --fail-with-body --show-error --silent \
      --request POST "${otlp_endpoint}" \
      --header "Content-Type: application/json" \
      --data-binary @- >/dev/null
  fi

  printf 'sent batch=%d records=%d start_event=%d\n' "$((batch + 1))" "${records_in_batch}" "$((start_index + 1))"
  sleep "${batch_delay}"
done

printf 'run_id=%s\n' "${run_id}"
