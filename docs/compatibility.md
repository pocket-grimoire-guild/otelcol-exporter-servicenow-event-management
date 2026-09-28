# Compatibility And Validation Matrix

This matrix records what has actually been validated. Keep it conservative: a row means the path was exercised end-to-end and evidence was recorded.

## Collector And Go

| Area | Version | Status |
| --- | --- | --- |
| Go | `1.25.0` module baseline; `1.25.7` CI/build toolchain | Full `make verify` passes with `1.25.7`; exporter tests and the default Collector also pass with `1.25.0` |
| OpenTelemetry Collector | `v0.153.0` component/tool modules and `v1.59.0` stable modules | Default, metrics, and trace distributions build; endpoint/auth configs and maintained integration gates pass |
| Collector tools | `builder` and `mdatagen` from Collector `v0.153.0` | Installed by `make install-tools` and enforced by `make verify` |
| Component stability | development, logs only | Intentional |

Go `1.25.0` hit an OTTL linker failure in the metrics distribution. The maintained distributions and full gate pass with Go `1.25.7`, which CI selects for builds. An [upstream report](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/45909#issuecomment-3855996013) records the same linker failure and patch resolution.

## ServiceNow

| Environment | Endpoint mode | Auth | Status | Evidence |
| --- | --- | --- | --- | --- |
| ServiceNow PDI | instance JSON v2 | Basic auth | Validated, including multi-record Collector read-back | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| ServiceNow PDI | instance Business Rules compatibility, `em_event.do?JSONv2&sysparm_action=insertMultiple` | Basic auth | Validated | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| ServiceNow PDI | instance JSON v2 | bearer token | Validated | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| ServiceNow PDI | instance JSON v2 | OAuth2 client credentials | Validated | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| ServiceNow PDI plus local Linux MID runtime | MID JSON v2 | MID API key | Validated, including Collector read-back through local MID | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| ServiceNow PDI plus local Linux MID runtime | MID JSON v2 | Basic auth | Validated | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| ServiceNow PDI plus local Linux MID runtime | MID JSON v2 | mTLS | Validated | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| ServiceNow PDI plus local Linux MID runtime | MID Business Rules compatibility forwarding to `em_event.do?JSONv2&sysparm_action=insertMultiple` | MID API key | Validated with Business Rules side-effect read-back | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| Customer-like dev/sub-prod instance | instance JSON v2 | any | Not yet validated | none |

## HTTP Request Compression

| Environment | Endpoint mode | Compression | Status | Evidence |
| --- | --- | --- | --- | --- |
| ServiceNow PDI, family not recorded | instance JSON v2, `/api/global/em/jsonv2` | `Content-Encoding: gzip` | One-record probe succeeded with `em_event` read-back; larger batches and customer-like environments remain unvalidated | [June 2026 compression evidence](validation-evidence/2026-06-compression-validation.md#tested-paths) |
| ServiceNow PDI, family not recorded | instance Business Rules compatibility, `em_event.do?JSONv2&sysparm_action=insertMultiple` | `Content-Encoding: gzip` | One-record gzip probe produced no event; the uncompressed control succeeded | [June 2026 compression evidence](validation-evidence/2026-06-compression-validation.md#tested-paths) |
| Local Linux MID runtime | MID JSON v2, `/api/mid/em/jsonv2` | `Content-Encoding: gzip` | Not validated; local listener was unavailable during the probe | [June 2026 compression evidence](validation-evidence/2026-06-compression-validation.md#tested-paths) |

## Example Pipelines

| Pipeline | Export path | Status | Evidence |
| --- | --- | --- | --- |
| Metrics-derived threshold events using OTLP metrics, OTTL filter, `metricsaslogs`, OTTL log enrichment, and OAuth-backed `servicenow_event_management` export | Instance JSON v2 | Configuration example is checked in; prior PDI validation proved the pattern with two threshold-breach events read back from `em_event` | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |
| Maintained metrics-derived event recipe | Local OAuth and JSON v2 fakes | `make test-metrics-event` passes with Go `1.25.7` and Collector `v0.153.0`; no new live-instance claim | [Integration test](../tests/metricsevent/example_test.go) |
| Selected trace-exception recipe | Local OAuth and JSON v2 fakes | `make test-trace-exception` passes with Go `1.25.7` and Collector `v0.153.0`; no alert-lifecycle claim | [Integration test](../tests/traceexception/example_test.go) |

## Open Validation Gaps

- Customer-like development or sub-production ServiceNow instance validation is still required before stable or production-ready claims.

## Blocked Or Negative Evidence

These rows are not validation claims. They record tested paths that did not complete so future work can resume from the right layer.

| Environment | Endpoint mode | Auth | Result | Evidence |
| --- | --- | --- | --- | --- |
| ServiceNow PDI | instance JSON v2 | certificate-based auth / mTLS | Attempted but not validated. The PDI did not complete ServiceNow-side CA trust publication, and the public endpoint did not request a client certificate. This is consistent with ServiceNow's ADCv2/front-door requirements and public PDI reports cited in the source map. | [May 2026 blocked evidence](validation-evidence/2026-05-pdi-validation.md#blocked-or-negative-evidence) |

## Before A Public Release

- Record the ServiceNow family for future real-instance tests.
- Validate at least one non-PDI or customer-like development instance.
- Record tested maximum batch size and observed response shapes.

Do not describe the exporter as production-ready or stable until those validation rows are complete. A release with any open row must be labeled as development/public preview and must call out the exact gap in the release notes.

See [docs/servicenow-real-instance-testing.md](servicenow-real-instance-testing.md) for the validation ladder and [docs/validation-evidence](validation-evidence/README.md) for the evidence template and current archive.
