# ServiceNow Event Management Exporter

The `servicenow_event_management` exporter converts OpenTelemetry logs into ServiceNow ITOM Event Management records and posts them to either the instance endpoint or a MID WebService endpoint.

Status: standalone development component. This exporter is not currently part of the OpenTelemetry Collector Contrib distribution; see [contrib readiness](../../docs/contrib-readiness.md) for the future donation checklist.

Only logs are supported. Metrics and traces are intentionally unsupported until a later plan defines clear event semantics.

## Mapping Diagnostics

The exporter emits `otelcol_exporter_servicenow_event_management_mapping_diagnostics`, an integer counter of affected log-record occurrences per mapping attempt. Each reason increments at most once per record, even if several fields or attributes trigger it. A record can have multiple distinct reasons. The only metric attributes are the fixed `reason` and exporter component ID `exporter`; producer keys and values never become labels.

The six reasons are:

- `invalid_severity_override`: the selected log or resource ServiceNow severity override is invalid.
- `configured_message_key_fallback`: a configured message-key attribute list is incomplete, so the exporter uses its default identity fields.
- `event_field_truncated`: at least one ServiceNow event field was shortened to fit its configured limit.
- `additional_info_value_truncated`: at least one rendered, non-redacted `additional_info` value was shortened by `max_value_length`.
- `additional_info_attribute_dropped`: an `additional_info` attribute was dropped because rendering failed, the attribute-count limit was reached, or byte-budget fitting removed it.
- `additional_info_metadata_reduced`: required exporter metadata did not fit the configured `additional_info` budget, so the bounded metadata-reduction path was used.

Filtering, redaction, and key collisions alone do not add a reason. A producer-supplied diagnostic-looking value cannot create or suppress a reason. The counter is computed from mapping decisions, independently of whether optional diagnostic metadata fits in the event payload. Observations reached before a later record fails mapping are retained; no mapping means no observation, so queue acceptance by itself does not increment it. A retry maps again and counts that attempt again, including when a later HTTP result fails. The counter is not a rejected-record total or delivery-loss rate, and multiple reasons may describe the same record.

## ServiceNow References

The exporter behavior is tied to ServiceNow Event Management docs:

- Instance JSON v2 ingestion: [`/api/global/em/jsonv2`](https://www.servicenow.com/docs/r/it-operations-management/event-management/send-events-via-web-service.html).
- Business Rules-compatible instance ingestion: [`/em_event.do?JSONv2&sysparm_action=insertMultiple`](https://www.servicenow.com/docs/r/it-operations-management/event-management/send-events-via-web-service.html).
- MID JSON v2 ingestion: [`/api/mid/em/jsonv2`](https://www.servicenow.com/docs/r/it-operations-management/event-management/event-collection-via-MID-using-push.html).
- Event field semantics: [Event field format for event collection](https://www.servicenow.com/docs/r/it-operations-management/event-management/c_EMIntegrateRequirementEvent.html?contentId=MXxZKDzOzoP0FwsQ1IlNwA).
- ServiceNow OAuth client credentials setup: [Client Credentials](https://www.servicenow.com/docs/r/xanadu/platform-security/authentication/client-credentials.html) and [Add the OAuth Application User](https://www.servicenow.com/docs/r/platform-security/authentication/add-oauth-application-user.html).
- ServiceNow direct mTLS setup: [Set up mutual authentication](https://www.servicenow.com/docs/r/platform-security/certificate-based-authentication/set-up-mutual-auth.html).

See the repository [source map](../../docs/references/source-map.md) for last-reviewed dates and decision notes.

## Configuration

The checked-in examples are the canonical runnable configurations. This section shows the exporter shape and leaves repeated defaults in the example files and the full [configuration reference](../../docs/configuration.md).

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
      redact_attributes:
        - "*authorization*"
        - "*password*"
        - "*secret*"
        - "*token*"
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

service:
  extensions: [basicauth/servicenow]
```

Full example configurations:

| File | Purpose |
| --- | --- |
| [examples/servicenow-event-management-exporter.yaml](../../examples/servicenow-event-management-exporter.yaml) | Direct instance JSON v2 with Basic auth. |
| [examples/servicenow-event-management-exporter-business-rules.yaml](../../examples/servicenow-event-management-exporter-business-rules.yaml) | Direct instance Business Rules compatibility with Basic auth. |
| [examples/servicenow-event-management-exporter-bearer.yaml](../../examples/servicenow-event-management-exporter-bearer.yaml) | Direct instance JSON v2 with static bearer token auth. |
| [examples/servicenow-event-management-exporter-oauth.yaml](../../examples/servicenow-event-management-exporter-oauth.yaml) | Direct instance JSON v2 with OAuth2 client credentials. |
| [examples/servicenow-event-management-exporter-mid.yaml](../../examples/servicenow-event-management-exporter-mid.yaml) | MID Web Server JSON v2 with MID API key auth. |

`mode` selects the deployment route. `mode: instance` posts directly to the ServiceNow instance, and `mode: mid` posts to the MID WebService listener.

`api` selects the ServiceNow ingestion flavor. `api: jsonv2` is the default and uses the high-throughput JSON v2 API. `api: business_rules` is a compatibility option for customers that intentionally need event table Business Rules to run.

| `mode` | `api` | Exporter request URL |
| --- | --- | --- |
| `instance` | `jsonv2` | `/api/global/em/jsonv2` |
| `instance` | `business_rules` | `/em_event.do?JSONv2&sysparm_action=insertMultiple` |
| `mid` | `jsonv2` | `/api/mid/em/jsonv2` |
| `mid` | `business_rules` | `/api/mid/em/jsonv2` |

For `mode: mid` with `api: business_rules`, the client-facing URL remains the MID JSON v2 listener. ServiceNow documents that the MID Server must be configured with `mid.probe.event.endpoint.url=em_event.do?JSONv2%26sysparm_action=insertMultiple` plus the related MID properties so the MID Server forwards to the Business Rules-compatible instance endpoint. See [MID Business Rules compatibility](../../docs/mid-business-rules.md) for the validation checklist and PDI evidence.

The current PDI validated direct `api: business_rules` with Basic auth and MID `api: business_rules` forwarding with a local Linux MID runtime configured for the upstream `insertMultiple` endpoint.

Endpoint query parameters are rejected except for the direct instance Business Rules compatibility query on `/em_event.do`. Use Collector `auth`, `headers`, `tls`, or `proxy_url` settings instead of encoding auth, routing, or proxy behavior in endpoint query strings.

HTTP request-body compression is available through Collector `confighttp` with `compression: gzip`, but support is endpoint-specific. A June 2026 PDI probe validated gzip for direct instance JSON v2 at `/api/global/em/jsonv2`; the direct Business Rules compatibility path `em_event.do?JSONv2&sysparm_action=insertMultiple` returned a JSON error and inserted no row when sent gzipped bytes. Keep Business Rules compatibility and MID forwarding paths uncompressed unless the exact target endpoint has been validated with read-back. See the [configuration reference](../../docs/configuration.md#http-request-compression) and [compression validation evidence](../../docs/validation-evidence/2026-06-compression-validation.md).

## Authentication

Authentication is delegated to Collector HTTP client settings and auth extensions. The exporter embeds `confighttp.ClientConfig`, following released exporters such as OTLP/HTTP, Prometheus Remote Write, Zipkin, Elasticsearch, OpenSearch, SignalFx, Azure Monitor, and Splunk HEC.

Basic auth uses `basicauth`:

```yaml
extensions:
  basicauth/servicenow:
    client_auth:
      username: ${env:SERVICENOW_USERNAME}
      password: ${env:SERVICENOW_PASSWORD}

exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    auth:
      authenticator: basicauth/servicenow
```

Static bearer tokens use `bearertokenauth`:

```yaml
extensions:
  bearertokenauth/servicenow:
    token: ${env:SERVICENOW_BEARER_TOKEN}

exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    auth:
      authenticator: bearertokenauth/servicenow
```

OAuth2 client credentials use `oauth2client`, which obtains and refreshes tokens:

```yaml
extensions:
  oauth2client/servicenow:
    client_id: ${env:SERVICENOW_CLIENT_ID}
    client_secret: ${env:SERVICENOW_CLIENT_SECRET}
    token_url: https://example.service-now.com/oauth_token.do

exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    auth:
      authenticator: oauth2client/servicenow
```

ServiceNow inbound OAuth client credentials must also be enabled on the instance. In ServiceNow, verify the client credentials system property, associate an OAuth Application User with the OAuth client, and make sure that user has the Event Management ingestion role and any required REST API auth scope. Token endpoint failures such as `access_denied` are usually ServiceNow OAuth application setup issues, not exporter request-signing behavior.

If the OAuth client is securely scoped, attach an auth scope to the OAuth entity. Collector `oauth2client` supports explicit scopes:

```yaml
extensions:
  oauth2client/servicenow:
    scopes: [useraccount]
```

MID API keys can use `headers_setter` as a client auth extension:

```yaml
extensions:
  headers_setter/servicenow_mid:
    headers:
      - key: Authorization
        value: key ${env:SERVICENOW_MID_API_KEY}
        action: upsert

exporters:
  servicenow_event_management:
    endpoint: http://mid.example.local:8080
    mode: mid
    auth:
      authenticator: headers_setter/servicenow_mid
```

For deployments that do not need an extension for a fixed header, Collector HTTP `headers` is also available through `confighttp.ClientConfig`.

Client-certificate authentication uses Collector HTTP TLS settings:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    mode: instance
    tls:
      cert_file: /path/to/client-cert.pem
      key_file: /path/to/client-key.pem
```

For direct ServiceNow instance mTLS, ServiceNow certificate-based authentication must first be configured so the instance trusts the client CA and maps the client certificate to an integration user. The PDI used by this harness did not validate direct instance mTLS because CA publication failed on the ServiceNow side and the endpoint did not request client certificates. MID mTLS has been validated separately through a local MID runtime and still uses the same Collector TLS client settings.

## Default Log-To-Event Mapping

The exporter uses deterministic default mapping. Each OTel log record becomes one ServiceNow Event Management event record. There is no schema discovery, machine inference, or ServiceNow-side transform in the exporter path.

Attributes prefixed with `servicenow.` are explicit overrides and should be preferred for production event quality:

| ServiceNow field | Override attribute | Default fallback |
| --- | --- | --- |
| `source` | `servicenow.source` | exporter `source`, then `opentelemetry` |
| `event_class` | `servicenow.event_class` | exporter `event_class`, `service.name`, `event.domain`, then `source` |
| `node` | `servicenow.node` | `host.name`, `host.id`, `service.instance.id`, `net.host.name` |
| `resource` | `servicenow.resource` | `service.name`, then `service.namespace` |
| `metric_name` | `servicenow.metric_name` | `event.name`, then OTel log event name |
| `type` | `servicenow.type` | exporter `type` |
| `message_key` | `servicenow.message_key` | configured `message_key.attributes` when all are present, then `source`, `node`, `type`, `resource`, and `metric_name` |
| `severity` | configured severity attribute, default `servicenow.severity` | mapped OTel log severity number, then configured default |
| `description` | `servicenow.description` | `servicenow.description`, then `event.description`, log body, and OTel severity text |
| `time_of_event` | OTel timestamp, `ObservedTimestamp`, then exporter admission time | UTC format `yyyy-MM-dd HH:mm:ss`; OTel timestamp takes precedence. |
| `resolution_state` | `servicenow.resolution_state` | `Closing` when severity is `0`, otherwise omitted |

When both OTel timestamps are zero, the exporter samples the clock once per `ConsumeLogs` call and fills only `ObservedTimestamp` on a private copy of the batch. Untimed records in the call share that instant, and helper queue delay, batching, and retries preserve it. A later call is a new admission. All-timed or empty calls pass through without a clock sample or copy, and caller-owned pdata is never changed by timestamp preparation. The mapper keeps its current-time fallback for standalone mapping and older persisted queue records with both native timestamps zero. Recovery of those entries bypasses `ConsumeLogs`, so retry attempts may select a new fallback; resubmission through `ConsumeLogs` is a new admission. The added field is part of serialized pdata and can change byte-sized queue admission for untimed records near a configured limit.

Extra resource and log attributes are copied to `additional_info`, excluding `servicenow.*` control attributes. The exact key `otel.servicenow.additional_info.metadata_reduced` is reserved for the exporter. When a producer value under that key passes the configured filters and rendering, it is counted once as a dropped optional attribute through the normal accounting and is never copied as the marker. No configuration change is needed for other producers; producers that rely on this exact key for enrichment must rename it to retain that value. On ordinary successful packing, original OTel severity text and number are preserved under `otel.severity_text` and `otel.severity_number`.

Native log trace/span IDs and instrumentation-scope name/version are not copied automatically. For an opt-in upstream Transform recipe, pipeline merge behavior, and the identity and retention caveats, see [Native Log Context in the configuration reference](../../docs/configuration.md#native-log-context).

`additional_info` is encoded as a JSON string whose values are strings. ServiceNow JSON v2 examples sometimes show this field as an object, but ServiceNow event forms document the field as a JSON string and real-instance testing showed object payloads storing as `[object Object]`. The exporter therefore sends a string such as `"{\"otel.signal\":\"logs\",\"test.count\":\"7\"}"`.

ServiceNow can use Additional information keys to populate custom alert fields when the key name matches the alert field name. For example, if the target instance has a custom alert field named `u_service_owner`, the upstream OTel record should carry an attribute named `u_service_owner`, or a transform processor or ServiceNow Event Rule should create that key before alert processing. Include those names in `additional_info.include_attributes` when using an allowlist, and tune the additional-info limits so important custom alert fields are not dropped.

When `message_key.attributes` is configured, every listed attribute must be present and non-empty before the exporter uses the configured key. If any configured part is missing, the exporter falls back as a whole to the ServiceNow default-field identity and normally records the missing attribute names under `otel.servicenow.message_key.missing_attributes` in `additional_info`. If auxiliary metadata itself exhausts the byte budget, this diagnostic is best-effort and may be omitted; the selected event identity does not change. This prevents partial configured keys; the legacy joined format can still collide when values contain its separator or when a generated key is truncated.

The default `message_key.format` is `legacy`, which keeps existing generated identities and truncation behavior. To opt into the versioned framed hash for generated identities, configure a stable attribute list and retain the 1,024-byte default budget:

```yaml
message_key:
  format: sha256_v1
  attributes: [service.name, host.name, event.name]
field_limits:
  message_key: 1024
```

`sha256_v1` hashes ordered attribute-name/value pairs when all configured values are present. Otherwise it hashes the five full mapped default-field pairs (`source`, `node`, `type`, `resource`, `metric_name`), retaining their empty positions. The format uses the existing string conversion and log-over-resource lookup. It hashes full values before event-field limits and emits `otel-sha256-v1:` followed by 64 lowercase hexadecimal digits. Its 79-byte generated keys are not truncated. `message_key.separator` is ignored by this format.

An explicit non-empty `servicenow.message_key` still wins and is preserved byte-for-byte when it fits the configured message-key budget. An explicit key over that budget fails mapping with a permanent sanitized error; the exporter does not hash or truncate it in this mode. One oversized key rejects the whole mapped callback batch before HTTP, so otherwise valid records in that batch are not delivered. With queueing enabled, exporter-helper processing surfaces the mapping error, so it may not be returned by the original `ConsumeLogs` call.

Opting in changes generated event identities. Close or drain alerts under their old keys before cutover, or manage an old-key route for their update and clear lifecycle. Include queued and replayed records in that migration. Changing configured attribute names or order, or entering or leaving configured-attribute fallback, can change identity in either format. Use the same format, configured attribute order and names, values, and byte-budget policy across replicas and for firing and clear records. Keep explicit upstream keys when pipelines need to share identity. See the [product specification](../../docs/product-specs/servicenow-event-management-exporter.md#message-key-formats) for the exact framing and the [configuration reference](../../docs/configuration.md#message-key) for the option and migration details.

## Field Limits And `additional_info`

ServiceNow Event Management fields have finite lengths. The exporter applies conservative defaults for documented fields and normally records truncated field names under `otel.servicenow.truncated_fields` in `additional_info`. When the metadata byte budget is exhausted, this diagnostic is best-effort.

Default field limits are chosen to prevent silent ServiceNow-side data loss: `source` 200, `event_class` 100, `node` 100, `resource` 100, `metric_name` 1024, `type` 100, `message_key` 1024, `description` 4000, `additional_info` 4000, and `resolution_state` 40. Live JSON v2 validation showed overlong `source` and `node` values can return HTTP 200 while storage truncates them, so the exporter truncates first and normally records the affected field names when the metadata budget allows; on the reduced-metadata path this diagnostic follows priority and may be omitted. In `legacy`, message-key truncation only reports that the field was shortened; it cannot preserve the original identity. In `sha256_v1`, generated keys remain 79 bytes and explicit keys that exceed the configured budget fail instead of being truncated. The v1 message-key budget must be at least 79 bytes and can be raised after target-instance validation.

These limits are per-exporter settings under `field_limits`. Operators with verified local schema behavior can raise or lower individual limits, such as allowing a longer `metric_name`; validate with ServiceNow read-back before depending on raised identity-field limits.

`additional_info` is bounded separately because it can absorb many OTel attributes. Its byte limit applies to the encoded `additional_info` JSON text, including keys and JSON escaping, before that value is escaped again inside the outer ServiceNow request JSON. By default the exporter includes up to 128 non-`servicenow.*` attributes, lets individual values use the configured 4000-byte field budget, redacts common secret-bearing attribute names, and keeps the encoded JSON text within `field_limits.additional_info`. Strings stay unchanged, scalar numbers and bools use stable string formatting, maps and slices become compact JSON strings, and bytes encode as base64. If entries are dropped to fit, exceed the attribute count, or cannot be rendered safely, the exporter tracks counts under `otel.servicenow.additional_info.dropped_attributes`, `otel.servicenow.additional_info.dropped_by_count`, or `otel.servicenow.additional_info.dropped_by_value`. The dropped-by-count and dropped-by-value diagnostics are best-effort on the reduced-metadata path.

When required exporter metadata still exceeds the budget after optional attributes are shed, the exporter sends the event with a compact metadata envelope. It pins `otel.signal`, the original numeric severity when specified, and `otel.servicenow.additional_info.metadata_reduced="true"`; it also keeps the exporter-computed optional-drop count when nonzero. Remaining whole metadata entries are added by priority if their actual JSON-encoded size fits: original severity text, configured-fallback strategy, missing identity names, truncated field names, then the dropped-by-count, redacted, dropped-by-value, and truncated-value counters. The marker means some exporter metadata was omitted; it does not list omissions or count dropped events. This marker is absent on ordinary successful packing, including normal optional-attribute shedding. The pinned envelope fits within 192 bytes even with the lowest signed 32-bit severity and the largest nonnegative 64-bit drop count, so every currently accepted budget of at least 256 bytes can carry a mappable event. Successful packing retains its existing metadata output except for producer values using the reserved marker key.

Use `additional_info.include_attributes` as a production allowlist when sending telemetry from sources that may contain personal data, payloads, headers, or application secrets. Use `additional_info.exclude_attributes` for known noisy or unsafe keys, and extend `additional_info.redact_attributes` for local naming conventions.

These filters apply only to top-level resource and log attributes copied into `additional_info`; they do not sanitize the body, mapped event fields, trusted ServiceNow controls, or exporter metadata. Redacting a matching parent replaces its whole copied value, including any nested map or slice content. See [Additional Info configuration](../../docs/configuration.md#additional-info) for glob and nested-value behavior, and [Production Hardening](../../docs/production-hardening.md#data-safety) for the event-input trust boundary.

For reliable ServiceNow deduplication, CI binding, alert correlation, and worker distribution, prefer setting at least `servicenow.node`, `servicenow.event_class`, `servicenow.metric_name`, `servicenow.message_key`, and `servicenow.severity` before the exporter.

## Severity And Clear Events

An explicit value from the configured severity attribute (default `servicenow.severity`) is accepted when it is an integer, a finite double that is exactly integral, or a string containing a base-10 integer after surrounding whitespace is trimmed. The resulting code must be from `0` through `5`. Fractional or non-finite doubles, out-of-range values, unparseable strings, and unsupported types are invalid overrides. An invalid selected override falls through to the OTel log severity mapping and then the configured default. A present log attribute still takes precedence over the resource attribute, even when the selected log value is invalid. Without a valid explicit ServiceNow severity, OTel log severity numbers are mapped through `severity.mapping`. The defaults are conservative for the JSON v2 behavior validated by this harness, but the map is configurable because ServiceNow severity labels are inconsistent across public Event Management references.

Fractional doubles were previously truncated before range validation, so a value such as `4.9` could be sent as severity `4`. This correction rejects fractional values. Producers that relied on truncation should normalize upstream or emit the intended whole code as an integer, integer string, or integral double. Invalid overrides still use the existing mapping/default policy, which can intentionally produce Clear.

When the final ServiceNow severity is `0` and `servicenow.resolution_state` is not already set, the exporter sends `resolution_state: Closing` by default. Set `severity.clear_resolution_state: ""` to omit this behavior, or set `servicenow.resolution_state` on individual records when the source already owns clear/open semantics. Incoming resolution state values are normalized to `New` or `Closing`; any other value is omitted before transport, with the clear-severity default still applied when severity is `0`.

## Metrics And `metric_name`

ServiceNow Event Management uses `metric_name` as an event field, for example `Used Memory` or `Total CPU utilization`. In this exporter, `metric_name` identifies what the event is about; it does not mean the exporter accepts raw OTel metrics.

Raw metrics require alert semantics before they become Event Management events. A future metrics design would need to define when a datapoint is actionable, how clear events are emitted, how the message key is kept stable, and which labels belong in `additional_info`. Until that design exists, metrics should be converted to event logs upstream or sent through ServiceNow's separate OTel metrics/MID path when that is the actual goal.

## Batching

The ServiceNow JSON v2 endpoint accepts a `records` array with one or more events. The exporter sends one JSON v2 request for each Collector logs request it receives, with one ServiceNow record per OTel log record.

Batch sizing is handled with Collector idioms:

- Use the Collector `batch` processor to group and cap log records before the exporter.
- Use exporter `sending_queue` for retry buffering. Queue batching is disabled by default; if `sending_queue.batch` is enabled, set `batch.max_size` so it cannot merge requests beyond the intended ServiceNow JSON v2 payload cap.
- Retryable `429` and `503` responses with `Retry-After` are passed to Collector retry handling as throttle delays. Accepted numeric seconds beyond `time.Duration`'s representable range saturate at its maximum value; Collector retry budgets still govern whether another attempt can occur.
- Non-empty 2xx response bodies are inspected for ServiceNow JSONv2 failure metadata. Record-level `__status: failure`, top-level failure status, top-level error objects, malformed JSON, and non-JSON 2xx bodies become permanent exporter errors with sanitized summaries. JSON `null` values in recognized error fields are treated like absent fields.
- Do not add a custom exporter-owned batching loop unless ServiceNow validation shows a behavior that Collector helpers cannot express.

The example config caps both the Collector batch processor and exporter queue batcher at 100 log records as a conservative starting point. Public JSON v2 docs show multi-record payloads but do not state a maximum `records` length. Real-instance validation should tune this value with `make smoke-servicenow-throughput` and record any ServiceNow response-size or throughput limits in `docs/validation-evidence` and the compatibility matrix.
