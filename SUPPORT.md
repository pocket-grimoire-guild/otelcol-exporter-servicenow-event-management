# Support

This project is a community OpenTelemetry Collector exporter for ServiceNow Event Management. It is not an official ServiceNow product and is not currently part of `opentelemetry-collector-contrib`.

The prepared v0.3.0 release remains a standalone development/public preview. It moves the Go package to the module root without a compatibility package for the former nested import; the Collector component type and YAML settings stay the same.

## Where To Ask

- Use GitHub issues for reproducible bugs, feature requests, and documentation problems.
- Use discussions, if enabled, for usage questions and integration design.
- For ServiceNow product behavior, licensing, plugin activation, and MID Server configuration, use your ServiceNow support channel.
- Use [docs/troubleshooting.md](docs/troubleshooting.md) before filing a live-ingestion bug when possible.

## What To Include In Issues

For exporter bugs, include:

- Exporter version or commit SHA.
- Collector version.
- Go version, if building locally.
- ServiceNow endpoint mode: `instance` or `mid`.
- Auth extension type, without secrets.
- Sanitized Collector config.
- Sanitized error text.
- Whether the event was visible in Event Management.

Do not include credentials, authorization headers, bearer tokens, OAuth secrets, MID API keys, customer payloads, or unredacted `additional_info`.

## Known Support Boundaries

- Native metrics and traces export are unsupported; the exporter registers logs only. The maintained [metrics-to-logs recipe](examples/servicenow-event-management-metrics-event-oauth.yaml) and [trace exception recipe](examples/servicenow-event-management-trace-exception-oauth.yaml) are supported examples of upstream conversion to logs. Their local fake-backed gates do not add native signal support or establish ServiceNow alert lifecycle behavior.
- MID mode has been validated against a local Linux MID runtime connected to a PDI with API key, Basic auth, and mTLS, but customer MID topology and certificate management still need environment-specific validation.
- Direct instance mTLS was attempted on the PDI but not validated because ServiceNow-side CA publication failed and the endpoint did not request client certificates.
- Real alert correlation and CI binding depend on ServiceNow-side CMDB content, event rules, and alert rules.

## Before Official Support Claims

Before describing the exporter as officially supported or sponsorship-backed, define named maintainers, a private vulnerability reporting path, release cadence, supported Collector version matrix, ServiceNow API compatibility promise, and config deprecation policy.
