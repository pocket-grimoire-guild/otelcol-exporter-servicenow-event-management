# OpenTelemetry Collector Exporter for ServiceNow Event Management

`servicenow_event_management` is an OpenTelemetry Collector exporter that turns OTel log records into ServiceNow ITOM Event Management events.

It is intended for teams that want to build their own custom Collector distribution with the OpenTelemetry Collector Builder and send alert-like events from OpenTelemetry pipelines into ServiceNow Event Management.

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
- Collector-native retry, timeout, queue, and queue-batching through `exporterhelper`.
- Bounded ServiceNow fields, bounded/redacted `additional_info`, deterministic `message_key`, and configurable severity mapping.

Metrics and traces are intentionally unsupported as raw exporter signals. Raw metrics do not inherently define open/update/clear Event Management semantics; convert alerts to logs upstream before this exporter, or use ServiceNow's separate OTel metrics path when that is the actual goal.

For a metrics-derived event configuration example, see [examples/servicenow-event-management-metrics-event-oauth.yaml](examples/servicenow-event-management-metrics-event-oauth.yaml). It filters OTLP metric datapoints over a threshold, converts the surviving datapoints to logs with `metricsaslogs`, enriches those logs with ServiceNow event fields, and exports through OAuth client credentials. The exporter still receives logs, not raw metrics.

## Quick Start

From a source checkout before the first public tag, build the local Collector distribution:

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

After a release tag is published, consumers can build from the tagged Collector Builder manifest:

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

For released versions, pin a tag. The `v0.1.0` snippet below is the planned first public tag and should be used only after that tag exists:

```yaml
exporters:
  - gomod:
      github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management v0.1.0
    import: github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management/exporter/servicenoweventmanagementexporter
```

The local development manifest in [builder-config.yaml](builder-config.yaml) uses `path: .` so contributors can build from a checkout.

## Development Tools

Run `make install-tools` before `make generate`, `make build-collector`, or `make verify`. It installs the pinned OpenTelemetry Collector `builder` and `mdatagen` binaries into `.tools/bin`.

The repository pins Collector tooling with `OTEL_COLLECTOR_VERSION`, currently `v0.153.0`. `mdatagen` is installed from a temporary checkout of the matching Collector tag because the released `cmd/mdatagen` module cannot be installed with `go install ...@version` while preserving upstream replace directives.

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
| [examples/servicenow-event-management-exporter.yaml](examples/servicenow-event-management-exporter.yaml) | Direct instance, JSON v2 | `basicauth` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_USERNAME`, `SERVICENOW_PASSWORD` | PDI validated |
| [examples/servicenow-event-management-exporter-business-rules.yaml](examples/servicenow-event-management-exporter-business-rules.yaml) | Direct instance, Business Rules compatibility | `basicauth` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_USERNAME`, `SERVICENOW_PASSWORD` | PDI validated |
| [examples/servicenow-event-management-exporter-bearer.yaml](examples/servicenow-event-management-exporter-bearer.yaml) | Direct instance, JSON v2 | `bearertokenauth` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_BEARER_TOKEN` | PDI validated |
| [examples/servicenow-event-management-exporter-oauth.yaml](examples/servicenow-event-management-exporter-oauth.yaml) | Direct instance, JSON v2 | `oauth2client` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_TOKEN_URL`, `SERVICENOW_CLIENT_ID`, `SERVICENOW_CLIENT_SECRET` | PDI validated |
| [examples/servicenow-event-management-exporter-mid.yaml](examples/servicenow-event-management-exporter-mid.yaml) | MID Web Server, JSON v2 | `headers_setter` MID API key | `SERVICENOW_MID_URL`, `SERVICENOW_MID_API_KEY` | PDI plus local Linux MID validated |
| [examples/servicenow-event-management-metrics-event-oauth.yaml](examples/servicenow-event-management-metrics-event-oauth.yaml) | Metrics-derived threshold events converted to logs | `oauth2client` | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_TOKEN_URL`, `SERVICENOW_CLIENT_ID`, `SERVICENOW_CLIENT_SECRET`, `SERVICENOW_METRICS_EVENT_RUN_ID` | Configuration example |
| [examples/collector-builder.yaml](examples/collector-builder.yaml) | Collector Builder distribution manifest | n/a | n/a | Validated by `make verify` after local Collector build |
| [examples/collector-builder-metrics-event.yaml](examples/collector-builder-metrics-event.yaml) | Collector Builder manifest for the metrics-derived example | n/a | n/a | Configuration example |

See [docs/compatibility.md](docs/compatibility.md) for the full validation matrix and open gaps.

## Local Validation

```bash
make test
make race
make verify
make build-collector
```

The metrics-derived event files are checked-in examples, not committed live test tooling. Use [examples/collector-builder-metrics-event.yaml](examples/collector-builder-metrics-event.yaml) with [examples/servicenow-event-management-metrics-event-oauth.yaml](examples/servicenow-event-management-metrics-event-oauth.yaml) when you want to build and validate that pattern in your own test environment.

For local ServiceNow credentials, copy [.env.example](.env.example) to `.env.local`:

```bash
set -a
source .env.local
set +a
```

## Production Notes

Read [docs/production-hardening.md](docs/production-hardening.md) before using this in a shared or production environment. The short version:

- Use explicit `servicenow.*` attributes from your alert source for production event quality.
- Set stable `servicenow.message_key` values for deduplication and clear events.
- Use a least-privileged ServiceNow user with `evt_mgmt_integration`; ServiceNow documents this as required for direct instance JSON v2 ingestion.
- Prefer auth extensions or Collector secret providers; do not put credentials in configs.
- Keep batch sizes conservative until validated against your ServiceNow instance.
- Use `additional_info.include_attributes` when input logs may carry sensitive application data, and include exact custom alert field names when ServiceNow should promote Additional information keys into alert fields.
- Use [docs/troubleshooting.md](docs/troubleshooting.md) when live or Collector-backed validation fails.

## Current Validation Status

Validated so far:

- Local unit, race, lint, Collector Builder, and example config checks.
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
