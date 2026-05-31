# Evaluation Plan

This repo uses layered checks so contributors can prove progress without needing a live ServiceNow instance for every change.

## Local Verification

- `make verify`: validates JSON/YAML, shell scripts, markdown links, fenced YAML snippets, docs presence, module path, Go SPDX headers, Collector Builder output, example configs, and Go checks when `go.mod` exists.
- `make test`: runs Go tests when the module exists.
- `make race`: runs Go race tests when the module exists.
- `make lint`: runs `golangci-lint` when the module exists.
- `make build-collector`: runs the pinned OpenTelemetry Collector `builder` against `builder-config.yaml`.
- `make verify` enforces at least 90% statement coverage for `exporter/servicenoweventmanagementexporter`.
- CI and `make verify` also validate checked-in Basic, bearer, OAuth, and MID Collector examples after a successful local Collector build.
- Release readiness includes changelog, security policy, support notes, Collector Builder consumer manifest, and a draft OpenTelemetry Registry entry.

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
  - `additional_info` is encoded as a JSON string, includes extra attributes and original severity, and keeps nested values string-valued for ServiceNow alert processing.
  - `additional_info` respects include, exclude, redaction, max-attribute, max-value, and total-byte limits.
  - `time_of_event` is UTC `yyyy-MM-dd HH:mm:ss`.
  - severity mapping handles explicit ServiceNow values and OTel log severities.
  - severity mapping is configurable and clear severity can set `resolution_state`.
  - `message_key` is deterministic and falls back to default ServiceNow identity fields instead of emitting partial configured keys.
  - ServiceNow field limits are applied without splitting UTF-8 sequences.
- Transport:
  - request path and headers match endpoint mode and API flavor.
  - Collector auth extensions can decorate outbound HTTP requests.
  - fixed MID API key headers can be supplied through Collector HTTP settings.
  - a Collector log batch becomes a JSON v2 request with a `records` array.
  - multi-resource, multi-scope, multi-record log batches preserve all expected records in the ServiceNow payload.
  - empty `plog.Logs` input does not POST an empty ServiceNow request.
  - 429 and 5xx are retryable.
  - throttling behavior honors ServiceNow `Retry-After` for retryable `429` and `503` responses through Collector throttle-retry helpers.
  - exporterhelper retries a retryable HTTP status and resends the same mapped payload.
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
- payloads do not mutate incoming `pdata`.
- exporter errors do not leak request bodies, auth headers, unredacted `additional_info`, or arbitrary ServiceNow response bodies.

These tests are the preferred fake endpoint. They should use Collector exporter APIs where feasible so config, factory construction, and `exporterhelper` behavior stay close to upstream practice.

## Review Follow-Up Targets

The 2026-05-22 implementation review found coverage strong for the MVP, with exporter package coverage above the local 90% gate. The same review cycle added focused regression coverage for:

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

For exporter behavior, leave a short trail in the pull request or commit message:

- failing test command and failure reason;
- implementation command or file slice;
- passing narrow test command;
- widened verification command.

## Manual ServiceNow Validation

Use `docs/servicenow-real-instance-testing.md` as the full real-instance checklist. The short version is: prove local behavior first, POST one synthetic event to the ServiceNow JSON v2 endpoint, then run the Collector against the same instance.

To send one synthetic event to an instance endpoint:

```bash
curl --fail-with-body --show-error --silent \
  --request POST "${SERVICENOW_INSTANCE_URL%/}/api/global/em/jsonv2" \
  --user "${SERVICENOW_USERNAME}:${SERVICENOW_PASSWORD}" \
  --header 'Accept: application/json' \
  --header 'Content-Type: application/json' \
  --data '{"records":[{"source":"opentelemetry-validation","event_class":"otel-servicenow-event-management-exporter","node":"otel-local","resource":"manual-validation","metric_name":"manual_validation","type":"collector_exporter","message_key":"otel-servicenow-event-management-exporter:manual-validation","severity":"5","description":"OpenTelemetry ServiceNow Event Management exporter manual validation","additional_info":"{\"otel.signal\":\"logs\"}"}]}'
```

To send through MID JSON v2 with an API key:

```bash
curl --fail-with-body --show-error --silent \
  --request POST "${SERVICENOW_MID_URL%/}/api/mid/em/jsonv2" \
  --header "Authorization: key ${SERVICENOW_MID_API_KEY}" \
  --header 'Accept: application/json' \
  --header 'Content-Type: application/json' \
  --data '{"records":[{"source":"opentelemetry-validation","event_class":"otel-servicenow-event-management-exporter","node":"otel-local","resource":"manual-validation","metric_name":"manual_validation","type":"collector_exporter","message_key":"otel-servicenow-event-management-exporter:manual-validation","severity":"5","description":"OpenTelemetry ServiceNow Event Management exporter manual validation","additional_info":"{\"otel.signal\":\"logs\"}"}]}'
```

To validate the direct Business Rules-compatible endpoint:

```bash
curl --fail-with-body --show-error --silent \
  --request POST "${SERVICENOW_INSTANCE_URL%/}/em_event.do?JSONv2&sysparm_action=insertMultiple" \
  --user "${SERVICENOW_USERNAME}:${SERVICENOW_PASSWORD}" \
  --header 'Accept: application/json' \
  --header 'Content-Type: application/json' \
  --data '{"records":[{"source":"opentelemetry-validation","event_class":"otel-servicenow-event-management-exporter","node":"otel-local","resource":"manual-validation","metric_name":"manual_validation","type":"collector_exporter","message_key":"otel-servicenow-event-management-exporter:manual-validation","severity":"5","description":"OpenTelemetry ServiceNow Event Management exporter manual validation","additional_info":"{\"otel.signal\":\"logs\"}"}]}'
```

For MID plus Business Rules compatibility, the exporter-facing request still posts to `/api/mid/em/jsonv2`. Configure the MID Server upstream endpoint property documented by ServiceNow before treating that as Business Rules validation.

For mTLS, first prove the target endpoint requests a client certificate. Direct ServiceNow instance mTLS additionally requires ServiceNow certificate-based authentication setup; the current PDI attempt did not validate that path because CA publication failed and the endpoint continued to challenge with Basic auth. MID mTLS has been validated through a local MID runtime and uses the same Collector HTTP `tls.cert_file` / `tls.key_file` settings.

Expected result: ServiceNow accepts the JSON v2 payload and the event appears in Event Management. Direct instance JSON v2 should return JSON. MID JSON v2 can return an empty successful response, but ServiceNow read-back is still required for an end-to-end validation claim. Non-JSON responses with a body, including the ServiceNow PDI hibernation page, should be treated as live-environment failures.

For broader live validation, use environment-owned generators or integration tests that can send representative OTLP logs to the local Collector and then read back sanitized ServiceNow results. Keep these tests opt-in, credential-free by default, and record only summarized evidence in [docs/validation-evidence](validation-evidence/README.md). If a recurring live check belongs in this repository, prefer a `//go:build integration` Go test that skips cleanly when required environment variables are absent.

The metrics-derived event example is intentionally configuration-only in this public repo. Use [../examples/collector-builder-metrics-event.yaml](../examples/collector-builder-metrics-event.yaml) and [../examples/servicenow-event-management-metrics-event-oauth.yaml](../examples/servicenow-event-management-metrics-event-oauth.yaml) as the starting point when validating that pattern in a target environment. Record only sanitized read-back summaries when that validation changes a compatibility claim.

## Evidence To Record

For each major implementation phase, record:

- commands run;
- pass/fail result;
- skipped checks and why;
- any ServiceNow behavior discovered through a real instance.

For credentialed ServiceNow or MID runs, record sanitized run evidence in [docs/validation-evidence](validation-evidence/README.md) and keep [docs/compatibility.md](compatibility.md) linked to the supporting evidence.
