# Load And Throughput Validation

This repository does not ship committed live load-test tooling. That is intentional: real ServiceNow load validation depends on credentials, instance family, Event Management rules, MID topology, customer data policy, and acceptable event volume. Keep target-specific generators and raw transcripts in private validation records.

Use the checked-in Collector examples as the starting point for an environment-owned test:

1. Build a local Collector with [../builder-config.yaml](../builder-config.yaml) or a released manifest from [../examples](../examples).
2. Send representative OTLP logs to the Collector's OTLP receiver.
3. Read back sanitized Event Management results through the ServiceNow UI or read-only `em_event` inspection.
4. Record only summarized evidence in [validation-evidence](validation-evidence/README.md).

If a recurring live check belongs in this repository, prefer a `//go:build integration` Go test that skips when required environment variables are absent. That keeps the default local and CI path credential-free while matching common OpenTelemetry Collector component practice.

The metrics-derived event example is kept as configuration only. To validate that pattern, build a Collector with [../examples/collector-builder-metrics-event.yaml](../examples/collector-builder-metrics-event.yaml), run [../examples/servicenow-event-management-metrics-event-oauth.yaml](../examples/servicenow-event-management-metrics-event-oauth.yaml), and record sanitized ServiceNow read-back results.

## What To Measure

For a real ServiceNow environment, capture:

- Collector version and exporter version.
- ServiceNow instance type and family.
- Endpoint mode: instance or MID.
- Endpoint API flavor: `jsonv2` or `business_rules`.
- Auth method.
- Batch processor `send_batch_size` and `send_batch_max_size`.
- Exporter queue settings.
- Total events sent and accepted.
- HTTP status distribution.
- Collector retry and queue behavior.
- Time until events appear in Event Management.
- Field variety in read-back: unique `source`, `event_class`, `node`, `resource`, `metric_name`, `type`, `severity`, and `resolution_state` values.
- Whether clear events close the expected alerts.

## Starting Point

Use `send_batch_max_size: 100` and, when exporter queue batching is enabled, `sending_queue.batch.max_size: 100` until the target ServiceNow environment proves a higher size is safe. Public JSON v2 docs show multi-record payloads but do not document a maximum record count.

Current PDI evidence: the Basic auth instance path accepted multi-record Collector-backed requests and all tested rows were readable from `em_event`, with varied major Event Management fields and parseable `additional_info`. Treat this as implementation evidence, not a production maximum.

Do not publish customer data, credentials, authorization headers, or unredacted `additional_info` in results.
