# ServiceNow Real Instance Testing

This guide is the handoff point between local exporter confidence and ServiceNow-specific validation. Local tests prove mapping, Collector wiring, auth-extension integration, queueing, retry classification, and JSON v2 request shape. A real instance proves that a specific ServiceNow environment accepts the payload and turns it into Event Management data.

## Environment Options

Prefer a customer-owned development, test, or sub-production ServiceNow instance with ITOM Event Management enabled. It gives the clearest signal because plugin availability, roles, event rules, alert rules, CMDB binding, and licensing match the environment the exporter will serve.

A Personal Developer Instance may work, but do not assume it will. ServiceNow documents that most PDI plugins can be activated, while some plugins are not available on PDIs or do not appear when ServiceNow personnel must activate them. Event Management is the `com.glideapp.itom.snac` plugin; ServiceNow's [components installed with Event Management](https://www.servicenow.com/docs/r/it-operations-management/event-management/r_InstalledWithEventManagement.html?contentId=iv0GcGqwhIDS3Z8KaMTMbQ) page documents the `evt_mgmt_integration` role and the `em_event` table. Treat a PDI as usable only after verifying the plugin is visible or active and the Event Management API endpoint is available.

MID Server validation is a separate deployment mode. Use it when the target customer routes events through MID WebService. ServiceNow documents MID JSON v2 at [`/api/mid/em/jsonv2`](https://www.servicenow.com/docs/r/it-operations-management/event-management/event-collection-via-MID-using-push.html). The exporter should still use Collector HTTP client auth idioms; MID API keys can be supplied with `headers_setter` or Collector HTTP `headers`.

Business Rules compatibility is a separate API flavor, not a deployment mode. ServiceNow documents that `/api/global/em/jsonv2` does not invoke Business Rules on the event table and that `em_event.do?JSONv2&sysparm_action=insertMultiple` can be used when Business Rules should run, at lower performance. For MID, the exporter still posts to `/api/mid/em/jsonv2`; configure the MID Server properties in [MID Business Rules compatibility](mid-business-rules.md) before claiming MID Business Rules validation.

## Prerequisites

- ServiceNow base URL, for example `https://example.service-now.com`.
- ServiceNow release family and instance type, recorded in the execution plan.
- Event Management plugin state verified by plugin name or ID: `Event Management` / `com.glideapp.itom.snac`.
- Integration user with the least-privileged ingestion role, normally `evt_mgmt_integration`.
- One supported Collector auth path: `basicauth`, `bearertokenauth`, `oauth2client`, `headers_setter`, static HTTP headers, or HTTP TLS client certificates after ServiceNow-side certificate setup.
- For MID mode, a reachable MID Web Server endpoint with the MID Web Server extension configured.
- For Business Rules compatibility through MID, MID properties set for the upstream `insertMultiple` endpoint before running smoke tests.

Do not store credentials in tracked repository files. For local testing, use an untracked `.env.local` file based on `.env.example`; for shared or deployed environments, use Collector secret providers or the platform secret manager.

Load local credentials before running smoke or Collector commands:

```bash
set -a
source .env.local
set +a
```

## OAuth Client Credentials Setup

The exporter should use Collector's `oauth2client` extension for OAuth. Do not add exporter-owned token-fetching code.

For ServiceNow inbound OAuth client credentials, verify the ServiceNow OAuth application before testing the exporter. The relevant ServiceNow docs are [OAuth Inbound](https://www.servicenow.com/docs/r/platform-security/authentication/oauth-inbound.html), [Client Credentials](https://www.servicenow.com/docs/r/xanadu/platform-security/authentication/client-credentials.html), and [Add the OAuth Application User](https://www.servicenow.com/docs/r/platform-security/authentication/add-oauth-application-user.html).

- The OAuth 2.0 plugin is active and the OAuth client exists.
- The inbound client credentials system property `glide.oauth.inbound.client.credential.grant_type.enabled` exists and is set to `true`.
- The OAuth client form includes `OAuth Application User`.
- The OAuth client has an OAuth Application User selected.
- That application user has the Event Management ingestion role, normally `evt_mgmt_integration`.
- Any REST API auth scope required by the instance allows access to Event Management JSON v2.

On a PDI, the client credentials property may need to be created manually:

```text
Table: sys_properties
Name:  glide.oauth.inbound.client.credential.grant_type.enabled
Type:  true | false
Value: true
```

If the OAuth client is marked securely scoped, map an auth scope to the OAuth entity before testing Event Management. For a PDI smoke test, `useraccount` may be the only available scope and grants broad REST API access. For production, create a narrower REST API Auth Scope for the Event Management JSON v2 API when the instance supports that policy shape.

Collector `oauth2client` can request scopes when needed:

```yaml
extensions:
  oauth2client/servicenow:
    scopes: [useraccount]
```

Set the token URL in `.env.local`:

```bash
SERVICENOW_TOKEN_URL=https://example.service-now.com/oauth_token.do
```

Then validate the OAuth Collector config before sending data:

```bash
./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev \
  validate --config examples/servicenow-event-management-exporter-oauth.yaml
```

## Direct Instance mTLS Setup

ServiceNow's direct instance mTLS path is certificate-based authentication for REST APIs. It is configured on the ServiceNow instance, not in this exporter. The exporter should keep using Collector HTTP client TLS settings; do not add exporter-owned certificate-auth code.

The ServiceNow-side setup must be completed before a Collector client certificate can authenticate:

- Confirm the Certificate-based authentication plugin `com.glide.auth.mutual` is active. Its documented tables are `sys_user_certificate`, `sys_ca_certificate`, and `sys_ca_certificate_api_track`.
- Use `sso_config_admin` for certificate-based authentication setup. Admin can install the plugin when it is available for self-service activation.
- Confirm the instance is on an ADCv2 load balancer. ServiceNow's setup docs direct non-ADCv2 instances to Now Support.
- Register the client CA and publish it through ServiceNow's certificate-based authentication setup.
- Wait for the CA certificate publish status to report the load-balancer trust material exists before testing the client certificate path.
- Map the client certificate to the intended ServiceNow integration user.
- Give that user the least-privileged Event Management ingestion role, normally `evt_mgmt_integration`.
- Confirm certificate-based authentication is enabled in Certificate Based Authentication > Properties.
- Verify that `openssl s_client` against the ServiceNow hostname shows a client certificate request before treating `tls.cert_file` and `tls.key_file` as an auth path.

The ServiceNow certificate-based authentication docs describe CA registration through the `sys_ca_certificate` CA Certificate Chain form, not through a dedicated CA-upload REST API. If this setup is automated with the generic REST APIs, the equivalent path is to create a `sys_ca_certificate` record with the Table API, then upload the PEM CA bundle as an attachment to that record with the Attachment API before the certificate normalization/publish logic runs. The same ServiceNow setup page says CA Cert records are automatically synced with the load balancer, so CA publication depends on instance/front-door infrastructure in addition to table data. The prior PDI validation attempted both a Table API write to `sys_ca_certificate` and the documented UI form upload path. The Table API write was blocked by ACLs; the UI form upload reached ServiceNow's certificate normalization logic, then failed while publishing trust material to the instance truststore/load-balancer path.

Environment checks on 2026-05-16 showed the PDI root response header `Server: snow_adc`, which is consistent with an ADCv2 front door. The official `/adcv2/supports_tls` status page did not return `true`; it redirected to `/session_timeout.do`, so inbound mTLS enablement is still not confirmed. ServiceNow's inbound mTLS enablement docs say `true` is the success condition and that any other response means inbound mTLS is not activated and requires a ServiceNow support case. Public ServiceNow Community reports from developer/PDI instances show similar symptoms: one PDI CA Certificate Chain upload failed with `Connection refused` and `Failed to load truststore`, and another developer-instance probe returned HTTP 200 with an empty `/adcv2/supports_tls` response instead of `true`. These posts are not official product guarantees, but they corroborate the repo's conclusion that direct instance mTLS is not practically validated on this PDI without ServiceNow-side front-door/support action. The `sso_config_admin` role exists on the PDI and was assigned to the configured `admin` user on 2026-05-16. The `otel_em_exporter` integration user does not have `sso_config_admin`, which is expected because it should only need Event Management ingestion permissions. After `sso_config_admin` was assigned, a retry staged the public CA as an uppercase `.PEM` attachment on a new `sys_ca_certificate` sys_id and then inserted the CA record through the Table API. The attachment step succeeded, the insert reached ServiceNow's `normalize CA certificate` business rule, and ServiceNow again failed while loading/publishing trust material with `Failed to load truststore` and `Connection refused`. No `sys_ca_certificate`, `sys_attachment`, or `sys_ca_certificate_api_track` rows remained after the aborted insert.

Collector configuration stays generic:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    mode: instance
    tls:
      cert_file: /path/to/client-cert.pem
      key_file: /path/to/client-key.pem
```

The PDI attempt on 2026-05-16 did not validate direct instance mTLS. The certificate-auth tables existed, which is evidence that `com.glide.auth.mutual` was installed, and a disposable CA/client cert was generated. ServiceNow's CA upload business rule still failed while updating the instance truststore/load-balancer path. A TLS probe showed no client certificate request, and a cert-only POST to `/api/global/em/jsonv2` returned HTTP 401 with a Basic challenge. Treat this as an environment limitation until a customer-like instance can return `/adcv2/supports_tls=true`, publish the CA, and request client certificates successfully. See [source map](references/source-map.md) for the official ServiceNow ADCv2/mTLS docs and the public PDI/developer-instance community reports that match this failure pattern.

## Validation Ladder

1. Run local unit and integration tests:

```bash
make test
make verify
```

2. Run the dry-run JSON v2 smoke payload:

```bash
make smoke-servicenow-jsonv2
```

3. POST one direct JSON v2 smoke event to ServiceNow. This validates credentials and the ServiceNow API independently of the exporter:

```bash
SERVICENOW_SEND=1 \
SERVICENOW_INSTANCE_URL=https://example.service-now.com \
SERVICENOW_USERNAME=... \
SERVICENOW_PASSWORD=... \
make smoke-servicenow-jsonv2
```

The command should print a JSON ServiceNow response for direct instance mode. MID JSON v2 can return an empty successful response; the smoke script treats that as success only in `SERVICENOW_MODE=mid` and prints a synthetic success marker. If it reports a non-JSON response with a body, such as the PDI hibernation page, wake or fix the target before continuing to Collector-backed live tests.

For direct Business Rules compatibility:

```bash
SERVICENOW_SEND=1 \
SERVICENOW_API=business_rules \
SERVICENOW_INSTANCE_URL=https://example.service-now.com \
SERVICENOW_USERNAME=... \
SERVICENOW_PASSWORD=... \
make smoke-servicenow-jsonv2
```

This targets `em_event.do?JSONv2&sysparm_action=insertMultiple` directly. Use this only when the validation goal is compatibility with customer Business Rules on `em_event`; the default JSON v2 endpoint remains preferred for throughput.

The current PDI validated this direct compatibility endpoint with Basic auth on 2026-05-16. An earlier HTTP 401 was traced to the smoke script selecting `SERVICENOW_MID_API_KEY` before Basic auth even in `SERVICENOW_MODE=instance`; the script now uses MID API-key auth only in `SERVICENOW_MODE=mid`.

For bearer-token auth:

```bash
SERVICENOW_SEND=1 \
SERVICENOW_INSTANCE_URL=https://example.service-now.com \
SERVICENOW_BEARER_TOKEN=... \
make smoke-servicenow-jsonv2
```

For a short-lived ServiceNow OAuth access token used as a static bearer test token, request the token outside the exporter and pass only the resulting access token to the Collector `bearertokenauth` extension:

```bash
export SERVICENOW_BEARER_TOKEN="$(
  curl --silent --show-error --fail \
    --request POST "${SERVICENOW_TOKEN_URL}" \
    --header "Content-Type: application/x-www-form-urlencoded" \
    --data-urlencode "grant_type=client_credentials" \
    --data-urlencode "client_id=${SERVICENOW_CLIENT_ID}" \
    --data-urlencode "client_secret=${SERVICENOW_CLIENT_SECRET}" \
    --data-urlencode "scope=useraccount" \
  | jq -r '.access_token'
)"
```

Do not print the token. This path validates static bearer header injection; prefer Collector `oauth2client` for long-running token refresh.

For MID API-key auth:

```bash
SERVICENOW_SEND=1 \
SERVICENOW_MODE=mid \
SERVICENOW_INSTANCE_URL=https://mid-host.example.net:8443 \
SERVICENOW_MID_API_KEY=... \
make smoke-servicenow-jsonv2
```

For MID Business Rules compatibility, set `SERVICENOW_API=business_rules` only after configuring the MID Server upstream endpoint properties in [MID Business Rules compatibility](mid-business-rules.md). The smoke command still sends to the MID listener URL, so a successful POST proves only that the exporter reached MID unless the MID properties and ServiceNow read-back evidence confirm `insertMultiple` forwarding.

The PDI plus local Linux MID runtime validated this forwarding path on 2026-05-29 UTC. The run set `mid.probe.event.endpoint.url=em_event.do?JSONv2%26sysparm_action=insertMultiple`, `mid.probe.event.bulk_size=100`, and `mid.probe.event.queue.compress=false` on the MID Server, restarted the MID runtime, proved direct `insertMultiple` with a temporary marker-scoped `em_event` Business Rule, then sent through `/api/mid/em/jsonv2` using both the direct smoke harness and a Collector-backed `api: business_rules` run. Both MID paths read back the same Business Rule side effect from `em_event`.

When validating against a Personal Developer Instance, the PDI is only the ServiceNow instance side of the connection. The MID Server runtime still has to run on a separate host or local container. The MID Web Server Event Management path requires both a started MID Web Server context and a started MID WebService Event Listener context. For current API-key auth, create a `mid_webserver_api_key_credentials` credential scoped to the MID Server and send requests with `Authorization: Key <32-character-key>`.

Checked-in guidance and PDI context records do not mean a local MID runtime is present in a fresh worktree. If `SERVICENOW_MID_URL` points to `127.0.0.1`, confirm a local listener is running before treating connection-refused errors as exporter behavior. A recreated runtime can reuse an existing PDI MID Server record and contexts, but the MID record must be `Up` and validated before starting the Web Server and Event Listener contexts. After a MID auto-upgrade, legacy encrypted `config.xml` parameters can fail to decrypt; for a disposable local runtime, stop MID, replace the affected value with plaintext in the ignored runtime config, and restart so the upgraded runtime re-encrypts it. Prefer a least-privileged MID service account; using an admin account is acceptable only for short-lived disposable PDI validation and must be recorded as such.

If a disposable MID service-account password must be rotated, prefer the same native path used by the ServiceNow Set Password modal. Directly patching `sys_user.user_password` through the Table API can return HTTP 200 without producing a usable Basic-auth password. On the 2026-05-29 PDI, a form-login session invoking `PasswordPolicyUtil.generatePassword` followed by `PasswordPolicyUtil.setPassword` produced a usable password for `otel_mid_server`; Basic REST auth, MID startup, MID role checks, MID Web Server ping, and Event Management read-back all succeeded after that rotation.

Prefer creating and copying the MID Web Server API key through the ServiceNow UI. The credential table generates a one-time key on insert and normally blocks later key updates. Avoid disabling those business rules except on disposable test instances where the change is immediately reverted.

For MID Basic auth, configure the MID Web Server extension with Authentication Type `Basic`, then start or restart it through the ServiceNow UI or another native `MIDExtensionContext` path. The Basic password is a `password2` field; ServiceNow re-encrypts it for MID extension payloads before the MID runtime can decrypt it. Hand-built ECC `MIDExtension` payloads that copy the table value can fail during web server startup.

```bash
SERVICENOW_SEND=1 \
SERVICENOW_MODE=mid \
SERVICENOW_INSTANCE_URL=https://mid-host.example.net:8443 \
SERVICENOW_MID_API_KEY= \
SERVICENOW_USERNAME=mid-basic-user \
SERVICENOW_PASSWORD=... \
make smoke-servicenow-jsonv2
```

The exporter does not need MID-specific Basic auth code. Use the Collector `basicauth` extension with `mode: mid` and the MID base URL.

For MID mTLS, the ServiceNow MID Web Server context needs `Secure Connection` enabled and Authentication Type `mTLS`. The MID runtime requires a server certificate/key from either the MID unified keystore (`Use MID Unified Keystore`) or a web server keystore, plus a web server truststore that trusts the client certificate presented by the Collector. Keystore and truststore passwords are also `password2` fields, so use the ServiceNow UI or native `MIDExtensionContext` start/restart path to generate encrypted MID extension payloads.

On the Collector side, no exporter-owned auth flow is needed. Configure the exporter HTTP client TLS settings with a client certificate and key, and the CA that validates the MID Web Server certificate:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://mid-host.example.net:8443
    mode: mid
    tls:
      ca_file: /path/to/mid-server-ca.pem
      cert_file: /path/to/collector-client.pem
      key_file: /path/to/collector-client-key.pem
```

For a disposable local mTLS validation, generate a test CA, server certificate, and client certificate; import the server keypair into the MID web server keystore or MID unified keystore; import the client CA into the MID web server truststore; set the MID Web Server context to `mTLS`; restart the MID Web Server and MID WebService Event Listener contexts; then run the same MID JSON v2 smoke and Collector rich smoke against an `https://` MID URL.

The PDI validation on 2026-05-15 used the MID unified keystore for the server keypair and the MID JVM `cacerts` truststore for the client certificate trust material. Importing only the disposable client CA exposed the CA in the TLS acceptable issuer list but still failed HTTP requests with a TLS `certificate_unknown` alert. Importing the disposable client leaf certificate into the same truststore and restarting the MID Web Server context resolved the handshake; the direct MID JSON v2 POST and Collector-backed OTLP smoke both inserted Event Management events. Treat this as PDI evidence, not a production certificate pattern: production should use the organization's normal CA, certificate lifecycle, and truststore management.

4. Build and run the local Collector distribution:

```bash
make build-collector
./otelcol-servicenow-event-management-dev/otelcol-servicenow-event-management-dev --config examples/servicenow-event-management-exporter.yaml
```

5. Send one OTLP log to the local Collector. Keep the attributes close to a real production event so ServiceNow event rules and binding behavior can be inspected:

```bash
curl --fail-with-body --show-error --silent \
  --request POST http://127.0.0.1:4318/v1/logs \
  --header 'Content-Type: application/json' \
  --data @- <<'JSON'
{
  "resourceLogs": [
    {
      "resource": {
        "attributes": [
          {"key": "service.name", "value": {"stringValue": "otel-servicenow-event-management-smoke"}},
          {"key": "host.name", "value": {"stringValue": "otel-devcontainer"}}
        ]
      },
      "scopeLogs": [
        {
          "logRecords": [
            {
              "timeUnixNano": "1777812000000000000",
              "severityNumber": 9,
              "severityText": "INFO",
              "body": {"stringValue": "OpenTelemetry ServiceNow Event Management exporter Collector smoke test"},
              "attributes": [
                {"key": "event.name", "value": {"stringValue": "collector_smoke_test"}},
                {"key": "servicenow.severity", "value": {"stringValue": "5"}}
              ]
            }
          ]
        }
      ]
    }
  ]
}
JSON
```

6. For broader field coverage, send a rich multi-batch OTLP smoke after the one-event path works:

```bash
OTEL_SMOKE_TOTAL_EVENTS=15 \
OTEL_SMOKE_BATCHES=4 \
OTEL_SMOKE_AUTH_LABEL=basicauth \
OTEL_SMOKE_SOURCE_PREFIX=otel-basic-field-coverage \
OTEL_SMOKE_SCENARIO=basic-field-coverage \
make smoke-otel-rich-batches
```

The rich smoke varies ServiceNow overrides for `source`, `event_class`, `node`, `resource`, `metric_name`, `type`, `message_key`, `severity`, `description`, and `resolution_state`. It also sends scalar, boolean, double, array, map, and bytes-valued OTel attributes so `additional_info` storage is exercised. The JSON v2 event field list does not include a separate inbound `state` field; use `resolution_state` for the supported New/Closing event state and include any UI-specific test state hint under `additional_info`.

For live throughput validation after the one-event path is healthy, run:

```bash
make build-collector

OTEL_SMOKE_TOTAL_EVENTS=500 \
OTEL_SMOKE_BATCHES=10 \
OTEL_SMOKE_BATCH_DELAY=0.05 \
OTEL_SMOKE_AUTH_LABEL=basicauth \
OTEL_SMOKE_SOURCE_PREFIX=throughput-basic \
OTEL_SMOKE_SCENARIO=throughput-basic \
make smoke-servicenow-throughput
```

The throughput wrapper starts the local Collector, sends the rich batches, and reads back `em_event` rows by run id to summarize accepted count, latency to read-back, and unique values across the main Event Management fields.

7. For the full Splunk fan-out scenario, run the opt-in HEC fan-out harness after the one-event and throughput paths are healthy:

```bash
SERVICENOW_HEC_FANOUT_DRY_RUN=1 make smoke-hec-fanout
```

The dry run generates the temporary Collector config and expected summary tables without credentials or containers. A live OAuth-backed run uses the Collector `oauth2client` extension for ServiceNow writes, a `splunk_hec` receiver for input, transform/filter processors for enrichment and `metric_value > 90` thresholding, and a `splunk_hec` exporter for all logs:

```bash
set -a
source .env.local
set +a

SERVICENOW_HEC_FANOUT_AUTH=oauth2client \
SERVICENOW_HEC_FANOUT_ACCEPT_SPLUNK_LICENSE=1 \
make smoke-hec-fanout
```

By default the live harness sends 50 HEC event-mode messages with `sourcetype=otel:servicenow-hec-fanout`; 10 exceed the threshold and should appear in ServiceNow Event Management, while all 50 should appear in Splunk. The harness prints paths to TSV summaries for sent messages, ServiceNow read-back, and Splunk read-back. If using an existing Splunk Enterprise instance instead of a temporary container, set `SERVICENOW_HEC_FANOUT_START_SPLUNK=0`, `SPLUNK_HEC_ENDPOINT`, `SPLUNK_HEC_TOKEN`, `SPLUNK_MANAGEMENT_URL`, and `SPLUNK_PASSWORD`.

8. Verify in ServiceNow that the event was accepted. The exporter must not write tables directly, but manual verification may use Event Management UI views or read-only inspection of `em_event` if the test account allows it.

## Evidence To Record

Update [docs/validation-evidence](validation-evidence/README.md) after each real-instance attempt, then update [docs/compatibility.md](compatibility.md) if the validation matrix changes:

```text
Date:
Instance type: PDI, partner dev, customer sub-prod, other
ServiceNow family:
Plugin state:
Endpoint mode: instance or mid
Endpoint API: jsonv2 or business_rules
Auth extension or smoke auth path:
Commands run:
HTTP status:
Response shape:
ServiceNow UI or em_event verification:
Field mapping adjustments needed:
Follow-up tests added:
```

Do not paste credentials, authorization headers, full payloads containing customer data, or unredacted `additional_info` values into validation evidence. Keep public evidence concise and update [docs/compatibility.md](compatibility.md) when a validation result changes a matrix row.

## Failure Handling

- `401` or `403`: verify role assignment, auth extension config, token scope, or MID API-key configuration before changing exporter code.
- `404`: verify the endpoint mode and whether Event Management is installed on the instance.
- `400`: record the response shape, add a failing test with the rejected payload behavior, then adjust mapping or validation.
- `429` or `5xx`: treat as transient and verify Collector retry behavior.
- PDI plugin missing: switch to a licensed development, partner, lab, or customer sub-production instance instead of weakening exporter behavior.
