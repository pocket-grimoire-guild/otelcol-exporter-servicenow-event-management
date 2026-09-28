# May 2026 PDI Validation Evidence

This page summarizes sanitized validation against ServiceNow Personal Developer Instances and a local Linux MID runtime. It supports the claims in [../compatibility.md](../compatibility.md) without publishing raw transcripts, credentials, hostnames, run IDs, or temporary instance changes.

## Validated Paths

| Date | Environment | Endpoint/API | Auth path | Public evidence summary |
| --- | --- | --- | --- | --- |
| May 2026 | ServiceNow PDI | Instance JSON v2, `/api/global/em/jsonv2` | Basic auth | Direct JSON v2 POSTs and Collector-backed runs inserted events and read them back from `em_event`. Coverage included multi-record batches, bounded field behavior, parseable JSON-string `additional_info`, severity values `0` through `5`, and both `New` and `Closing` resolution states. |
| May 2026 | ServiceNow PDI | Instance JSON v2, `/api/global/em/jsonv2` | Bearer token | A ServiceNow OAuth access token supplied through the Collector bearer-token auth extension exported events successfully; read-back confirmed inserted rows and parseable `additional_info`. |
| May 2026 | ServiceNow PDI | Instance JSON v2, `/api/global/em/jsonv2` | OAuth2 client credentials | The Collector `oauth2client` extension acquired tokens from ServiceNow and exported events through this exporter; read-back confirmed inserted rows and expected mapped fields. |
| May 2026 | ServiceNow PDI | Instance Business Rules compatibility, `/em_event.do?JSONv2&sysparm_action=insertMultiple` | Basic auth | The direct compatibility endpoint returned successful JSON v2 record results and inserted Event Management rows. This supports the opt-in `api: business_rules` instance mode. |
| May 2026 | ServiceNow PDI plus local Linux MID runtime | MID JSON v2, `/api/mid/em/jsonv2` | MID API key | MID Web Server JSON v2 accepted events through a Collector headers-based MID API key configuration. Direct POSTs and Collector-backed runs inserted rows and confirmed parseable `additional_info`. |
| May 2026 | ServiceNow PDI plus local Linux MID runtime | MID JSON v2, `/api/mid/em/jsonv2` | Basic auth | MID Web Server Basic auth accepted direct and Collector-backed JSON v2 events. The exporter required no MID-specific Basic auth code beyond standard Collector auth configuration. |
| May 2026 | ServiceNow PDI plus local Linux MID runtime | MID JSON v2 over HTTPS, `/api/mid/em/jsonv2` | mTLS | MID Web Server mTLS accepted direct and Collector-backed events after the MID runtime was configured with server key material and client certificate trust material. The exporter used standard Collector HTTP TLS client certificate settings. |
| May 2026 | ServiceNow PDI plus local Linux MID runtime | MID Business Rules compatibility, exporter-facing `/api/mid/em/jsonv2` forwarded upstream to `em_event.do?JSONv2&sysparm_action=insertMultiple` | MID API key | With documented MID forwarding properties configured, MID accepted events through the normal JSON v2 listener and ServiceNow read-back confirmed the Business Rules-compatible upstream path. Temporary validation-only Business Rules and MID property changes were removed after the run. |
| May 2026 | ServiceNow PDI | Metrics-derived event example through instance JSON v2 | OAuth2 client credentials | A temporary Collector composition using OTLP metrics, OTTL filtering, `metricsaslogs`, OTTL log enrichment, and this exporter produced two threshold-breach ServiceNow events from three input datapoints. The current checkout also maintains a fake-backed regression gate and an opt-in live smoke; this row records the earlier PDI run only. |

## Blocked Or Negative Evidence

These rows are not validation claims. They document tested boundaries so future work resumes from the right layer.

| Date | Environment | Path | Public evidence summary |
| --- | --- | --- | --- |
| May 2026 | ServiceNow PDI | Direct instance certificate-based auth / mTLS | Not validated. The PDI did not complete ServiceNow-side CA trust publication and the public endpoint did not request a client certificate. Treat direct instance mTLS as unvalidated until a customer-like instance can publish CA trust material, request client certificates, map the certificate to an integration user, and accept a Collector-backed JSON v2 request. |
| May 2026 | ServiceNow PDI | Instance JSON v2 while PDI hibernated | Not a validation claim. PDI hibernation can return non-JSON HTML; treat that as a live-environment failure for direct instance mode. |
| May 2026 | ServiceNow PDI | Body-level JSON v2 failure probe | Not reproduced with safe probes. Direct JSON v2 returned normal non-2xx JSON errors for malformed request shapes. Local fake-endpoint tests remain the regression coverage for 2xx body-level ServiceNow failure parsing. |

## Open Validation Gaps

- Customer-like development or sub-production ServiceNow instance validation is still required before stable or production-ready claims.
- Direct instance mTLS remains unvalidated until a ServiceNow instance successfully publishes CA trust material, requests client certificates, maps a certificate to the integration user, and accepts a Collector-backed JSON v2 request.
- Tested maximum batch size remains environment-specific. The conservative public starting point is `100` records per ServiceNow request.

## Evidence Hygiene

- PDI validation is useful implementation evidence, not a production maximum.
- Keep raw command transcripts, exact run IDs, temporary local paths, and operational lab notes in private development records.
- Public evidence should summarize endpoint, auth path, status, read-back shape, and any implementation-relevant outcome.
