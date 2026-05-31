# Troubleshooting

Start by separating exporter behavior from ServiceNow environment behavior:

1. Run `make verify` for local mapping, config, fake endpoint, and example checks.
2. POST one synthetic JSON v2 event directly to ServiceNow or MID before running a full Collector pipeline.
3. Only then run Collector-backed validation against the target environment.

## Symptoms

| Symptom | First checks |
| --- | --- |
| `401` or `403` | Auth extension wiring, token expiry, OAuth Application User, REST API auth scope, `evt_mgmt_integration` role, MID API key format. |
| OAuth token request returns `access_denied` or `server_error` | Confirm `glide.oauth.inbound.client.credential.grant_type.enabled=true`, an OAuth Application User is selected, the user has `evt_mgmt_integration`, and any required REST API auth scope allows JSON v2. |
| `404` | `endpoint`, `mode`, `api`, Event Management plugin state, MID listener path, and whether a full URL names the wrong known Event Management path. |
| `400` | Capture the safe response shape, add or update a focused failing test for the rejected payload behavior, then adjust mapping or validation. Do not paste customer payloads into issues. |
| `2xx` with exporter error | Inspect the sanitized error summary for JSONv2 record failures, top-level failure status, non-JSON response, malformed JSON, or an oversized response body. ServiceNow can report per-record failure in the body even when the HTTP request succeeds. |
| `429` | Reduce input rate or batch size first. Then tune retry intervals and queue depth if delayed Event Management delivery is acceptable. |
| `503` or network errors | ServiceNow availability, proxy settings, TLS trust, Collector retry behavior, queue pressure, and ServiceNow maintenance windows. |
| Direct instance mTLS still returns Basic challenge | Verify ServiceNow certificate-based authentication, `com.glide.auth.mutual`, ADCv2 inbound mTLS support, CA publish status, user certificate mapping, and that `openssl s_client` shows a client certificate request. |
| MID mTLS handshake fails | Check MID Web Server secure-connection state, server keypair, Collector `tls.ca_file`, MID truststore contents, client certificate validity, and whether the MID Web Server/Event Listener contexts were restarted after trust changes. |
| Successful MID POST but no ServiceNow event | Confirm MID Event Listener context is started, upstream connectivity to the instance is healthy, Event Management plugin is active, and read-back is querying the right run id/time range. |
| MID Business Rules compatibility not visible | Confirm the MID forwarding properties in [MID Business Rules compatibility](mid-business-rules.md), then verify an actual Business Rules side effect after read-back. |
| Direct validation returns non-JSON HTML | Wake the PDI or fix the target URL. PDI hibernation pages and login pages are live-environment failures, not exporter successes. |
| Events are accepted but not useful | Review `servicenow.message_key`, `servicenow.node`, `servicenow.event_class`, `servicenow.metric_name`, severity, Event Management rules, and CMDB binding. |
| Clear events do not close the expected alert | Verify the clear event has the same `servicenow.message_key` as the open event, final severity `0`, and `resolution_state=Closing` or equivalent source-owned resolution semantics. |

## Useful Commands

Validate a local Collector config after `make build-collector`:

```bash
./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev validate \
  --config examples/servicenow-event-management-exporter.yaml
```

Check whether a direct ServiceNow endpoint asks for client certificates:

```bash
openssl s_client \
  -connect example.service-now.com:443 \
  -servername example.service-now.com \
  -state </dev/null
```

For live throughput or field-variety issues, use an environment-owned generator or integration test and record the run id, accepted count, read-back latency, status distribution, and field variety in the validation evidence.
