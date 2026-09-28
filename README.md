# OpenTelemetry Collector Exporter for ServiceNow Event Management

`servicenow_event_management` is an OpenTelemetry Collector exporter that turns OTel log records into ServiceNow ITOM Event Management events.

It is intended for teams that want to build their own custom OpenTelemetry Collector Builder (OCB) distribution and send alert-like events from OpenTelemetry pipelines into ServiceNow Event Management.

This project is not an official ServiceNow product and is not currently part of `opentelemetry-collector-contrib`.

OpenTelemetry contrib readiness is tracked in [docs/contrib-readiness.md](docs/contrib-readiness.md).

## ServiceNow Documentation

This exporter intentionally links product choices back to ServiceNow documentation. The short version:

- Instance mode follows ServiceNow Event Management JSON v2 ingestion by default: [`/api/global/em/jsonv2`](https://www.servicenow.com/docs/r/it-operations-management/event-management/send-events-via-web-service.html).
- Business Rules compatibility uses ServiceNow's documented `em_event.do?JSONv2&sysparm_action=insertMultiple` endpoint when `mode: instance` and `api: business_rules` are set. ServiceNow documents this as lower-throughput than `/api/global/em/jsonv2`.
- MID mode follows MID WebService JSON v2 ingestion: [`/api/mid/em/jsonv2`](https://www.servicenow.com/docs/r/it-operations-management/event-management/event-collection-via-MID-using-push.html).
- Field mapping, `message_key`, `resolution_state`, and `additional_info` behavior follow the Event Management event field format: [Event field format for event collection](https://www.servicenow.com/docs/r/it-operations-management/event-management/c_EMIntegrateRequirementEvent.html?contentId=MXxZKDzOzoP0FwsQ1IlNwA).
- OAuth setup follows ServiceNow inbound OAuth client credentials docs: [Client Credentials](https://www.servicenow.com/docs/r/xanadu/platform-security/authentication/client-credentials.html) and [Add the OAuth Application User](https://www.servicenow.com/docs/r/platform-security/authentication/add-oauth-application-user.html).
- Direct instance mTLS uses ServiceNow certificate-based authentication setup plus Collector HTTP client certificate settings.

The maintained bibliography is [docs/references/source-map.md](docs/references/source-map.md), including last-reviewed dates, takeaways, and notes where ServiceNow docs and real-instance behavior diverged.

## What It Supports

- Logs to ServiceNow Event Management events.
- Direct instance ingestion: `POST /api/global/em/jsonv2`.
- Direct instance Business Rules compatibility ingestion: `POST /em_event.do?JSONv2&sysparm_action=insertMultiple`.
- MID Web Server ingestion mode: `POST /api/mid/em/jsonv2`.
- MID Web Server can also be used with Business Rules compatibility intent, but the exporter still posts to `/api/mid/em/jsonv2`; the MID Server must be configured to forward events to the insertMultiple endpoint.
- Collector-native Basic, bearer token, OAuth2 client credentials, static header, TLS/client certificate, and proxy settings through `confighttp.ClientConfig` and auth extensions.
- Collector-native HTTP request compression through `confighttp` when configured. Use `compression: gzip` only on endpoint paths validated for request-body gzip; the June 2026 PDI probe validated direct instance JSON v2 but not the Business Rules compatibility path.
- Collector-native retry, timeout, queue, and queue-batching through `exporterhelper`.
- Bounded ServiceNow fields, bounded/redacted `additional_info`, deterministic `message_key`, and configurable severity mapping.

Metrics and traces are intentionally unsupported as raw exporter signals. Raw metrics do not inherently define open/update/clear Event Management semantics; convert alerts to logs upstream before this exporter, or use ServiceNow's separate OTel metrics path when that is the actual goal.

For a runnable metrics-derived event recipe, see [examples/servicenow-event-management-metrics-event-oauth.yaml](examples/servicenow-event-management-metrics-event-oauth.yaml). It accepts OTLP JSON double gauge values (`asDouble`) for `demo.checkout.error_rate`, with resource `service.name` and `host.name` plus datapoint `route`; values strictly above 90 become breach events, while equal or lower values are dropped. The explicit `message_key` uses the run ID and those dimensions, so changing only the gauge value keeps the same key. This recipe emits breaches only and does not define recovery, missing-data, or clear-event behavior. The automatic regression gate is `make test-metrics-event`; it uses local fakes and does not need ServiceNow credentials.

The optional [trace exception recipe](examples/servicenow-event-management-trace-exception-oauth.yaml) turns selected exception span events into occurrence events. It requires nonempty resource `service.name`, `host.name`, and `deployment.environment.name`, keeps only `exception.type` exactly equal to `SelectedFailure` on error spans, and trusts resource host/environment over span values. Producer ServiceNow controls and unapproved attributes are discarded; native trace/span IDs and instrumentation scope are retained in `additional_info` when present. A fixed rule ID and SHA-256 key over rule, service, host, environment, and exception type provide stable identity. It emits occurrences only, with no firing/clear/recovery state; repeated occurrences can share a key. The connector recipe does not make the exporter accept traces as a signal: it converts selected trace events to logs before export. Its maintained gate is `make test-trace-exception` and uses local fakes, without ServiceNow credentials.

## Quick Start

From a source checkout, install the pinned Collector tools and build the local distribution:

```bash
make install-tools
make build-collector
```

Run the Collector with the Basic auth example:

```bash
export SERVICENOW_INSTANCE_URL=https://example.service-now.com
export SERVICENOW_USERNAME=otel_em_exporter
export SERVICENOW_PASSWORD='...'

./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev \
  --config examples/servicenow-event-management-exporter.yaml
```

After a release tag is published, consumers can build from the tagged OCB manifest:

```bash
cp examples/collector-builder.yaml builder-config.yaml
builder --config builder-config.yaml
./otelcol-servicenow-event-management/otelcol-servicenow-event-management --config examples/servicenow-event-management-exporter.yaml
```

Send one OTLP log to the local Collector:

```bash
curl --fail-with-body --show-error --silent \
  --request POST http://127.0.0.1:4318/v1/logs \
  --header 'Content-Type: application/json' \
  --data @- <<'JSON'
{
  "resourceLogs": [{
    "resource": {
      "attributes": [
        {"key": "service.name", "value": {"stringValue": "checkout"}},
        {"key": "host.name", "value": {"stringValue": "host-01"}}
      ]
    },
    "scopeLogs": [{
      "logRecords": [{
        "severityNumber": 17,
        "severityText": "ERROR",
        "body": {"stringValue": "High error rate"},
        "attributes": [
          {"key": "event.name", "value": {"stringValue": "request.error_rate"}},
          {"key": "servicenow.severity", "value": {"intValue": "2"}}
        ]
      }]
    }]
  }]
}
JSON
```

## Collector Builder Manifest

For a released build, use a module version available from your Go module proxy. The version below is an example; replace it with the released version you intend to build:

```yaml
exporters:
  - gomod:
      github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management v0.1.0
    import: github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management/exporter/servicenoweventmanagementexporter
```

The local development manifest in [builder-config.yaml](builder-config.yaml) uses `path: .` so contributors can build from a checkout.

## Development Tools

Run `make install-tools` before `make generate`, `make build-collector`, or `make verify`. It installs the pinned OpenTelemetry Collector `builder` and `mdatagen` binaries into `.tools/bin`.

The repository pins Collector tooling with `OTEL_COLLECTOR_VERSION`, currently `v0.153.0`. `mdatagen` is built from the matching Collector source tag because the released `cmd/mdatagen` module cannot be installed with `go install ...@version` while preserving upstream replace directives.

The `go.mod` Go 1.25.0 directive is the module minimum. CI uses Go 1.25.7 for reproducible builds and verification because Collector OTTL generic linking failed in the Go 1.25.0 build; an upstream Collector Contrib report documents a related failure resolved with Go 1.25.7 ([issue #45909](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/45909#issuecomment-3855996013)). Run `GOTOOLCHAIN=go1.25.7 make install-tools verify` to use the same patch locally.

## Collector Configuration

Minimal exporter configuration:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    mode: instance
    api: jsonv2
    auth:
      authenticator: basicauth/servicenow
    message_key:
      attributes: [service.name, host.name, event.name]

extensions:
  basicauth/servicenow:
    client_auth:
      username: ${env:SERVICENOW_USERNAME}
      password: ${env:SERVICENOW_PASSWORD}
```

See [docs/configuration.md](docs/configuration.md) for the complete option reference.

## Examples

| File | Route and API | Auth path | Required environment | Validation status |
| --- | --- | --- | --- | --- |
| [examples/servicenow-event-management-trace-exception-oauth.yaml](examples/servicenow-event-management-trace-exception-oauth.yaml) | Selected trace exception occurrences through JSON v2 | `oauth2client` | Local fakes for the regression target; `SERVICENOW_*` credentials for live use | Local composition regression gate; no live instance claim |
| [examples/servicenow-event-management-exporter.yaml](examples/servicenow-event-management-exporter.yaml) | Direct instance, JSON v2 | `basicauth` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_USERNAME`, `SERVICENOW_PASSWORD` | PDI validated |
| [examples/servicenow-event-management-exporter-business-rules.yaml](examples/servicenow-event-management-exporter-business-rules.yaml) | Direct instance, Business Rules compatibility | `basicauth` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_USERNAME`, `SERVICENOW_PASSWORD` | PDI validated |
| [examples/servicenow-event-management-exporter-bearer.yaml](examples/servicenow-event-management-exporter-bearer.yaml) | Direct instance, JSON v2 | `bearertokenauth` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_BEARER_TOKEN` | PDI validated |
| [examples/servicenow-event-management-exporter-oauth.yaml](examples/servicenow-event-management-exporter-oauth.yaml) | Direct instance, JSON v2 | `oauth2client` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_TOKEN_URL`, `SERVICENOW_CLIENT_ID`, `SERVICENOW_CLIENT_SECRET` | PDI validated |
| [examples/servicenow-event-management-exporter-mid.yaml](examples/servicenow-event-management-exporter-mid.yaml) | MID Web Server, JSON v2 | `headers_setter` MID API key | `SERVICENOW_MID_URL`, `SERVICENOW_MID_API_KEY` | PDI plus local Linux MID validated |
| [examples/servicenow-event-management-metrics-event-oauth.yaml](examples/servicenow-event-management-metrics-event-oauth.yaml) | Metrics-derived breach events through JSON v2 | `oauth2client` | Local fakes for the regression target; `SERVICENOW_*` credentials for live smoke | Local composition regression gate plus opt-in live smoke |
| [examples/collector-builder.yaml](examples/collector-builder.yaml) | Collector Builder (OCB) distribution manifest | n/a | n/a | Validated by `make verify` after local Collector build |

See [docs/compatibility.md](docs/compatibility.md) for the full validation matrix and open gaps.

## Local Validation

```bash
make test
make race
make verify
make test-metrics-event
make test-trace-exception
make build-collector
make smoke-servicenow-jsonv2
make smoke-servicenow-throughput
SERVICENOW_HEC_FANOUT_DRY_RUN=1 make smoke-hec-fanout
SERVICENOW_METRICS_EVENT_DRY_RUN=1 make smoke-metrics-event
```

`make smoke-servicenow-jsonv2` is a dry run by default. It only sends to ServiceNow when `SERVICENOW_SEND=1` is set with credentials.
`make smoke-servicenow-throughput` sources `.env.local` by default, starts the local Collector, sends rich synthetic OTLP batches, and reads ServiceNow events back by run id.
`make smoke-hec-fanout` is the opt-in end-to-end demo harness for Splunk HEC input, Collector enrichment, threshold filtering, ServiceNow Event Management export, and Splunk HEC fan-out. Use `SERVICENOW_HEC_FANOUT_DRY_RUN=1` to generate the Collector config and expected TSV summaries without credentials or containers.
`make test-metrics-event` builds the maintained metrics builder manifest with this checkout as a local exporter override, then runs the maintained Collector example against local OAuth and JSON v2 fakes. It checks two resources, threshold and unrelated-metric suppression, stable identity when a value changes, mapped fields, string-valued `additional_info`, and client-credentials bearer authentication. `make verify` includes this gate and the trace gate after `make install-tools`.
`make smoke-metrics-event` remains the opt-in live metrics-derived event harness. It sends synthetic OTLP gauges through OAuth client credentials and reads back matching `em_event` rows by `message_key` prefix. Use `SERVICENOW_METRICS_EVENT_DRY_RUN=1` to generate the temporary builder config, Collector config, OTLP metrics payload, and expected ServiceNow summary without credentials.

For local ServiceNow credentials, copy [.env.example](.env.example) to `.env.local`:

```bash
set -a
source .env.local
set +a
```

`make test-trace-exception` builds a separate maintained trace-recipe distribution and exercises selected and rejected span events, trusted resource identity, native context, exact occurrence keys, complete outbound payload filtering, and propagated-transform failure with no HTTP request. The gate does not validate ServiceNow-specific alert rules or a live instance.

## Production Notes

Read [docs/production-hardening.md](docs/production-hardening.md) before using this in a shared or production environment. The short version:

- Use explicit `servicenow.*` attributes from your alert source for production event quality.
- Set stable `servicenow.message_key` values for deduplication and clear events.
- Use a least-privileged ServiceNow user with `evt_mgmt_integration`; ServiceNow documents this as required for direct instance JSON v2 ingestion.
- Prefer auth extensions or Collector secret providers; do not put credentials in configs.
- Keep batch sizes conservative until validated against your ServiceNow instance.
- Use `additional_info.include_attributes` when input logs may carry sensitive application data, and include exact custom alert field names when ServiceNow should promote Additional information keys into alert fields.
- Use [docs/troubleshooting.md](docs/troubleshooting.md) when a live smoke or Collector-backed run fails.

## Current Validation Status

Validated so far:

- Local unit, race, lint, OCB build, and example config checks.
- ServiceNow PDI direct instance JSON v2 with Basic auth, bearer token auth, and OAuth2 client credentials.
- ServiceNow PDI direct instance Business Rules compatibility with Basic auth.
- ServiceNow PDI MID JSON v2 through a local Linux MID runtime with MID API key, Basic auth, and mTLS.
- ServiceNow PDI MID Business Rules compatibility forwarding through a local Linux MID runtime configured with the upstream `insertMultiple` endpoint.

Not yet validated:

- A non-PDI/customer-like ServiceNow development instance.
- Direct instance mTLS/certificate-based authentication. A PDI attempt reached ServiceNow's CA upload path, but the instance failed to publish CA trust material and did not request client certificates.

Customer-like ServiceNow validation is a production/stable release blocker. Direct instance mTLS should remain documented as unvalidated unless a target instance can complete ServiceNow certificate-based authentication setup.

See [docs/compatibility.md](docs/compatibility.md) and [docs/servicenow-real-instance-testing.md](docs/servicenow-real-instance-testing.md).

## Project Hygiene

- [CHANGELOG.md](CHANGELOG.md)
- [SECURITY.md](SECURITY.md)
- [SUPPORT.md](SUPPORT.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [docs/release.md](docs/release.md)
- [docs/troubleshooting.md](docs/troubleshooting.md)
- [docs/otel-registry-entry.yml](docs/otel-registry-entry.yml)

## License

Apache-2.0. See [LICENSE](LICENSE).
