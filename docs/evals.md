# Evaluation Plan

This repo uses layered checks so future agents can prove progress without needing a live ServiceNow instance for every change.

## Local Verification

When OCB or builder is installed, `make verify` also builds the separate maintained trace exception recipe and runs its local-fake integration gate.

- `make verify`: validates JSON/YAML, shell scripts, markdown links, fenced YAML snippets, docs presence, smoke generator dry-run, HEC fan-out harness dry-run, module path, Go SPDX headers, and Go checks when `go.mod` exists. When OCB or builder is installed, it also builds and executes the maintained metrics event example against local OAuth and JSON v2 fakes.
- `make test`: runs Go tests when the module exists.
- `make race`: runs Go race tests when the module exists.
- `make lint`: runs `golangci-lint` when the module exists.
- `make build-collector`: runs OCB against `builder-config.yaml` when OCB is installed.
- `make verify` enforces at least 90% statement coverage for the root `servicenoweventmanagementexporter` Go package.
- CI and `make verify` also validate checked-in Basic, bearer, OAuth, and MID Collector examples after a successful local Collector build.
- Release readiness includes changelog, security policy, support notes, OCB consumer manifest, and a draft OpenTelemetry Registry entry.

## Unit Test Targets

- Config validation:
  - missing endpoint fails.
  - unsupported mode fails.
  - unsupported API flavor fails.
  - malformed endpoints and mismatched endpoint-mode paths fail.
  - invalid nested mapping, field-limit, glob-pattern, queue, retry, and HTTP client settings fail at startup.
  - Collector auth extension wiring succeeds or fails through `confighttp.ClientConfig` and `host.GetExtensions()`.
  - factory exposes logs only and leaves metrics/traces unsupported until explicitly designed.
  - default source, timeout, queue, and retry settings are stable.
- Mapping:
  - log body and attributes map to ServiceNow fields.
  - `additional_info` is encoded as a JSON string, includes extra attributes and original severity when they fit, and keeps nested values string-valued for ServiceNow alert processing.
  - `additional_info` respects include, exclude, redaction, max-attribute, max-value, and total-byte limits; when required exporter metadata alone exceeds the byte budget, a pinned bounded envelope carries the reduction marker and whole optional diagnostics follow the documented priority.
  - The reserved metadata-reduction marker cannot be forged by producer values; fallback counts every optional entry once, uses actual encoded JSON size at boundaries, and does not reject otherwise mappable log records.
  - `time_of_event` is UTC `yyyy-MM-dd HH:mm:ss`, with event timestamp precedence over `ObservedTimestamp`.
  - when both timestamps are absent, admission captures one time per call on a private copy, fills only missing `ObservedTimestamp` fields, and preserves that value through queue delay, batching, and retry; separate admissions sample separately.
  - empty and all-timestamped calls do not sample the clock or copy pdata; prepared native timestamps survive protobuf round-trip, while legacy timestamp-free mapping retains its current-time fallback.
  - severity mapping handles explicit ServiceNow values and OTel log severities.
  - severity mapping is configurable and clear severity can set `resolution_state`.
  - `message_key` is deterministic and falls back to default ServiceNow identity fields instead of emitting partial configured keys.
  - legacy message-key defaults, explicit values, separator behavior, and truncation metadata remain compatible.
  - `sha256_v1` matches independent framed-byte known answers, preserves configured and default tuple boundaries/empty slots, and hashes full mapped values before field limits.
  - `sha256_v1` keeps fitting explicit keys byte-for-byte, rejects over-budget keys without leaking values, and rejects a mixed mapped batch permanently before HTTP.
  - ServiceNow field limits are applied without splitting UTF-8 sequences.
  - the generated mapping-diagnostics counter reports affected record occurrences per attempt with only the six fixed reason values and exporter ID; repeated reasons on one record deduplicate, and producer metadata cannot forge, suppress, or expand the labels.
- Transport:
  - request path and headers match endpoint mode and API flavor.
  - Collector auth extensions can decorate outbound HTTP requests.
  - fixed MID API key headers can be supplied through Collector HTTP settings.
  - a Collector log batch becomes a JSON v2 request with a `records` array.
  - multi-resource, multi-scope, multi-record log batches preserve all expected records in the ServiceNow payload.
  - the factory path sends distinct v1 keys for former delimiter/empty-slot/long-prefix collisions, preserves fitting explicit keys, and keeps firing/clear identity stable.
  - empty `plog.Logs` input does not POST an empty ServiceNow request.
  - 429 and 5xx are retryable.
  - throttling behavior honors ServiceNow `Retry-After` for retryable `429` and `503` responses through Collector throttle-retry helpers.
  - exporterhelper retries a retryable HTTP status and resends the same mapped payload.
  - exporterhelper retries recognized interruptions while reading a successful 2xx acknowledgment across the direct JSON v2, direct Business Rules, and MID listener routes; complete invalid gzip header/checksum errors, unknown read errors, response-size violations, and known ServiceNow rejection bodies remain permanent.
  - truncated and timed-out 2xx body reads retry the full callback with unchanged mapped data, including admission-captured fallback time, while retries-disabled, caller-canceled, finite-budget, and one-consumer queue-progress behavior stays bounded and observable.
  - safe exporter errors and helper retry logs omit wrapped read causes and partial response contents while retaining mode/API/status and a concise reason.
  - context cancellation and network errors are surfaced through Collector retry/permanent-error semantics.
  - permanent 4xx responses are marked permanent and include mode/API/status plus only safe response summaries.
  - non-2xx responses are summarized safely while preserving HTTP connection hygiene for large response bodies.
  - non-empty 2xx responses are bounded, inspected for ServiceNow JSONv2 record/top-level failures, and rejected safely when they are malformed, non-JSON, or too large to verify.
  - arbitrary ServiceNow response bodies are omitted from normal exporter errors.
  - endpoint paths that already name the wrong Event Management path for the selected mode/API are rejected at startup.
  - direct Business Rules compatibility uses `em_event.do?JSONv2&sysparm_action=insertMultiple`; MID Business Rules compatibility still targets `/api/mid/em/jsonv2`.

## Integration Tests Without ServiceNow

Use `httptest.Server` to assert:

- payload is `{"records":[...]}`;
- batches contain the expected number of records;
- configured Collector batch and exporter queue-batch limits can cap ServiceNow JSON v2 request size;
- retry classification is correct;
- context cancellation is honored;
- admission timestamp preparation leaves caller-owned `pdata` unchanged, copies only batches with records missing both timestamps, and leaves all-timed and empty batches on the no-copy path.
- the admission wrapper delegates lifecycle, context, errors, and the helper's effective capability; helper queue batching still reports `MutatesData: true` when configured.
- production factory retries preserve exact mixed-timestamp payload bytes with both default queueing and queueing disabled; interrupted 2xx acknowledgments preserve fallback time across a delay longer than one formatted second.
- a blocked queue consumer delays a later untimed request without changing its admission time; queue merging and splitting preserve each record's per-call time.
- the native `ObservedTimestamp` survives the pdata protobuf codec, and timestamp-free legacy mapping still uses its fallback clock.
- an overflow record inside a 100-record factory callback is delivered with healthy siblings, and queue shutdown drains that request before a later healthy request.
- exporter errors do not leak request bodies, auth headers, unredacted `additional_info`, or arbitrary ServiceNow response bodies.
- mapping diagnostics preserve reached observations through a later mapping error, count each retry attempt, remain independent of HTTP outcomes, start only after queued data reaches mapping, isolate exporter IDs under concurrent callbacks, and propagate telemetry construction errors.

These tests are the preferred fake endpoint. They should use Collector exporter APIs where feasible so config, factory construction, and `exporterhelper` behavior stay close to upstream practice.

## Selected Trace Exception Recipe

Run `make test-trace-exception` to build the checked-in trace builder manifest with this checkout as a local exporter override, then send synthetic OTLP traces through the maintained recipe to local OAuth and JSON v2 fakes. The race-enabled integration test expects exactly eight selected exception occurrences, checks same-span matching and nonmatching events, and varies service, host, and environment independently. It asserts exact occurrence keys and fields, event-time provenance, trusted resource identity, native IDs/scope including zero and empty cases, and complete outbound-body removal of producer controls and stacktrace sentinels. A second subtest injects an OTTL parse failure and requires OTLP HTTP 503 with no JSON v2 request. This verifies local Collector composition only; it does not validate a ServiceNow event rule, deduplication behavior on an instance, or live credentials.

## Optional Native Log-Context Recipe

The checked-in optional overlay has historical fixed-timestamp validation with a temporary OCB distribution containing Transform Processor v0.151.0. Current build manifests use v0.153.0; the earlier matrix is not represented as a v0.153.0 rerun. The historical acceptance path sent synthetic OTLP/HTTP logs through the receiver, checked-in Transform statements, exporter, and a loopback fake JSON v2 endpoint at `/api/global/em/jsonv2`. It compared a base-only run with the overlay run for unchanged mapped fields and `message_key`, and checked copied IDs/scope values, zero and missing sources, empty strings, scalar values, empty AnyValues (`value: {}`), JSON nulls (`value: null`) at resource and log level, log-over-resource precedence, filtering/redaction, count loss, and the 256-byte encoded `additional_info` cap. The runtime found empty and null AnyValues compare equal to `nil` in the Transform guards; with no source they remain exported as an empty string, and a log empty/null still shadows a non-nil resource value. Assertions required the expected copies even though the example uses `error_mode: ignore`; a configuration parse alone is not acceptance. This was local Collector validation and did not claim ServiceNow instance validation.

## Review Follow-Up Targets

The 2026-05-22 multi-agent review found coverage strong for the MVP, with exporter package coverage above the local 90% gate. The same review cycle added focused regression coverage for:

- queue-batching request-size caps and the risk of uncapped exporter queue batching;
- multi-resource, multi-scope, multi-record exporter payloads;
- empty `plog.Logs` inputs avoiding empty ServiceNow POSTs;
- context cancellation before send, cancellation while an HTTP request is blocked, and network errors through retry/permanent-error semantics;
- `severity.from_attribute`, valid string severity, out-of-range numeric severity, and unsupported severity-type fallback cases;
- deterministic mapper fallback time coverage without relying on wall-clock parsing;
- `additional_info.exclude_attributes` plus array, map, and bytes-valued attributes;
- invalid queue and retry config validation;
- bounded large non-2xx response-body draining while still omitting unsafe response bodies.
- bounded 2xx ServiceNow JSONv2 response-body inspection for record-level `__status: failure`, top-level failures, malformed/non-JSON responses, oversized responses, and exporter-level `ConsumeLogs` propagation.

Remaining local release-hardening coverage should focus on any new behavior. Do not broaden ServiceNow throttling claims beyond `Retry-After` handling on `429` and `503` without target-instance validation.

## TDD Evidence

For exporter behavior, keep the regression test beside the changed code and describe the user-visible contract in the product docs. When real-instance evidence changes, update the sanitized evidence file and compatibility matrix.

## Manual ServiceNow Smoke

Use `docs/servicenow-real-instance-testing.md` as the full real-instance checklist. The short version is: prove local behavior first, POST one synthetic event to the ServiceNow JSON v2 endpoint, then run the Collector against the same instance.

Use `make smoke-servicenow-jsonv2` for a dry run. To send one synthetic event to an instance endpoint:

```bash
SERVICENOW_SEND=1 \
SERVICENOW_INSTANCE_URL=https://example.service-now.com \
SERVICENOW_USERNAME=... \
SERVICENOW_PASSWORD=... \
make smoke-servicenow-jsonv2
```

To send through MID JSON v2 with an API key:

```bash
SERVICENOW_SEND=1 \
SERVICENOW_MODE=mid \
SERVICENOW_INSTANCE_URL=https://mid-host.example.net:8443 \
SERVICENOW_MID_API_KEY=... \
make smoke-servicenow-jsonv2
```

The smoke script only uses `SERVICENOW_MID_API_KEY` when `SERVICENOW_MODE=mid`. Instance-mode smokes use bearer token auth when `SERVICENOW_BEARER_TOKEN` is set, otherwise Basic auth from `SERVICENOW_USERNAME`/`SERVICENOW_PASSWORD`.

To smoke the direct Business Rules-compatible endpoint:

```bash
SERVICENOW_SEND=1 \
SERVICENOW_MODE=instance \
SERVICENOW_API=business_rules \
SERVICENOW_INSTANCE_URL=https://example.service-now.com \
SERVICENOW_USERNAME=... \
SERVICENOW_PASSWORD=... \
make smoke-servicenow-jsonv2
```

For MID plus Business Rules compatibility, keep `SERVICENOW_MODE=mid` and set `SERVICENOW_API=business_rules`; the smoke command still posts to `/api/mid/em/jsonv2`. Configure the MID Server upstream endpoint property documented by ServiceNow before treating that as Business Rules validation.

For mTLS, first prove the target endpoint requests a client certificate. Direct ServiceNow instance mTLS additionally requires ServiceNow certificate-based authentication setup; the current PDI attempt did not validate that path because CA publication failed and the endpoint continued to challenge with Basic auth. MID mTLS has been validated through a local MID runtime and uses the same Collector HTTP `tls.cert_file` / `tls.key_file` settings.

Expected result: ServiceNow accepts the JSON v2 payload, the smoke command prints a JSON response for direct instance mode or a synthetic success marker for an empty MID success response, and the event appears in Event Management. Empty successful responses are accepted only for `SERVICENOW_MODE=mid`. Non-JSON responses with a body, including the ServiceNow PDI hibernation page, should be treated as live-environment failures.

After one-event validation, `make smoke-otel-rich-batches` can exercise a local Collector with varied fields and `additional_info` types. Start the Collector first, then run the target; it sends OTLP logs to `http://127.0.0.1:4318/v1/logs` by default. The script supports `OTEL_SMOKE_TOTAL_EVENTS`, `OTEL_SMOKE_BATCHES`, `OTEL_SMOKE_AUTH_LABEL`, `OTEL_SMOKE_SOURCE_PREFIX`, and `OTEL_SMOKE_SCENARIO` so one fixture can cover Basic and OAuth paths with run-specific labels.

For live throughput validation, `make smoke-servicenow-throughput` starts the local Collector, sends rich batches, polls `em_event` by run id, and summarizes read-back count plus unique `source`, `event_class`, `node`, `resource`, `metric_name`, `type`, `severity`, and `resolution_state` values. Use conservative totals on PDIs and record only a concise sanitized result in `docs/validation-evidence`, then update `docs/compatibility.md`.

For the full fan-out demo, use `make smoke-hec-fanout`. The harness builds a temporary Collector distribution with `splunk_hec` receiver, transform and filter processors, this ServiceNow exporter, and `splunk_hec` exporter. The default scenario sends 50 Splunk HEC event-mode messages to the Collector with `sourcetype=otel:servicenow-hec-fanout`, enriches them in the Collector, filters ServiceNow output to `metric_value > 90`, exports all 50 messages to Splunk, and reads back 10 ServiceNow events plus 50 Splunk events.

Start with a no-send dry run:

```bash
SERVICENOW_HEC_FANOUT_DRY_RUN=1 make smoke-hec-fanout
```

For a live OAuth-backed ServiceNow run with a temporary Splunk container, load `.env.local`, then opt in to Splunk's license terms explicitly:

```bash
set -a
source .env.local
set +a

SERVICENOW_HEC_FANOUT_AUTH=oauth2client \
SERVICENOW_HEC_FANOUT_ACCEPT_SPLUNK_LICENSE=1 \
make smoke-hec-fanout
```

The live harness writes `*-sent.tsv`, `*-servicenow.tsv`, and `*-splunk.tsv` under its work directory and prints their paths. It requires ServiceNow read-back credentials for `em_event` verification. Set `SERVICENOW_HEC_FANOUT_START_SPLUNK=0` plus `SPLUNK_HEC_ENDPOINT`, `SPLUNK_HEC_TOKEN`, `SPLUNK_MANAGEMENT_URL`, and `SPLUNK_PASSWORD` to target an existing Splunk instance instead of starting a temporary container.

For an automatic local composition regression check, use `make test-metrics-event`. It derives a temporary OCB config from `examples/collector-builder-metrics-event.yaml`, replacing only the output location and exporter module with this checkout. It then runs `examples/servicenow-event-management-metrics-event-oauth.yaml` with loopback listener overrides and local OAuth/JSON v2 fakes. The fixture covers OTLP JSON `asDouble` gauge points for `demo.checkout.error_rate` at 91.5, 90, 89.5, 94.25, and 93.25 across two resources, plus an unrelated high metric. Exactly three breach records must arrive after graceful shutdown: the boundary and lower points and unrelated metric are suppressed; changing the first resource's value keeps its explicit key. The check asserts every mapped event field, string-valued `additional_info`, client-credentials token exchange, bearer authorization, and `POST /api/global/em/jsonv2`. The recipe contract depends on `service.name`, `host.name`, and datapoint `route`; identity is stable for a fixed run ID and dimensions. It covers breach creation only, not recovery, missing-data, or clear-event lifecycle behavior. It requires OCB or builder and yq, uses no ServiceNow credentials, and is run automatically by `make verify` when builder tooling is available.

For real-instance validation of the metrics-derived event path, use the opt-in `make smoke-metrics-event` harness. It builds a temporary Collector distribution with the OTLP receiver, filter processor, `metricsaslogs` connector, transform processor, OAuth2 client auth extension, and this ServiceNow exporter. It sends three synthetic OTLP gauge datapoints for `demo.checkout.error_rate`, drops the datapoint at or below the threshold, exports the two surviving datapoints as ServiceNow Event Management events, and reads back `em_event` rows by `message_key` prefix.

```bash
SERVICENOW_METRICS_EVENT_DRY_RUN=1 make smoke-metrics-event
```

Expected dry-run result: temporary builder config, Collector config, OTLP metrics payload, and a two-row expected ServiceNow TSV are created.

Live OAuth run:

```bash
source .env.local
make smoke-metrics-event
```

Expected live result: the Collector obtains a ServiceNow token through `oauth2client`, the OTLP metrics request succeeds, and ServiceNow read-back returns two `em_event` rows with `source=opentelemetry-metrics-threshold`, `metric_name=demo.checkout.error_rate`, `severity=3`, and parseable `additional_info`.

## Evidence To Record

For credentialed ServiceNow or MID runs, record the endpoint, auth path, outcome, and relevant limits in [docs/validation-evidence](validation-evidence/README.md). Keep raw transcripts, exact run IDs, credentials, and operational setup notes private. Update [docs/compatibility.md](compatibility.md) to link to evidence for any changed matrix row.
