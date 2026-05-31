# Security Policy

## Supported Versions

Security fixes are provided for the latest minor release line once public releases begin. Before `v1.0.0`, users should expect to upgrade to the latest `v0.x` release for fixes.

| Version | Supported |
| --- | --- |
| `main` | development only |
| `v0.1.x` | planned |

## Reporting A Vulnerability

Do not publish vulnerability details, credential exposure details, exploit steps, private URLs, request bodies, response bodies, or customer data in a public issue.

Use GitHub private vulnerability reporting for this repository when it is available from the repository **Security** tab. If GitHub does not show a private reporting path, open a minimal public issue tagging `@traytonwhite` that says only that you need a security contact for this project; do not include technical details.

## Sensitive Data Handling

The exporter:

- Does not own ServiceNow credential flows.
- Uses Collector auth extensions and HTTP client settings for Basic, bearer, OAuth2, header-based auth, TLS, proxy, and mTLS behavior.
- Relies on ServiceNow-side certificate-based authentication setup for direct instance mTLS; client certificate files are only presented through generic Collector HTTP TLS settings.
- Redacts common secret-bearing `additional_info` attribute names by default.
- Bounds `additional_info` and ServiceNow field sizes before transport.
- Redacts ServiceNow error response bodies when they contain common secret-bearing field names.

The exporter cannot prove that arbitrary application log attributes are safe to copy. For production, prefer `additional_info.include_attributes` to allowlist attributes.

## Issue Hygiene

Never paste:

- ServiceNow credentials.
- OAuth client secrets or access tokens.
- Authorization headers.
- MID API keys.
- Full payloads containing customer data.
- Unredacted `additional_info`.
- Internal ServiceNow instance URLs if they are sensitive.
