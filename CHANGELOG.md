# Changelog

All notable changes to this project are documented here. Before `v1.0.0`, minor releases may include breaking changes when called out.

## Unreleased — development/public preview

### Added

- Optional `sha256_v1` generated message keys with framed inputs and explicit-key overflow checks. The existing `legacy` format remains the default.
- A bounded mapping-diagnostics counter with six fixed reasons and low-cardinality attributes.
- Maintained fake-backed integration gates for the metrics-to-logs and selected trace-exception recipes, plus an optional native log-context overlay.

### Changed

- Fractional and non-finite severity doubles are rejected before conversion and use the existing mapping/default fallback.
- Untimed log records capture one admission timestamp before queueing, so retries and queue delay keep a stable event time.
- When required exporter metadata exceeds the `additional_info` byte budget, otherwise mappable records use a bounded metadata-reduction envelope. The reserved marker key is `otel.servicenow.additional_info.metadata_reduced`.
- `additional_info` sizing now avoids repeated whole-map serialization while preserving byte-for-byte packing output.
- Recognized interruptions while reading a successful 2xx acknowledgment can enter Collector retry handling when the bounded read was incomplete. A retry can replay a POST whose response was interrupted.
- Representable numeric `Retry-After` values beyond `time.Duration` range saturate at the representable maximum; Collector retry budgets still apply.

### Compatibility and migration

- `sha256_v1` changes generated identities. Keep `legacy` during migration planning; close or drain old-key alerts and account for queued or replayed events before changing formats. Fitting explicit keys remain unchanged; an oversized explicit key is a permanent mapping error.
- Diagnostics count affected record occurrences per mapping attempt. Each fixed reason is counted at most once per record and attempt; retries count again. This is not a unique-loss count or a delivery acknowledgment.
- Rename producer use of `otel.servicenow.additional_info.metadata_reduced` because the key is reserved. Optional metadata and original severity text can be omitted on the overflow path.
- Metrics and traces remain unsupported as native exporter signals. The maintained recipes convert selected upstream telemetry into logs before this exporter receives it; their local fake-backed gates do not prove ServiceNow alert lifecycle behavior.

## v0.1.0 source baseline

The existing baseline documents the logs-only exporter, endpoint and authentication examples, field mapping, bounded `additional_info`, and historical PDI/MID validation paths. See [README.md](README.md) and [docs/compatibility.md](docs/compatibility.md) for current scope and evidence.
