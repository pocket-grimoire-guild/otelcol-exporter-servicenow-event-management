# June 2026 Compression Validation

This page records a bounded, single-record PDI probe. It does not establish customer-like environment support, a request-size limit, or behavior for other ServiceNow families.

- Date: 2026-06-01 UTC
- Environment: ServiceNow PDI; family not recorded
- Evidence method: direct requests followed by `em_event` read-back

## Tested Paths

| Endpoint | Request encoding | Outcome |
| --- | --- | --- |
| Instance JSON v2, `/api/global/em/jsonv2` | gzip | HTTP 200; one event read back |
| Instance JSON v2, `/api/global/em/jsonv2` | uncompressed control | HTTP 200; one event read back |
| Instance Business Rules compatibility, `/em_event.do?JSONv2&sysparm_action=insertMultiple` | gzip | No event read back; the tested PDI returned a JSON parsing error |
| Instance Business Rules compatibility, `/em_event.do?JSONv2&sysparm_action=insertMultiple` | uncompressed control | HTTP 200; one event read back |
| MID JSON v2, `/api/mid/em/jsonv2` | gzip | Not tested; the local MID listener was unavailable |

## Limits

- Each completed request contained one record. Larger compressed batches remain unvalidated.
- The ServiceNow family was not recorded, so these outcomes are not a general compatibility guarantee.
- MID request-body compression remains unvalidated.
- Keep Business Rules compatibility requests uncompressed unless a target-specific test establishes otherwise.
- Validate compression on a customer-like instance before relying on it in production.
