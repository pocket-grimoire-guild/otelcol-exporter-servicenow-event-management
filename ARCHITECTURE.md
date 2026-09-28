# ARCHITECTURE.md

This project builds an OpenTelemetry Collector exporter for ServiceNow ITOM Event Management. The exporter converts OTel log records into ServiceNow Event Management JSON v2 records and sends them through either the ServiceNow instance endpoint or a MID WebService endpoint.

## Current Codemap

- `exporter/servicenoweventmanagementexporter`: Collector exporter package.
  - `factory.go`: component type, default config, signal registration, lifecycle wiring, and `exporterhelper` integration.
  - `config.go`: typed config, validation, defaults, auth options.
  - `runtime_config.go`: private normalized route, mapping, retry, queue, and HTTP client config used after startup validation.
  - `admission.go`: captures fallback event time on a private pdata copy before helper queueing and retry.
  - `mapper.go`: OTel log traversal and ServiceNow event-record assembly.
  - `mapper_fields.go`: ServiceNow field limits and timestamp formatting.
  - `mapper_identity.go`: fallback field selection and message-key construction.
  - `mapper_severity.go`: explicit ServiceNow severity parsing and OTel severity fallback.
  - `mapper_additional_info.go`: bounded `additional_info` packing, filtering, redaction, and metadata-reduction fallback.
  - `client.go`: HTTP transport, endpoint route resolution, safe error summaries, and retry classification.
  - `metadata.yaml`: component metadata for `mdatagen`.
  - `doc.go`: package docs and the version-checked `run-mdatagen.sh` generator directive.
  - `README.md`: user-facing exporter configuration.
  - `internal/metadata`: generated component type and stability metadata.
- `examples`: Collector and ServiceNow smoke-test examples.
- `docs/product-specs`: durable product behavior and API decisions.
- `docs/validation-evidence`: sanitized real-instance and local integration evidence.
- `docs/servicenow-real-instance-testing.md`: credentialed ServiceNow validation ladder and evidence template.
- `docs/references/source-map.md`: reviewed ServiceNow and OpenTelemetry references with decision takeaways.
- `scripts`: repeatable local checks and smoke tests.

## Runtime Flow

1. Collector receives telemetry through normal receivers.
2. Processors enrich or filter telemetry before the exporter.
3. The exporter maps supported records to a JSON payload shaped as `{"records":[...]}`.
4. The HTTP client sends the payload to the configured ServiceNow endpoint.
5. Retryable failures flow through Collector retry and queue helpers; permanent mapping or authorization failures are surfaced clearly.

## Architectural Invariants

- The exporter owns mapping to ServiceNow Event Management JSON v2. It does not rely on ServiceNow table APIs.
- The factory registers logs only. Native metrics and traces export is unsupported; maintained upstream recipes convert selected telemetry to logs before export.
- Configuration is parsed and validated at startup. Runtime code should not guess missing endpoint mode, auth mode, or required mapping behavior.
- The config should embed Collector helper settings where practical, especially `timeout`, `retry_on_failure`, and `sending_queue`.
- Mapping gives the OTel event timestamp precedence over its observed timestamp. If both are zero, `ConsumeLogs` samples one admission time per call, copies the pdata batch, and sets only `ObservedTimestamp` on the untimed records before helper queueing and retry. Standalone mapping and pre-change persisted queue entries with both timestamps zero retain the mapper's current-time fallback. Queue recovery bypasses `ConsumeLogs`, so an older entry can select a new fallback on each retry; producer resubmission through `ConsumeLogs` is a new admission.
- Transport logic is testable against `httptest.Server` and never reaches real ServiceNow from unit tests.
- Timestamp preparation does not mutate caller-owned pdata. An untimed batch is deep-copied before it is passed to the helper; an all-timed or empty batch passes through without a copy. The wrapper delegates `Capabilities`, lifecycle, context, and errors to the helper, including its effective `MutatesData: true` capability when helper queue batching is enabled.
- Secret-bearing config fields, headers, request bodies, arbitrary response bodies, and unredacted event data must not be emitted in normal errors or logs.
- ServiceNow-specific optional fields belong in `additional_info` unless they are documented Event Management fields. Encode `additional_info` as a JSON string with string-valued keys so ServiceNow does not store object payloads as `[object Object]`.

## Cross-Cutting Concerns

- **Auth:** use Collector HTTP client auth idioms instead of exporter-owned credential flows. Basic auth, static bearer tokens, OAuth2, MID API keys, headers, TLS, and client certificates are handled by auth extensions or `confighttp` settings. Direct ServiceNow mTLS requires ServiceNow-side certificate-based authentication setup before the generic client certificate settings can authenticate.
- **Time:** send ServiceNow `time_of_event` in UTC as `yyyy-MM-dd HH:mm:ss`; admission time is stable through queue delay and retries for newly submitted untimed records.
- **Severity:** preserve original severity in `additional_info` when it fits; numeric OTel severity stays pinned during metadata reduction when specified, while original severity text is best-effort. Map to ServiceNow values with explicit defaults.
- **Metadata budget:** retain successful `additional_info` output; if required exporter metadata alone exceeds the configured byte budget, send a pinned signal/severity/loss-marker envelope and fit remaining whole metadata entries by documented priority. Reserve `otel.servicenow.additional_info.metadata_reduced` from producer attributes.
- **Backpressure:** use Collector queue and retry helpers rather than hand-rolled retry loops.
- **Request sizing:** ServiceNow request size is controlled by both upstream processors and exporter queue batching. Queue batching stays opt-in in the exporter default config. Checked-in examples that enable `sending_queue.batch` must set a finite `batch.max_size`, and docs must not imply that the batch processor alone caps JSON v2 request size when queue batching can merge exporter requests.
- **Throttling:** honor `Retry-After` on retryable `429` and `503` responses through `exporterhelper.NewThrottleRetry`; otherwise use normal Collector retry backoff.
- **Observability:** rely on Collector/exporterhelper telemetry for queue, retry, and request behavior. The bounded mapping-diagnostics counter records reached per-record reasons per mapping attempt with only fixed `reason` and exporter-ID `exporter` attributes; it is not delivery-loss accounting and retries repeat observations.
- **Distribution:** `builder-config.yaml` includes the local exporter by module path plus `path`; generated OCB output stays ignored.
- **Metadata:** keep `metadata.yaml` distribution and codeowner fields aligned with the repository's actual status. Standalone metadata should not imply OpenTelemetry Collector contrib ownership until a real contrib migration occurs.
- **Naming:** keep the module path, Collector Builder manifests, registry metadata, docs, generated metadata, examples, and binary names aligned. Do not rename ServiceNow-owned API or table identifiers such as `/api/global/em/jsonv2`, `/api/mid/em/jsonv2`, `em_event.do`, or `em_event`.
- **Verification:** after `make install-tools`, `make verify` enforces generated metadata reproducibility, Go tests, race tests, lint, 90% exporter package coverage, SPDX headers, an OCB build, example validation, and maintained fake-backed integration gates.
- **Real-instance validation:** local tests use fake endpoints; credentialed ServiceNow checks follow `docs/servicenow-real-instance-testing.md` and record sanitized evidence in `docs/validation-evidence`.

## Maintainability Notes

The 2026-05-22 review found the package maintainable and idiomatic for a development-stage Collector exporter. Future changes should watch these pressure points:

- Keep `mapper.go` focused on traversal and record assembly. Add new mapping policy to the focused helper files, or introduce a small named abstraction if `additional_info` packing gains more required-key, priority, or overflow policy.
- Endpoint routing uses a table keyed by `(mode, api)`. Add new endpoint flavors to that table and its tests rather than adding scattered conditionals.
- Public YAML config is normalized into private runtime config at exporter creation. Runtime send paths should use the normalized mode/API/route/client values rather than re-resolving public strings.
- Tests are split by concern to keep navigation aligned with production boundaries. Keep future client and mapper tests near the route, error, exporterhelper, severity, or `additional_info` behavior they exercise.
- The shell and `jq` smoke harness is valuable, but dense. If the rich fixture grows further, prefer moving payload generation into a small Go test/tool and keeping shell scripts as wrappers.
