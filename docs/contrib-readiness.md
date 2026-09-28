# OpenTelemetry Contrib Readiness

This project is a standalone Collector exporter. It is shaped to be friendly to a future `opentelemetry-collector-contrib` donation, but it is not currently part of the contrib repository or contrib distribution.

## Current Local Alignment

- Uses `exporter.NewFactory` and registers only the implemented logs signal.
- Embeds Collector HTTP client configuration so auth extensions, TLS, proxy, and headers stay Collector-native.
- Uses `exporterhelper` for retry, queue, batching, start, and shutdown behavior.
- Declares non-mutating exporter capabilities and keeps mapping separate from transport.
- Keeps `metadata.yaml` and generated metadata checked in.
- Validates examples with the local Collector Builder distribution through `make verify`.

## Required Before A Contrib Proposal

- Keep the standalone public naming stable before any public tag or contrib proposal.
- Identify at least three prospective code owners by GitHub handle.
- Find an OpenTelemetry approver or maintainer sponsor from a different company.
- Prepare a proposal issue covering use cases, supported signals, config options, ServiceNow API choices, similar components, and maintenance ownership.
- Get ServiceNow product/API review for endpoint choice, field mapping, clear-event behavior, `additional_info`, role expectations, and validation claims.
- Validate against a customer-like ServiceNow dev or sub-production instance, not only a PDI.
- Decide the standalone support model: named maintainers, security escalation path, release cadence, supported Collector version matrix, ServiceNow API compatibility promise, and config deprecation policy.
- Keep the documented Go minimum and selected build toolchain aligned with the supported Collector version when updating either compatibility baseline or proposing contrib support.

## Required During A Contrib Port

- Move the component to `github.com/open-telemetry/opentelemetry-collector-contrib/exporter/servicenoweventmanagementexporter`.
- Add a contrib-style component `go.mod` and Makefile that includes the repository `Makefile.Common`.
- Replace standalone codeowner metadata with accepted contrib code owners.
- Replace standalone copyright headers with the OpenTelemetry repository convention if requested during the accepted contrib PR.
- Add the component to contrib version/build files only in the accepted PR sequence.
- Add the generated README status section using the contrib generation flow.
- Run contrib checks such as metadata validation, generated docs checks, crosslinking, generated collector builds, and multimodule verification.

## Standalone Metadata Policy

Standalone metadata must not claim inclusion in the contrib distribution. Add `distributions: [contrib]` only as part of an accepted contrib PR that enables the component in contrib builds.

## Remaining Local Preflight Work

- Keep local test coverage aligned as new mapping, routing, retry, and queue-batching behavior is added.
- Keep large test files split by concern so contrib reviewers can navigate endpoint, error, exporterhelper, severity, and `additional_info` behavior quickly.
