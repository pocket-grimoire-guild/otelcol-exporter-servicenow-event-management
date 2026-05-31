# Compatibility And Validation Matrix

This matrix records what has actually been validated. Keep it conservative: a row means the path was exercised end-to-end and evidence was recorded.

## Collector And Go

| Area | Version | Status |
| --- | --- | --- |
| Go | `1.25.0` module baseline | Local tests pass |
| OpenTelemetry Collector | `v0.153.0` component/tool modules and `v1.59.0` stable modules | Local tests and Collector Builder build pass |
| Collector tools | `builder` and `mdatagen` from Collector `v0.153.0` | Installed by `make install-tools` and enforced by `make verify` |
| Component stability | development, logs only | Intentional |

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

## Example Pipelines

| Pipeline | Export path | Status | Evidence |
| --- | --- | --- | --- |
| Metrics-derived threshold events using OTLP metrics, OTTL filter, `metricsaslogs`, OTTL log enrichment, and OAuth-backed `servicenow_event_management` export | Instance JSON v2 | Configuration example is checked in; prior PDI validation proved the pattern with two threshold-breach events read back from `em_event` | [May 2026 evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths) |

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
