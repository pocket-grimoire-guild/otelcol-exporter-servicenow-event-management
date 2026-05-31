# Changelog

All notable changes to this project will be documented in this file.

This project uses semantic versioning. Until `v1.0.0`, breaking changes may occur in minor releases, but they must be called out explicitly.

## Unreleased

### Added

- OpenTelemetry Collector exporter `servicenow_event_management` for logs-to-ServiceNow Event Management JSON v2.
- Direct instance mode for `/api/global/em/jsonv2`.
- MID Web Server mode for `/api/mid/em/jsonv2`.
- Collector HTTP auth extension support through `confighttp.ClientConfig`.
- Basic, bearer token, OAuth2 client credentials, and MID API key examples.
- Field limits, bounded and redacted `additional_info`, configurable severity mapping, and clear-event resolution-state support.
- Collector Builder manifests for local development and released consumer builds.
- Local verification with tests, race tests, lint, Collector Builder output, and example config validation.
- Local verification now enforces Apache-2.0 SPDX headers and at least 90% exporter package test coverage.
- ServiceNow documentation links now appear at exporter decision points, with the source map as the maintained bibliography.
- Live validation evidence now covers direct instance JSON v2 with Basic, bearer token, and OAuth2 client credentials; direct instance Business Rules compatibility with Basic auth; and MID JSON v2 with MID API key, Basic, and mTLS.
- Exporter-level retry coverage now verifies that `exporterhelper` resends after a retryable HTTP 429 response.
- Optional `api: business_rules` compatibility mode for ServiceNow's documented `em_event.do?JSONv2&sysparm_action=insertMultiple` path, kept separate from `mode: instance|mid`.

### Changed

- ServiceNow error responses are summarized without logging arbitrary response bodies.
- Non-empty 2xx ServiceNow JSONv2 responses are now bounded and inspected so record-level or top-level failures are not silently accepted.
- Release documentation now treats customer-like instance and batch-size validation as production/stable release gates, and requires direct mTLS evidence before that path is advertised as validated.
- Direct instance mTLS is documented as attempted but not validated on the current PDI because ServiceNow-side CA publication failed and the endpoint did not request client certificates.
- Direct instance mTLS documentation now calls out the ServiceNow `com.glide.auth.mutual` plugin, ADCv2 requirement, and CA publish status checks.
- Direct instance mTLS documentation now clarifies that CA registration is documented through the `sys_ca_certificate` CA Certificate Chain form; API automation would use generic Table API plus Attachment API endpoints, not an exporter-specific or ServiceNow certificate-auth-specific CA upload endpoint.
- Direct instance mTLS documentation now summarizes the PDI limitation without preserving detailed lab transcripts in public docs.

## v0.1.0 - Planned

Initial public preview release after the repository is published at its durable module path.
