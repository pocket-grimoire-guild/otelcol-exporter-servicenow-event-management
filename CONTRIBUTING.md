# Contributing

Thanks for helping improve the ServiceNow Event Management exporter.

## Development Loop

```bash
make install-tools
make test
make race
make docs-check
make verify
make build-collector
```

`make install-tools` installs the pinned OpenTelemetry Collector `builder` and `mdatagen` binaries into `.tools/bin`; the verification targets put that directory first on `PATH`.

`make verify` runs the full local gate: YAML and shell validation, shellcheck, markdown link checks, fenced YAML validation, generated metadata checks, tests, race tests, vet, lint, Collector builder, and example config validation.

The verification gate enforces at least 90% statement coverage for `exporter/servicenoweventmanagementexporter` and requires standalone copyright plus Apache-2.0 SPDX headers on checked-in Go files.

## Licensing

This standalone repository is licensed under Apache-2.0. New Go files should use:

```go
// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0
```

If a future change copies substantial source from another Apache-2.0 project, retain the original copyright and attribution notices required by that source. A future accepted OpenTelemetry Collector Contrib donation may change ported file headers to the OpenTelemetry repository convention as part of that upstream PR.

## Component Rules

- Keep the Collector component ID `servicenow_event_management`.
- Keep the Go package path `exporter/servicenoweventmanagementexporter`.
- Use Collector helper APIs for queue, retry, timeout, auth, TLS, and HTTP behavior.
- Do not add exporter-owned Basic, bearer, OAuth, MID API key, or mTLS flows.
- Do not write directly to ServiceNow tables.
- Keep mapping pure and covered by tests.
- Do not mutate incoming `pdata`.
- Do not log credentials, authorization headers, request bodies containing secrets, or unredacted `additional_info`.

## Testing ServiceNow Changes

Local unit tests must not call ServiceNow. Use `httptest.Server` for transport behavior.

Real ServiceNow validation belongs in a sanitized entry under [docs/validation-evidence](docs/validation-evidence/README.md). Use [docs/servicenow-real-instance-testing.md](docs/servicenow-real-instance-testing.md) as the checklist.

## Pull Request Checklist

- Tests added or updated for behavior changes.
- `make verify` passes locally, or skipped tools are called out.
- Examples still validate with dummy environment values.
- Docs updated for user-visible config or behavior changes.
- No generated Collector distribution checked in.
