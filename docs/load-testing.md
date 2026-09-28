# Load And Throughput Validation

The repo includes a rich OTLP log generator for exercising a local Collector pipeline:

```bash
OTEL_SMOKE_TOTAL_EVENTS=1000 \
OTEL_SMOKE_BATCHES=20 \
OTEL_SMOKE_AUTH_LABEL=basicauth \
OTEL_SMOKE_SOURCE_PREFIX=load-smoke \
OTEL_SMOKE_SCENARIO=load-smoke \
make smoke-otel-rich-batches
```

The generator sends varied ServiceNow override fields and varied `additional_info` attribute types to `http://127.0.0.1:4318/v1/logs` by default. Start a Collector with one of the example configs first.

For a live ServiceNow throughput smoke, use the wrapper target. It starts the local Collector, sends rich OTLP batches, then reads `em_event` back by the generated `message_key` prefix:

```bash
make build-collector

OTEL_SMOKE_TOTAL_EVENTS=500 \
OTEL_SMOKE_BATCHES=10 \
OTEL_SMOKE_BATCH_DELAY=0.05 \
OTEL_SMOKE_AUTH_LABEL=basicauth \
OTEL_SMOKE_SOURCE_PREFIX=throughput-basic \
OTEL_SMOKE_SCENARIO=throughput-basic \
make smoke-servicenow-throughput
```

The wrapper sources `.env.local` by default when present. Set `SERVICENOW_THROUGHPUT_ENV_FILE` to use another file, `SERVICENOW_THROUGHPUT_CONFIG` to select a Collector config, or `SERVICENOW_THROUGHPUT_START_COLLECTOR=0` to target an already-running Collector. Read-back uses `SERVICENOW_ADMIN`/`SERVICENOW_ADMIN_PASSWORD` when available, then falls back to `SERVICENOW_USERNAME`/`SERVICENOW_PASSWORD`.

For local payload-shape validation without a live Collector, use:

```bash
OTEL_SMOKE_DRY_RUN_DIR=/tmp/servicenow-event-management-load \
OTEL_SMOKE_TOTAL_EVENTS=1000 \
OTEL_SMOKE_BATCHES=20 \
OTEL_SMOKE_BATCH_DELAY=0 \
make smoke-otel-rich-batches
```

For a live fan-out demonstration that includes Splunk HEC input, threshold filtering, ServiceNow Event Management output, and Splunk HEC output for every log, use:

```bash
SERVICENOW_HEC_FANOUT_DRY_RUN=1 make smoke-hec-fanout
```

Then, after ServiceNow and Splunk credentials are loaded, opt into the temporary Splunk container or point the harness at an existing Splunk Enterprise instance:

```bash
SERVICENOW_HEC_FANOUT_AUTH=oauth2client \
SERVICENOW_HEC_FANOUT_ACCEPT_SPLUNK_LICENSE=1 \
make smoke-hec-fanout
```

The default HEC fan-out sends 50 HEC event-mode messages with `sourcetype=otel:servicenow-hec-fanout`; 10 have `metric_value > 90` and should be exported to ServiceNow, while all 50 should be searchable in Splunk.

For the smaller metrics-derived event demonstration, use:

```bash
SERVICENOW_METRICS_EVENT_DRY_RUN=1 make smoke-metrics-event
```

The live run uses OAuth client credentials, sends three OTLP gauge datapoints, drops the one at or below the configured threshold, and reads back two ServiceNow `em_event` rows by `message_key` prefix.

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
- For HEC fan-out runs, Splunk indexed count, search time range, `sourcetype`, and time from HEC send to search visibility.

## Starting Point

Use `send_batch_max_size: 100` and, when exporter queue batching is enabled, `sending_queue.batch.max_size: 100` until the target ServiceNow environment proves a higher size is safe. Public JSON v2 docs show multi-record payloads but do not document a maximum record count.

Current PDI evidence: on 2026-05-16, the Basic auth instance path accepted a 120-event live throughput smoke sent as 6 OTLP batches of 20 generated records. All 120 rows were readable from `em_event` within 12 seconds after send, with unique values across the major Event Management fields and parseable `additional_info` for every row. Treat this as PDI harness evidence, not a production maximum.

Do not publish customer data, credentials, authorization headers, or unredacted `additional_info` in results.
