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
- Request compression: exposed through Collector HTTP `compression`, but route-specific. A June 2026 PDI probe validated gzip for direct instance JSON v2 and found gzip unsupported by the direct `insertMultiple` compatibility path.
- Tests: unit mapping tests, config validation tests, HTTP client tests, and an OCB build path.

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
| Treat request-body compression as endpoint-specific. | [June 2026 compression validation](../validation-evidence/2026-06-compression-validation.md) shows direct instance JSON v2 accepted `Content-Encoding: gzip`, while `em_event.do?JSONv2&sysparm_action=insertMultiple` returned a JSON error and inserted no row when sent gzipped bytes. |
| Treat connector and listener transform endpoints as future/non-MVP modes. | [Integrate with push connectors](https://www.servicenow.com/docs/r/it-operations-management/event-management/configure-listener-transform-script.html) documents `/api/sn_em_connector/em/inbound_event?source=...`, and [Use legacy listener transform scripts](https://www.servicenow.com/docs/r/it-operations-management/event-management/migrate-transform-scripts.html) covers listener-transform behavior. |
| Map fields to Event Management event fields and use `resolution_state`, not an inbound `state`, for open/clear semantics. | [Event field format for event collection](https://www.servicenow.com/docs/r/it-operations-management/event-management/c_EMIntegrateRequirementEvent.html?contentId=MXxZKDzOzoP0FwsQ1IlNwA) documents `message_key`, severity, `resolution_state`, `time_of_event`, and `additional_info`. |
| Encode `additional_info` as a JSON string with string-valued keys. | The event field format page documents `additional_info` as a JSON string with string values; real PDI validation in the execution plan confirms object payloads can store poorly. |
| Treat `additional_info` as ServiceNow alert-enrichment input, not just opaque overflow text. | [Custom alert fields](https://www.servicenow.com/docs/r/it-operations-management/event-management/populate-custom-alert-fields.html) documents that matching Additional information keys can populate custom alert fields, and the field format page documents normalization when Additional information is not JSON key/value data. |
| Delegate OAuth client credentials to Collector auth extensions instead of exporter-owned OAuth code. | [OAuth Inbound](https://www.servicenow.com/docs/r/platform-security/authentication/oauth-inbound.html), [Client Credentials](https://www.servicenow.com/docs/r/xanadu/platform-security/authentication/client-credentials.html), and [Add the OAuth Application User](https://www.servicenow.com/docs/r/platform-security/authentication/add-oauth-application-user.html) describe ServiceNow-side OAuth setup. |
| Delegate mTLS/client certificate presentation to Collector HTTP TLS settings. | [Set up mutual authentication](https://www.servicenow.com/docs/r/platform-security/certificate-based-authentication/set-up-mutual-auth.html) describes ServiceNow-side certificate-based authentication. PDI validation blocked on ServiceNow CA publication and did not prove direct instance mTLS, while MID mTLS was validated separately. |
| Keep raw OTel metrics out of the MVP logs exporter. | [Metric collection from OpenTelemetry metrics](https://www.servicenow.com/docs/r/it-operations-management/event-management/metric-collection-otel.html) documents a separate MID metrics collection path with different transform semantics. |

## Endpoint Modes And API Flavors

`mode` selects the deployment route. `api` selects the ServiceNow ingestion flavor.

| Mode | API | URL shape | Intended use |
| --- | --- | --- | --- |
| `instance` | `jsonv2` | `https://<instance>.service-now.com/api/global/em/jsonv2` | Direct high-throughput ITOM Event Management ingestion. |
| `instance` | `business_rules` | `https://<instance>.service-now.com/em_event.do?JSONv2&sysparm_action=insertMultiple` | Direct compatibility path when customer Business Rules on `em_event` must run. Keep request bodies uncompressed unless the exact target path proves gzip support. |
| `mid` | `jsonv2` | `http(s)://<mid-host>:<port>/api/mid/em/jsonv2` | Customers that route events through MID WebService. Request-body compression needs target MID listener validation. |
| `mid` | `business_rules` | `http(s)://<mid-host>:<port>/api/mid/em/jsonv2` | Same exporter-facing MID listener; configure MID properties in [MID Business Rules compatibility](../mid-business-rules.md) for upstream Business Rules compatibility. Keep request bodies uncompressed unless the exact target path proves gzip support. |
| future | `connector` | `/api/sn_em_connector/em/inbound_event?source=<source>` | Future source-specific mode if needed. |
| not an exporter mode | `legacy_listener` | `/api/global/em/inbound_event` | Legacy listener transform script endpoint for custom/non-JSON-v2 payloads or upgraded integrations. |

Do not switch the generic exporter to `/api/now/em/inbound_event`. ServiceNow's current Event Management docs describe `/api/global/em/inbound_event` as the legacy listener-transform endpoint and `/api/sn_em_connector/em/inbound_event` as the push connector endpoint; `/api/now/em/inbound_event` is not documented as a current Event Management ingestion path. For this exporter, JSON v2 remains the primary target because we already produce ServiceNow Event Management event records and do not need instance-side listener transform scripts.

## Real Instance Expectations

The exporter is considered locally complete when unit tests, fake endpoint tests, OCB build, and dry-run smoke checks pass. It is considered ServiceNow-validated only after a real Event Management instance accepts at least one exporter-generated log event.

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
| `message_key` | explicit attribute, configured attribute list, or generated default | Controls ServiceNow de-duplication. Configured attribute lists are used only when every listed attribute is present and non-empty; otherwise the exporter falls back to the default field key and normally records missing names in `additional_info`. The diagnostic may be omitted if auxiliary metadata exhausts its byte budget. The `legacy` format remains the default. |
| `severity` | `servicenow.severity`, `log.severity_number`, or mapping table | Preserve original values in `additional_info` when space permits; on overflow the numeric OTel severity remains pinned when specified, while severity text is best-effort. |
| `description` | `servicenow.description`, `event.description`, log body, then OTel severity text | Each attribute lookup prefers a present log attribute over the same-name resource attribute. Truncate safely if ServiceNow rejects large values. |
| `additional_info` | selected attributes and original severity data | Encode as a JSON string with string values. Use the bounded metadata-reduction fallback if exporter metadata alone exhausts the byte budget. Do not create custom event table fields. |
| `time_of_event` | log timestamp, observed timestamp fallback, then exporter admission time | UTC format `yyyy-MM-dd HH:mm:ss`; the OTel event timestamp takes precedence over `ObservedTimestamp`. |
| `resolution_state` | `servicenow.resolution_state` or clear severity mapping | Values include `New` and `Closing`; default clear severity emits `Closing`. |

When both native timestamps are zero, the exporter samples the clock once at each `ConsumeLogs` admission, copies the batch, and fills only `ObservedTimestamp` on records missing both times. Untimed records in one call share that instant; a separate call samples a new instant. Queue delay, helper batching, and retries preserve the captured value. Timestamp preparation leaves caller-owned pdata unchanged, and all-timed or empty calls need neither a clock sample nor a copy. The mapper keeps its current-time fallback for standalone use and for recovered queue entries persisted before this behavior when both native timestamps are zero. Recovery bypasses `ConsumeLogs`, so each retry of such an older entry may select a new fallback time; a producer or caller resubmission through `ConsumeLogs` is a new admission. The added native timestamp is serialized with pdata, so byte-sized queue admission can change for untimed records near configured limits.

Native log trace IDs, span IDs, and instrumentation-scope name/version are not included automatically. Pipelines that need this optional context can copy nonzero IDs and nonempty scope fields into ordinary log attributes with the [guarded Transform overlay](../../examples/servicenow-event-management-log-context.yaml); those values then follow the normal `additional_info` filters and limits. The default ServiceNow condition identity and native context remain unchanged. Operators with existing `message_key.attributes` lists must audit them before enabling the recipe because a newly present context key can change a configured key from its previous default-field fallback; see [Native Log Context guidance](../configuration.md#native-log-context).

ServiceNow JSON v2 does not document a separate inbound `state` field for event creation. State-like exporter behavior should use `resolution_state`; test-only UI hints or workflow labels belong in `additional_info`.

Ordinary application logs and logs authored as events are both valid inputs. Optional producer labels such as `alert.rule.id`, `alert.state`, and `alert.originating_signal` have no built-in validation, lifecycle inference, or routing semantics; not every occurrence event represents a firing or resolved condition, and `alert.state=resolved` alone does not request a Clear event. The exporter-owned `otel.signal` describes the input signal as `logs`, including when an upstream connector converts metric datapoints into log records.

These illustrative JSON log-attribute maps (not an OTLP payload or Collector configuration) assume the default `servicenow.severity` attribute, default `legacy` message-key format and field limits (`field_limits.message_key=1024`, `field_limits.resolution_state=40`), default `severity.clear_resolution_state=Closing`, and no `servicenow.resolution_state` at log or resource scope:

```json
{
  "servicenow.message_key": "checkout-prod-host01-error-rate",
  "servicenow.severity": 3,
  "alert.rule.id": "checkout.error_rate.high",
  "alert.state": "firing",
  "alert.originating_signal": "metrics",
  "alert.evaluated_at": "2026-09-27T12:00:00Z"
}
```

```json
{
  "servicenow.message_key": "checkout-prod-host01-error-rate",
  "servicenow.severity": 0,
  "alert.rule.id": "checkout.error_rate.high",
  "alert.state": "resolved",
  "alert.originating_signal": "metrics",
  "alert.evaluated_at": "2026-09-27T12:01:00Z"
}
```

With those assumptions, the pair maps to the same message key and severities `3` then `0`; the first record omits `resolution_state`, and the second emits `Closing`. A valid selected `servicenow.resolution_state` wins even at severity `0`, including `New`; an invalid severity override falls back to OTel severity and then the configured default, while the configured clear state is used when no valid `servicenow.resolution_state` is selected for a final severity `0`. This describes the mapped payload, not the final ServiceNow alert state. Choose condition identity upstream and keep it stable; keep changing measurements, `alert.state`, timestamps, trace IDs, and smoke run IDs out of enduring condition identity. Changing a deployed key is an identity migration covered by [message-key guidance](../configuration.md#message-key).

Optional provenance, rule, evidence, and condition/evaluation-time attributes are ordinary log attributes that may be encoded as string-valued `additional_info`, subject to the existing [filters and budgets](../configuration.md#additional-info); they are not guaranteed to be retained. Native event, observed, or admission timestamps map occurrence time; arbitrary evaluation-time attributes do not change `time_of_event` or create pending periods or evaluation windows. See the [timestamp semantics](#field-mapping) and [native log context guidance](../configuration.md#native-log-context).

The source or evaluator owns rule identity, predicate and duration, recovery and missing-data behavior, and transition emission; the exporter maps and delivers authored logs; ServiceNow performs downstream event and alert processing. Collector batching, retry, and queue settings do not evaluate condition state. See [trusted-control precedence](../production-hardening.md#data-safety), [ordering limits](#error-handling), and the [maintained breach-only metrics example](../../examples/servicenow-event-management-metrics-event-oauth.yaml).

## Mapping Diagnostic Telemetry

The exporter emits `otelcol_exporter_servicenow_event_management_mapping_diagnostics`, an integer counter of affected log-record occurrences per mapping attempt. Its only attributes are the exporter component ID (`exporter`) and one of six fixed `reason` values: `invalid_severity_override`, `configured_message_key_fallback`, `event_field_truncated`, `additional_info_value_truncated`, `additional_info_attribute_dropped`, and `additional_info_metadata_reduced`. Each reason increments at most once per record; different reasons may overlap. Counts come from mapping decisions rather than by reading `additional_info`, so a payload-budget omission cannot erase an observation. Retries count the same records again on each attempt. Reached observations survive a later record's mapping error, while queued data is counted only when it reaches mapping. Filtering, redaction, and collision-only operations do not create reasons. These observations do not describe delivery loss or rejected-row totals and do not change payloads, error precedence, or queue/retry behavior.

## Message Key Formats

`message_key.format` supports `legacy` and `sha256_v1`. Omitted or empty format means `legacy`, and the factory default explicitly selects it. Unknown values fail configuration validation. `message_key.separator` applies only to `legacy`; keep its existing validation and accept non-default separators when `sha256_v1` is selected.

`legacy` preserves the existing behavior: a non-empty explicit `servicenow.message_key` wins; otherwise, a complete configured attribute list is joined in its configured order; otherwise, the non-empty values of `source`, `node`, `type`, `resource`, and `metric_name` are joined. Configured attributes still use all-or-fallback selection, and missing names are diagnostic metadata when they fit. Legacy keys continue to use the configured byte limit, UTF-8-safe truncation, and best-effort truncation metadata when the auxiliary metadata budget is exhausted.

In `sha256_v1`, a non-empty explicit `servicenow.message_key` keeps the existing log-over-resource lookup and OTel string conversion. The value is preserved exactly when its byte length fits `field_limits.message_key`; an oversized value produces a sanitized mapping error before field limiting and HTTP transport. This mode does not truncate, escape, prefix, or hash explicit keys.

Generated `sha256_v1` identity material is selected as follows:

- When every configured `message_key.attributes` value is present and non-empty, use the ordered attribute-name/value pairs. Attribute names and order are part of identity.
- Otherwise, including when no configured attribute list exists, use the five full mapped pairs named `source`, `node`, `type`, `resource`, and `metric_name`, in that order. Retain empty values and keep the existing whole-list fallback and missing-attribute diagnostics.

The exact version-one byte contract is:

1. `frame(s)` is the unsigned 64-bit big-endian byte length of `s`, followed by the raw string bytes of `s`.
2. Stream these bytes into SHA-256: `frame("servicenow_event_management.message_key.sha256_v1")`, then `frame("attributes")` or `frame("default_fields")`, then the unsigned 64-bit big-endian pair count, then `frame(name) + frame(value)` for each ordered pair.
3. Emit `otel-sha256-v1:` followed by the 64 lowercase hexadecimal SHA-256 digits, for 79 ASCII bytes total.

The versioned representation hashes complete selected values before event-field truncation and excludes severity, time, body, and other unselected fields. The branch label prevents configured and default-field material from sharing an encoding namespace. The separator is ignored. This is an exporter-defined stable encoding, not ServiceNow's native message-key algorithm or a mathematical guarantee against SHA-256 collisions. Require `field_limits.message_key >= 79` in this mode; retain the 1,024-byte default and allow raised budgets after target-instance validation, with no exporter-side upper cap.

One oversized explicit key rejects its entire mapped Collector callback batch as a permanent error, so valid records in that same batch are not sent or retried. With queueing enabled, exporter-helper processing surfaces the error, so it may not be returned to the original `ConsumeLogs` caller. Opting in changes generated event identities. Operators should close or drain alerts under old keys before cutover, or keep an explicitly managed old-key route for their update/clear lifecycle. Queued and replayed records are part of that migration. Changing configured attribute names or order, or entering or leaving configured-attribute fallback, can change identity in either format. Replicas and firing/clear events must use the same format, attribute names and order, values, and byte-budget policy. Retain producer-supplied explicit keys when identity must interoperate across pipelines; the exporter does not dual-send or migrate existing alerts.

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

- Accept a configured severity attribute (default `servicenow.severity`) when its value is an integer, a finite exactly integral double, or a string containing a base-10 integer after surrounding whitespace is trimmed, and the resulting code is `0` through `5`.
- Treat fractional and non-finite doubles, out-of-range values, unparseable strings, and unsupported types as invalid overrides. Fall back to the OTel severity mapping and then the configured default. A present log attribute retains precedence over the resource attribute even when the selected log value is invalid.
- Map OTel log severities conservatively, with configurable `fatal`, `error`, `warn`, and `info` output values because ServiceNow references conflict on severity labels.
- Preserve the original OTel severity text and number in `additional_info` when the byte budget allows. If metadata reduction is needed, keep the numeric OTel severity when specified; original severity text is best-effort.
- Treat unknown severities as the configured default.
- Set `resolution_state` to the configured clear state, default `Closing`, when the final ServiceNow severity is `0` and no valid explicit `servicenow.resolution_state` is selected.

Fractional doubles were previously truncated before range validation (for example, `4.9` became `4`). Fractional overrides are now rejected and use the existing mapping/default fallback, which can still intentionally emit Clear. Producers that depended on truncation should normalize values upstream or send the intended whole code as an integer, integer string, or integral double.

## Additional Information Behavior

The exporter owns only the Event Management event `additional_info` field. ServiceNow can copy, normalize, or use that data later while processing alerts and event rules. CI binding remains ServiceNow-side behavior based on fields such as `node`, CMDB content, and event rules; it is not created by putting an object into `additional_info`.

Public JSON v2 examples show `additional_info` as an object, but ServiceNow event-entry documentation describes it as a JSON string and notes that string values are the supported key/value shape. Real PDI testing also showed object payloads being stored as `[object Object]`, while a JSON string payload was stored correctly. The exporter therefore encodes additional data as a JSON string whose top-level values are strings. Strings stay unchanged, ints, doubles, and bools use stable scalar formatting, map and slice values become compact JSON strings under their original key, and bytes encode as base64. Values that cannot be rendered as this documented string shape are dropped and normally counted in `otel.servicenow.additional_info.dropped_by_value`; the count is best-effort and may be omitted if it does not fit after metadata reduction.

ServiceNow's custom alert field behavior makes `additional_info` a structured enrichment channel. If an alert has a custom field and the processed event contains an Additional information key with the same technical field name, ServiceNow can copy that value into the alert. Deployments that depend on this should create or transform OTel attributes with the exact ServiceNow alert field name, such as a customer `user_*`, `u_*`, or application-scoped field name, and make sure `additional_info.include_attributes`, `additional_info.max_attributes`, value limits, and the total `field_limits.additional_info` budget leave room for those keys. Event Rules can also add or rename Additional information fields on the ServiceNow side.

Do not rely on ServiceNow's normalization of plain text Additional information for exporter output. ServiceNow can normalize non-key/value content when processing events, but that tends to become generic JSON content rather than actionable named fields. The exporter should keep emitting named JSON key/value data so event rules, alert-field promotion, reporting, and post-alert processing have stable keys.

The exporter must bound Event Management fields before sending them. Default field limits should match conservative documented ServiceNow limits and live schema evidence where available: source 200, event class 100, node 100, resource 100, metric name 1024, type 100, message key 1024, description 4000, additional info 4000, and resolution state 40. Real-instance testing showed JSON v2 can accept overlong values and then silently truncate small string fields, while large string fields can store beyond their dictionary max length because of platform type mapping. The exporter should truncate before transport and normally record truncated field names in `additional_info`; this diagnostic can be omitted when auxiliary metadata exhausts the byte budget.

`additional_info` must also support include, exclude, and redact attribute patterns. Default redaction should cover common credential-bearing names such as authorization, token, password, secret, API key, cookie, session, credential, and client secret. Production deployments that may carry sensitive application data should prefer an allowlist through `additional_info.include_attributes`.

The `additional_info` byte budget applies to the encoded JSON text, including keys and JSON escaping, before that text is escaped as a string in the outer request JSON. Successful packing preserves its existing entries and order-independent JSON content, apart from the exact reserved producer key `otel.servicenow.additional_info.metadata_reduced`, whose value is never copied. If a producer value under that key survives normal filters, rendering, redaction, and the attribute-count limit, it counts once as a dropped optional entry. No configuration change is needed for other producers; producers that rely on this exact key for enrichment must rename it to retain the value. If required exporter metadata still exceeds the budget after optional attributes are shed, send the record with a bounded envelope: keep `otel.signal`, the original numeric OTel severity when specified, `otel.servicenow.additional_info.metadata_reduced="true"`, and the exporter-computed `otel.servicenow.additional_info.dropped_attributes` count when nonzero. The marker says some exporter metadata was omitted; it does not enumerate omitted fields or count rejected events. Then consider whole metadata entries, in order, for original severity text, configured-fallback strategy, missing configured identity names, truncated event-field names, dropped-by-count, redacted-attribute, dropped-by-value, and truncated-value counts. Keep an entry only if its actual JSON-encoded form fits; continue to later entries after a skip. At the supported minimum budget of 256 bytes, the pinned envelope remains within 192 bytes even with an `int32` severity and nonnegative `int64` optional-drop count.

This fallback changes prior behavior for callbacks rejected only because exporter metadata exceeded the `additional_info` budget: mappable records now proceed to normal transport. Original severity text and diagnostics become best-effort only on this overflow path. The event fields, identity selection, severity mapping, and explicitly oversized `sha256_v1` key rejection behavior do not change. Consumers should treat the marker as evidence that missing metadata cannot be interpreted as proof the underlying condition did not occur.

## Error Handling

- Retry transient network errors, 429, and 5xx responses. While verifying a successful 2xx response, retry recognized read interruptions (`io.ErrUnexpectedEOF`, context cancellation/deadline, or a `net.Error`) only when the bounded read has not exceeded its verification limit. The exporter returns a sanitized mode/API/status summary and lets Collector `exporterhelper` apply the configured retry policy. Unknown read errors remain permanent.
- Treat 400-class validation errors, except 429, as permanent unless implementation evidence shows otherwise.
- Inspect bounded non-empty 2xx JSONv2 response bodies. Treat record-level `__status: failure`, top-level `_status` or `status` failure, and top-level JSONv2 error objects as permanent exporter errors. JSON `null` values in recognized error fields are treated like absent fields.
- Treat non-empty non-JSON or malformed JSON 2xx response bodies, complete invalid gzip header/checksum errors, and oversized bodies that cannot be verified as permanent, sanitized errors because the exporter cannot verify ServiceNow accepted the records. Empty 2xx responses remain valid for MID JSON v2.
- Redact auth headers and configured secret fields in all errors.
- Include endpoint mode, API flavor, status code, and concise failure reason in logs without arbitrary response bodies.

A retry after an interrupted 2xx acknowledgment can replay the whole mapped Collector callback because ServiceNow may have processed the POST before the response read failed. This can create duplicate `em_event` rows or repeat Event table Business Rule side effects. A `message_key` supports alert correlation; it does not provide exactly-once insertion or make the POST idempotent. Retries do not selectively resend records from a partially observed response.

`io.ErrUnexpectedEOF` is a delivery-policy category, not proof of a socket fault or transience; it can also describe a truncated compressed representation. With `sending_queue` disabled, caller cancellation or deadline stops the active send and prevents further helper retries. Default asynchronous queueing detaches queued work from the original caller cancellation/deadline; a per-attempt HTTP timeout can retry while the active send context remains live.

With `sending_queue` enabled in its default asynchronous mode (`wait_for_result: false`), a successful `ConsumeLogs` call means the request was enqueued; it does not establish that ServiceNow accepted it. Collector `exporterhelper` owns retry scheduling and termination. Keep a finite `retry_on_failure.max_elapsed_time` appropriate to event usefulness (the default is five minutes); setting it to `0s` removes the elapsed-time cutoff. This cutoff is checked after a failed attempt when deciding whether to schedule another retry and does not interrupt an in-flight response read, so keep the HTTP `timeout` finite as well (30 seconds by default). Disabling retries makes one attempt and prevents retry after every retryable error class.

Separate requests that share a `message_key` have no exporter per-key or end-to-end ordering guarantee. For example, a firing request may receive a retryable `503` and wait while a later Clear request with the same key progresses; the firing request may then be retried. `message_key` supports correlation, and preserved `time_of_event` records when an event occurred; neither sequences delivery or ServiceNow processing. HTTP acceptance alone does not establish the final alert state. Verify the state after events have been processed, including relevant delayed transitions, using the [clear-event check](../load-testing.md#what-to-measure).

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
- OCB can build a local Collector distribution including the exporter.
- A dry-run smoke script can show the exact ServiceNow JSON v2 payload without sending secrets.
