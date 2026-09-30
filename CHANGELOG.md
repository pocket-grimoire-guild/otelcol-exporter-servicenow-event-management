# Changelog

All notable changes to this project are documented here. Before `v1.0.0`, minor releases may include breaking changes when called out.

## v0.3.0 — 2026-09-30 (development/public preview)

- Move the exporter package, implementation, tests, metadata, and generated files to the Go module root import path `github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management`. The former nested Go import path is removed, which is a breaking pre-1.0 import change. The package name and Collector component ID remain the same; the instrumentation scope follows the root import path.
- Keep the Collector YAML configuration contract, exporter behavior, Go module dependencies, and Collector component/tool versions unchanged.

Compatibility and validation: this remains a standalone development/public-preview release, not a stable or production-ready release. The module minimum remains Go `1.25.0`; Go `1.25.7` is the selected build toolchain. Collector component/tool modules remain `v0.153.0`, with stable modules at `v1.59.0`. Historical ServiceNow evidence remains dated and does not establish current live-instance, alert-lifecycle, or production validation. See the [v0.2.0 release notes](docs/releases/v0.2.0.md) for the preserved historical release record.

## v0.2.0 — 2026-09-28 (development/public preview)

- Add opt-in `sha256_v1` generated message keys with framed inputs and explicit-key overflow checks; `legacy` remains the default. Changing formats changes generated identities, so operators must plan migration, close or drain old-key alerts, and account for queued or replayed events. Fitting explicit keys are unchanged; oversized explicit keys under `sha256_v1` are permanent mapping errors.
- Capture one admission timestamp for untimed records before queueing, preserving event time across retries and queue delay. This does not establish delivery or ServiceNow processing order.
- Keep otherwise mappable records when exporter metadata alone exceeds the `additional_info` budget by using a bounded reduction envelope. The marker key `otel.servicenow.additional_info.metadata_reduced` is reserved; overflow can omit optional metadata and severity text.
- Add bounded mapping diagnostics for affected record occurrences per attempt. Retries count again; these counters do not measure unique loss or confirm delivery.
- Saturate representable numeric `Retry-After` values beyond `time.Duration` range at the maximum; Collector retry budgets still apply. Recognized interruptions while reading successful 2xx acknowledgments can retry a POST already processed by ServiceNow.
- Treat JSON `null` values in recognized 2xx error fields as absent while preserving HTTP status and non-null response failure checks.
- Reject fractional and non-finite severity doubles before conversion, then use the existing severity mapping and fallback.
- Maintain optional upstream-to-logs recipes for metrics-derived breach events and selected trace exceptions, plus an optional native log-context overlay. The exporter still accepts logs only; these recipes do not define alert recovery or lifecycle behavior.

Compatibility and validation: this is a development/public-preview release. The module minimum remains Go `1.25.0`; Go `1.25.7` is the selected build toolchain following a metrics-distribution linker issue observed with `1.25.0`. Collector component/tool modules remain `v0.153.0`, with stable modules at `v1.59.0`. The tag workflow runs `make verify` on the tagged commit; historical ServiceNow evidence remains dated and does not establish current live-instance, alert-lifecycle, or production validation. See the [v0.2.0 release notes](docs/releases/v0.2.0.md).

## v0.1.0 — published baseline

The published v0.1.0 release established the logs-only exporter, endpoint and authentication examples, field mapping, bounded `additional_info`, and historical PDI/MID validation paths. See [README.md](README.md) and [docs/compatibility.md](docs/compatibility.md) for current scope and evidence.
