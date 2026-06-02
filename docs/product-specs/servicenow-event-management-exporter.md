# ServiceNow Event Management Exporter Spec

## Goal

Build an OpenTelemetry Collector exporter named `servicenow_event_management` that sends telemetry-derived events to ServiceNow ITOM Event Management.

The exporter should make ServiceNow alert ingestion work through normal Collector pipelines while keeping mapping, authentication, retry behavior, and testability explicit.

## MVP Scope

- Primary signal: logs to ServiceNow Event Management events.
- Primary endpoint: ServiceNow instance JSON v2, `POST /api/global/em/jsonv2`.
- Secondary endpoint mode: MID WebService JSON v2, `POST /api/mid/em/jsonv2`.
- Optional compatibility API: direct instance `POST /em_event.do?JSONv2&sysparm_action=insertMultiple` for deployments that intentionally need Event table Business Rules to run. For MID deployments, this is a MID Server forwarding configuration, not a different client-facing exporter URL.
- Auth: Collector HTTP auth extensions and HTTP client settings. Basic uses `basicauth`, static bearer uses `bearertokenauth`, OAuth2 uses `oauth2client`, MID API key should use `headers_setter` or Collector HTTP `headers`, and mTLS/client certificates use Collector HTTP `tls` settings after the ServiceNow endpoint is configured to request and trust client certificates.
- Transport: JSON over HTTP with Collector retry and queue settings.
- Tests: unit mapping tests, config validation tests, HTTP client tests, and a Collector Builder path.

## Non-Goals For MVP

- Direct writes to ServiceNow tables.
- Alert thresholding from arbitrary metrics.
- Trace-to-event conversion.
- Source-specific connector endpoints such as `source=prometheus`, unless added as a later endpoint mode.
- ServiceNow OTel metrics collection through MID `/api/mid/sa/inbound_metrics`; that path is documented as adjacent, not as this exporter.

## ServiceNow Decision Sources

ServiceNow documentation is intentionally linked here for vendor-shaped behavior. Keep [docs/references/source-map.md](../references/source-map.md) as the detailed bibliography and last-reviewed record.

| Exporter choice | ServiceNow source |
| --- | --- |
| Use instance JSON v2 as the primary Event Management API instead of Table API writes. | [Pushing events to the instance using web service API](https://www.servicenow.com/docs/r/it-operations-management/event-management/send-events-via-web-service.html) documents `/api/global/em/jsonv2`, the `records` payload shape, `evt_mgmt_integration`, and `additional_info` for extra data. |
| Add Business Rules compatibility as an opt-in API flavor, not as the default. | The same ServiceNow page states that `/api/global/em/jsonv2` does not invoke Business Rules on `em_event`, and documents `em_event.do?JSONv2&sysparm_action=insertMultiple` when Business Rules should be activated, with lower performance than JSON v2. |
| Support MID JSON v2 as a deployment mode. | [Pushing events to the MID Server using web service API](https://www.servicenow.com/docs/r/it-operations-management/event-management/event-collection-via-MID-using-push.html) documents `/api/mid/em/jsonv2` and notes it uses the same JSON v2 format as instance ingestion. |
| Treat connector and listener transform endpoints as future/non-MVP modes. | [Integrate with push connectors](https://www.servicenow.com/docs/r/it-operations-management/event-management/configure-listener-transform-script.html) documents `/api/sn_em_connector/em/inbound_event?source=...`, and [Use legacy listener transform scripts](https://www.servicenow.com/docs/r/it-operations-management/event-management/migrate-transform-scripts.html) covers listener-transform behavior. |
| Map fields to Event Management event fields and use `resolution_state`, not an inbound `state`, for open/clear semantics. | [Event field format for event collection](https://www.servicenow.com/docs/r/it-operations-management/event-management/c_EMIntegrateRequirementEvent.html?contentId=MXxZKDzOzoP0FwsQ1IlNwA) documents `message_key`, severity, `resolution_state`, `time_of_event`, and `additional_info`. |
| Encode `additional_info` as a JSON string with string-valued keys. | The event field format page documents `additional_info` as a JSON string with string values; real PDI validation in [docs/validation-evidence](../validation-evidence/README.md) confirms object payloads can store poorly. |
| Treat `additional_info` as ServiceNow alert-enrichment input, not just opaque overflow text. | [Custom alert fields](https://www.servicenow.com/docs/r/it-operations-management/event-management/populate-custom-alert-fields.html) documents that matching Additional information keys can populate custom alert fields, and the field format page documents normalization when Additional information is not JSON key/value data. |
| Delegate OAuth client credentials to Collector auth extensions instead of exporter-owned OAuth code. | [OAuth Inbound](https://www.servicenow.com/docs/r/platform-security/authentication/oauth-inbound.html), [Client Credentials](https://www.servicenow.com/docs/r/xanadu/platform-security/authentication/client-credentials.html), and [Add the OAuth Application User](https://www.servicenow.com/docs/r/platform-security/authentication/add-oauth-application-user.html) describe ServiceNow-side OAuth setup. |
| Delegate mTLS/client certificate presentation to Collector HTTP TLS settings. | [Set up mutual authentication](https://www.servicenow.com/docs/r/platform-security/certificate-based-authentication/set-up-mutual-auth.html) describes ServiceNow-side certificate-based authentication. PDI validation blocked on ServiceNow CA publication and did not prove direct instance mTLS, while MID mTLS was validated separately. |
| Keep raw OTel metrics out of the MVP logs exporter. | [Metric collection from OpenTelemetry metrics](https://www.servicenow.com/docs/r/it-operations-management/event-management/metric-collection-otel.html) documents a separate MID metrics collection path with different transform semantics. |

## Endpoint Modes And API Flavors

`mode` selects the deployment route. `api` selects the ServiceNow ingestion flavor.

| Mode | API | URL shape | Intended use |
| --- | --- | --- | --- |
| `instance` | `jsonv2` | `https://<instance>.service-now.com/api/global/em/jsonv2` | Direct high-throughput ITOM Event Management ingestion. |
| `instance` | `business_rules` | `https://<instance>.service-now.com/em_event.do?JSONv2&sysparm_action=insertMultiple` | Direct compatibility path when customer Business Rules on `em_event` must run. |
| `mid` | `jsonv2` | `http(s)://<mid-host>:<port>/api/mid/em/jsonv2` | Customers that route events through MID WebService. |
| `mid` | `business_rules` | `http(s)://<mid-host>:<port>/api/mid/em/jsonv2` | Same exporter-facing MID listener; configure MID properties in [MID Business Rules compatibility](../mid-business-rules.md) for upstream Business Rules compatibility. |
| future | `connector` | `/api/sn_em_connector/em/inbound_event?source=<source>` | Future source-specific mode if needed. |
| not an exporter mode | `legacy_listener` | `/api/global/em/inbound_event` | Legacy listener transform script endpoint for custom/non-JSON-v2 payloads or upgraded integrations. |

Do not switch the generic exporter to `/api/now/em/inbound_event`. ServiceNow's current Event Management docs describe `/api/global/em/inbound_event` as the legacy listener-transform endpoint and `/api/sn_em_connector/em/inbound_event` as the push connector endpoint; `/api/now/em/inbound_event` is not documented as a current Event Management ingestion path. For this exporter, JSON v2 remains the primary target because we already produce ServiceNow Event Management event records and do not need instance-side listener transform scripts.

## Real Instance Expectations

The exporter is considered locally complete when unit tests, fake endpoint tests, Collector Builder output, and example config validation pass. It is considered ServiceNow-validated only after a real Event Management instance accepts at least one exporter-generated log event.

Use `docs/servicenow-real-instance-testing.md` for the validation ladder. A PDI is acceptable only when Event Management is visible or active on that PDI. Public ServiceNow docs confirm that PDIs can activate many plugins, but also state that some plugins are unavailable; therefore PDI support for Event Management must be verified per instance.

## Collector Config Shape

Keep runnable configuration in [docs/configuration.md](../configuration.md) and the checked-in [examples](../../examples). The product-level shape should stay focused on the behavior contract:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    mode: instance
    api: jsonv2
    auth:
      authenticator: basicauth/servicenow
    source: opentelemetry
    type: otel-log
    message_key:
      attributes: [service.name, host.name, event.name]
      separator: "|"
    severity:
      from_attribute: servicenow.severity
      default: 5
      clear_resolution_state: Closing
    additional_info:
      include_attributes: [deployment.environment, service.*, cloud.*, k8s.*, alert.*, ci.*]
    timeout: 30s
    sending_queue:
      enabled: true
      queue_size: 1000
      batch:
        sizer: items
        min_size: 100
        max_size: 100
        flush_timeout: 200ms
    retry_on_failure:
      enabled: true

extensions:
  basicauth/servicenow:
    client_auth:
      username: ${env:SERVICENOW_USERNAME}
      password: ${env:SERVICENOW_PASSWORD}
```

These concepts should remain visible: endpoint mode, API flavor, Collector auth extension, mapping rules, timeout, retry, and queueing. Keep default values and full examples in the configuration reference so operational docs do not drift from runnable configs.

Auth should not be implemented as ServiceNow-specific request signing code in the exporter. The exporter embeds Collector `confighttp.ClientConfig`, so users can select supported auth extensions:

```yaml
extensions:
  bearertokenauth/servicenow:
    token: ${env:SERVICENOW_BEARER_TOKEN}

exporters:
  servicenow_event_management:
    auth:
      authenticator: bearertokenauth/servicenow
```

```yaml
extensions:
  oauth2client/servicenow:
    client_id: ${env:SERVICENOW_CLIENT_ID}
    client_secret: ${env:SERVICENOW_CLIENT_SECRET}
    token_url: https://example.service-now.com/oauth_token.do

exporters:
  servicenow_event_management:
    auth:
      authenticator: oauth2client/servicenow
```

```yaml
extensions:
  headers_setter/servicenow_mid:
    headers:
      - key: Authorization
        value: key ${env:SERVICENOW_MID_API_KEY}
        action: upsert

exporters:
  servicenow_event_management:
    mode: mid
    auth:
      authenticator: headers_setter/servicenow_mid
```

Client certificate settings are also standard Collector HTTP client configuration:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    mode: instance
    tls:
      cert_file: /path/to/client-cert.pem
      key_file: /path/to/client-key.pem
```

For direct ServiceNow instance mTLS, those TLS settings are only an auth path after ServiceNow certificate-based authentication is configured and the instance TLS endpoint requests client certificates.

## ServiceNow Payload

ServiceNow JSON v2 expects:

```json
{
  "records": [
    {
      "source": "opentelemetry",
      "event_class": "checkout-service",
      "node": "host-01",
      "resource": "checkout",
      "metric_name": "request.error_rate",
      "type": "application",
      "message_key": "checkout-service:host-01:error_rate",
      "severity": "2",
      "description": "High error rate",
      "additional_info": "{\"otel.signal\":\"logs\",\"otel.severity_number\":\"17\"}",
      "time_of_event": "2026-05-03 18:34:21"
    }
  ]
}
```

## Field Mapping

This is default log-to-event mapping. It is deterministic fallback logic, not automatic schema discovery or ServiceNow-side transform behavior. Production pipelines should prefer explicit `servicenow.*` attributes when the event source knows the right ServiceNow values.

| ServiceNow field | Source in OTel logs | Notes |
| --- | --- | --- |
| `source` | exporter config default, override by log/resource attribute | Default `opentelemetry`. |
| `event_class` | `service.name`, `event.domain`, or config template | Should be stable for rules and grouping. |
| `node` | `host.name`, `host.id`, `service.instance.id`, `net.host.name` | Used for CI binding when present in CMDB. |
| `resource` | `service.name`, `service.namespace`, or configured attribute | Device, app, or service associated with event. |
| `metric_name` | `event.name`, metric-like attribute, or log name | Optional but useful for dedup defaults. |
| `type` | configured value or attribute | General grouping. |
| `message_key` | configured attribute list or generated default | Controls ServiceNow de-duplication. Configured attribute lists are used only when every listed attribute is present; otherwise the exporter falls back to the default field key and records missing parts in `additional_info`. |
| `severity` | `servicenow.severity`, `log.severity_number`, or mapping table | Preserve original values in `additional_info`. |
| `description` | log body, `event.description`, or summary attribute | Truncate safely if ServiceNow rejects large values. |
| `additional_info` | selected attributes and original severity data | Encode as a JSON string with string values. Do not create custom event table fields. |
| `time_of_event` | log timestamp, observed timestamp fallback | UTC format `yyyy-MM-dd HH:mm:ss`. |
| `resolution_state` | `servicenow.resolution_state` or clear severity mapping | Values include `New` and `Closing`; default clear severity emits `Closing`. |

ServiceNow JSON v2 does not document a separate inbound `state` field for event creation. State-like exporter behavior should use `resolution_state`; test-only UI hints or workflow labels belong in `additional_info`.

## Metrics And `metric_name`

`metric_name` is a ServiceNow Event Management event field. It names the measurement or condition that caused the event and participates in default message-key construction and alert correlation. Its presence in JSON v2 does not imply that the exporter should accept raw OTel metrics.

Metrics-to-events requires a separate design because raw datapoints do not inherently say when to open, update, or close an Event Management alert. A future metrics signal could be considered only with explicit semantics for thresholding or alert-state input, clear-event emission, label-to-`additional_info` behavior, stable message-key construction, and interaction with ServiceNow's separate OTel metrics/MID ingestion path.

## Batching Semantics

ServiceNow JSON v2 accepts one or more events in a `records` array. The exporter maps each incoming Collector logs request to one JSON v2 POST and includes one ServiceNow record for each OTel log record in that request.

Batch sizing should remain Collector-native:

- The `batch` processor groups and can cap log records before the exporter.
- `exporterhelper` handles retry buffering through `sending_queue`. Queue batching is disabled by default, but when `sending_queue.batch` is enabled it can merge or split exporter requests.
- Any example that enables `sending_queue.batch` must set a finite `batch.max_size` so queue batching cannot merge requests beyond the intended ServiceNow JSON v2 payload cap.
- The exporter should not maintain a separate custom batching loop unless ServiceNow real-instance validation proves the Collector helpers cannot express the needed behavior.

Use a conservative starting cap, such as 100 log records per request, for both `send_batch_max_size` and `sending_queue.batch.max_size` until real ServiceNow testing establishes safe throughput and payload-size limits for the target instance. Public JSON v2 docs show multi-event payloads but do not document a maximum `records` count.

## Severity Behavior

ServiceNow Event Management JSON v2 documents severity values from `1` critical through `5`, with `0` for clear. Some source-specific connector docs label `5` as info while JSON v2 text labels it OK. The exporter should:

- Accept explicit `servicenow.severity` values `0` through `5`.
- Map OTel log severities conservatively, with configurable `fatal`, `error`, `warn`, and `info` output values because ServiceNow references conflict on severity labels.
- Preserve the original OTel severity text and number in `additional_info`.
- Treat unknown severities as the configured default.
- Set `resolution_state` to the configured clear state, default `Closing`, when the final ServiceNow severity is `0` and no explicit `servicenow.resolution_state` is present.

## Additional Information Behavior

The exporter owns only the Event Management event `additional_info` field. ServiceNow can copy, normalize, or use that data later while processing alerts and event rules. CI binding remains ServiceNow-side behavior based on fields such as `node`, CMDB content, and event rules; it is not created by putting an object into `additional_info`.

Public JSON v2 examples show `additional_info` as an object, but ServiceNow event-entry documentation describes it as a JSON string and notes that string values are the supported key/value shape. Real PDI testing also showed object payloads being stored as `[object Object]`, while a JSON string payload was stored correctly. The exporter therefore encodes additional data as a JSON string whose top-level values are strings. Strings stay unchanged, ints, doubles, and bools use stable scalar formatting, map and slice values become compact JSON strings under their original key, and bytes encode as base64. Values that cannot be rendered as this documented string shape are dropped and counted in `otel.servicenow.additional_info.dropped_by_value`.

ServiceNow's custom alert field behavior makes `additional_info` a structured enrichment channel. If an alert has a custom field and the processed event contains an Additional information key with the same technical field name, ServiceNow can copy that value into the alert. Deployments that depend on this should create or transform OTel attributes with the exact ServiceNow alert field name, such as a customer `user_*`, `u_*`, or application-scoped field name, and make sure `additional_info.include_attributes`, `additional_info.max_attributes`, per-value limits, and the total `field_limits.additional_info` budget leave room for those keys. The default per-value limit should match the default total `additional_info` field limit, while the default attribute-count limit should stay aligned with OpenTelemetry's 128-attribute default. Event Rules can also add or rename Additional information fields on the ServiceNow side.

Do not rely on ServiceNow's normalization of plain text Additional information for exporter output. ServiceNow can normalize non-key/value content when processing events, but that tends to become generic JSON content rather than actionable named fields. The exporter should keep emitting named JSON key/value data so event rules, alert-field promotion, reporting, and post-alert processing have stable keys.

The exporter must bound Event Management fields before sending them. Default field limits should match conservative documented ServiceNow limits and live schema evidence where available: source 200, event class 100, node 100, resource 100, metric name 1024, type 100, message key 1024, description 4000, additional info 4000, and resolution state 40. Real-instance testing showed JSON v2 can accept overlong values and then silently truncate small string fields, while large string fields can store beyond their dictionary max length because of platform type mapping. The exporter should still truncate before transport and record truncated field names in `additional_info` so operators see the loss explicitly.

`additional_info` must also support include, exclude, and redact attribute patterns. Default redaction should cover common credential-bearing names such as authorization, token, password, secret, API key, cookie, session, credential, and client secret. Production deployments that may carry sensitive application data should prefer an allowlist through `additional_info.include_attributes`.

## Error Handling

- Retry transient network errors, 429, and 5xx responses.
- Treat 400-class validation errors, except 429, as permanent unless implementation evidence shows otherwise.
- Inspect bounded non-empty 2xx JSONv2 response bodies. Treat record-level `__status: failure`, top-level `_status` or `status` failure, and top-level JSONv2 error objects as permanent exporter errors.
- Treat non-empty non-JSON or malformed JSON 2xx response bodies as permanent, sanitized errors because the exporter cannot verify ServiceNow accepted the records. Empty 2xx responses remain valid for MID JSON v2.
- Redact auth headers and configured secret fields in all errors.
- Include endpoint mode, API flavor, status code, and concise failure reason in logs without arbitrary response bodies.

## Security

- Credentials come from environment variables or Collector secret providers.
- Do not print complete payloads by default.
- Support TLS validation by default; any insecure TLS option must be explicit and documented.
- Prefer least-privileged ServiceNow users with `evt_mgmt_integration`.

## Acceptance Criteria

- A Collector pipeline can configure `servicenow_event_management` as a logs exporter.
- A log record maps to a valid JSON v2 `records` payload.
- Unit tests cover required field defaults, severity mapping, `message_key`, timestamps, and `additional_info`.
- HTTP tests cover auth headers, retryable/permanent status classes, and response parsing.
- Collector Builder can build a local Collector distribution including the exporter.
- Local tests prove the exact ServiceNow JSON v2 payload shape without sending secrets to ServiceNow.
