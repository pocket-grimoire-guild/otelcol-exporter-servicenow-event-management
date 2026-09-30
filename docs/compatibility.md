# Compatibility And Validation Matrix

This matrix separates compatibility baselines from paths exercised. Validation rows link recorded evidence; a pinned baseline is not itself a validation claim.

## Collector And Go

| Area | Version | Status |
| --- | --- | --- |
| Go | `1.25.0` module minimum; `1.25.7` selected CI/build toolchain | Earlier local validation on 2026-09-28 passed `make verify` with Go `1.25.7`; the tag workflow reruns it on the tagged commit. The module minimum remains `1.25.0`. |
| OpenTelemetry Collector | `v0.153.0` component/tool modules and `v1.59.0` stable modules | Pinned compatibility baseline for this preview; dated build and integration results are described with their evidence below. |
| Collector tools | `builder` and `mdatagen` from Collector `v0.153.0` | Installed by `make install-tools` and enforced by `make verify` |
| Component stability | development, logs only | Intentional |
| Go package import | v0.2.0 and earlier: nested package; v0.3.0: module root | Breaking pre-1.0 import migration with no compatibility package. The package name, Collector component type `servicenow_event_management`, and YAML settings remain unchanged. The generated meter and tracer scopes move to the root import path; see the [README migration examples](../README.md#go-package-migration-in-v030). |

During 2026 build validation, Go `1.25.0` hit an OTTL linker failure in the metrics distribution. CI selects Go `1.25.7` for builds following the patch-level resolution documented in an [upstream report](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/45909#issuecomment-3855996013). Earlier local validation on 2026-09-28 passed `make verify` on Go `1.25.7`; the tag workflow reruns that gate on the tagged commit. This toolchain evidence is separate from the dated historical ServiceNow validation rows below.

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

## Before Stable Or Production-Ready Claims

- Record the ServiceNow family for future real-instance tests.
- Validate at least one non-PDI or customer-like development instance.
- Record tested maximum batch size and observed response shapes.

These gaps do not block a development/public-preview release. Keep the preview label, list the open gaps in its release notes, and do not describe the exporter as production-ready or stable until the required validation rows are complete.

See [docs/servicenow-real-instance-testing.md](servicenow-real-instance-testing.md) for the validation ladder and [docs/validation-evidence](validation-evidence/README.md) for the evidence template and current archive.
