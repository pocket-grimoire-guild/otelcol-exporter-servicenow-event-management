# Configuration Reference

This page documents the `servicenow_event_management` exporter configuration. The exporter embeds Collector `confighttp.ClientConfig`, so standard HTTP client options such as `endpoint`, `auth`, `headers`, `tls`, `proxy_url`, `read_buffer_size`, `write_buffer_size`, and `timeout` are available.

For ServiceNow-specific behavior behind these options, see [ServiceNow Decision Sources](product-specs/servicenow-event-management-exporter.md#servicenow-decision-sources). The canonical source list is [docs/references/source-map.md](references/source-map.md).

## Required Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `endpoint` | string | none | ServiceNow instance base URL, full JSON v2 URL, MID base URL, or proxy base URL. |

## Exporter Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `mode` | `instance` or `mid` | `instance` | Selects `/api/global/em/jsonv2` or `/api/mid/em/jsonv2`. |
| `api` | `jsonv2` or `business_rules` | `jsonv2` | Selects the ServiceNow ingestion API flavor. `business_rules` uses `insertMultiple` for direct instance mode and records MID forwarding intent for MID mode. |
| `source` | string | `opentelemetry` | Default ServiceNow `source` when `servicenow.source` is absent. |
| `event_class` | string | empty | Default ServiceNow `event_class` before fallback to `service.name`, `event.domain`, then `source`. |
| `type` | string | `otel-log` | Default ServiceNow `type` when `servicenow.type` is absent. |

If `endpoint` already ends in the correct path for the selected `mode` and `api`, it is used as-is. If it has no path, the exporter appends the expected path. If it already names a different known Event Management path than the selected combination, startup fails. Query parameters are not supported on base, proxy, or JSON v2 endpoints because the exporter owns the ServiceNow route. The only accepted endpoint query is the direct instance Business Rules compatibility query on `/em_event.do`.

Startup validation also enforces that `endpoint` is an `http` or `https` URL with a host. Known Event Management paths that conflict with the selected `mode` and `api` are rejected. Unknown paths are treated as proxy base paths and have the selected Event Management route appended, so do not point this exporter at Table API, connector, or listener-transform URLs. Use Collector `auth`, `headers`, `tls`, or `proxy_url` settings for behavior that might otherwise be encoded as endpoint query parameters.

Endpoint selection:

| `mode` | `api` | Exporter request URL | Notes |
| --- | --- | --- | --- |
| `instance` | `jsonv2` | `/api/global/em/jsonv2` | Default high-throughput ServiceNow Event Management API. |
| `instance` | `business_rules` | `/em_event.do?JSONv2&sysparm_action=insertMultiple` | Compatibility path for customers that need Event table Business Rules invoked. |
| `mid` | `jsonv2` | `/api/mid/em/jsonv2` | Default MID WebService JSON v2 listener. |
| `mid` | `business_rules` | `/api/mid/em/jsonv2` | The exporter still targets MID; configure the MID Server upstream endpoint property for `insertMultiple`. |

ServiceNow documents direct instance ingestion at [`/api/global/em/jsonv2`](https://www.servicenow.com/docs/r/it-operations-management/event-management/send-events-via-web-service.html), the Business Rules-compatible `em_event.do?JSONv2&sysparm_action=insertMultiple` URL on the same page, and MID WebService ingestion at [`/api/mid/em/jsonv2`](https://www.servicenow.com/docs/r/it-operations-management/event-management/event-collection-via-MID-using-push.html). Connector and listener-transform URLs are separate ServiceNow integration modes and are not accepted by this exporter.

For `mode: mid` and `api: business_rules`, configure the MID Server properties documented by ServiceNow, including `mid.probe.event.endpoint.url=em_event.do?JSONv2%26sysparm_action=insertMultiple`. That setting controls where MID forwards events after the exporter posts to the local MID JSON v2 listener. See [MID Business Rules compatibility](mid-business-rules.md) for the full validation checklist.

## Authentication

Use Collector HTTP client auth features instead of exporter-owned credential flows. Basic, bearer token, OAuth2 client credentials, MID API keys, custom headers, TLS settings, and client certificates should be configured through `auth`, `headers`, and `tls`.

The ServiceNow instance JSON v2 documentation lists `evt_mgmt_integration` as the required role. Whichever direct-instance ServiceNow identity your auth path resolves to must have that role for Event Management ingestion: the Basic-auth user, the bearer-token user, the OAuth Application User used by client credentials, or the user mapped from a client certificate. MID Web Server auth is a separate client-to-MID concern; follow the MID Server setup docs for the MID-to-instance account and roles.

For direct ServiceNow mTLS, first complete ServiceNow certificate-based authentication setup on the instance. The exporter can present a client certificate with `tls.cert_file` and `tls.key_file`, but those settings only work as authentication after the ServiceNow endpoint requests and trusts client certificates.

## Message Key

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `message_key.attributes` | list of strings | empty | Attribute names used to build `message_key` when every listed attribute is present and non-empty. |
| `message_key.separator` | string | `|` | Separator used when joining configured attributes or default identity fields. |

Explicit `servicenow.message_key` always wins. If `message_key.attributes` is set but any listed attribute is missing, the exporter falls back to the default identity key: `source`, `node`, `type`, `resource`, and `metric_name`. Missing configured attributes are recorded in `additional_info`. `message_key.separator` cannot be only whitespace; leave it unset to use the default separator.

ServiceNow documents `message_key` as the de-duplication identifier and describes the empty-key default as Source, Node, Type, Resource, and Metric Name on the [Event Management event field format page](https://www.servicenow.com/docs/r/it-operations-management/event-management/c_EMIntegrateRequirementEvent.html?contentId=MXxZKDzOzoP0FwsQ1IlNwA).

## Severity

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `severity.from_attribute` | string | `servicenow.severity` | Attribute used for explicit ServiceNow severity. |
| `severity.default` | integer `0`-`5` | `5` | Fallback when neither explicit ServiceNow severity nor OTel severity mapping applies. |
| `severity.clear_resolution_state` | string | `Closing` | Resolution state to send when final severity is `0` and no explicit `servicenow.resolution_state` is set. Use `""` to omit. |
| `severity.mapping.fatal` | integer `0`-`5` | `1` | ServiceNow severity for OTel fatal logs. |
| `severity.mapping.error` | integer `0`-`5` | `2` | ServiceNow severity for OTel error logs. |
| `severity.mapping.warn` | integer `0`-`5` | `4` | ServiceNow severity for OTel warn logs. |
| `severity.mapping.info` | integer `0`-`5` | `5` | ServiceNow severity for OTel info logs. |

All configured ServiceNow severity outputs must be integers from `0` through `5`. `severity.clear_resolution_state` may be empty, `New`, or `Closing`; use an empty string when clear-severity records should omit `resolution_state`. Incoming `servicenow.resolution_state` values are normalized to `New` or `Closing` with surrounding whitespace and case differences ignored. Any other incoming value is omitted before transport; when severity is `0`, the exporter falls back to `severity.clear_resolution_state`.

## Field Limits

| Option | Default |
| --- | --- |
| `field_limits.source` | `200` |
| `field_limits.event_class` | `100` |
| `field_limits.node` | `100` |
| `field_limits.resource` | `100` |
| `field_limits.metric_name` | `1024` |
| `field_limits.type` | `100` |
| `field_limits.message_key` | `1024` |
| `field_limits.description` | `4000` |
| `field_limits.additional_info` | `4000` |
| `field_limits.resolution_state` | `40` |

The exporter truncates fields to these byte limits before transport and records truncated field names under `otel.servicenow.truncated_fields` in `additional_info`. This is intentional even when ServiceNow JSON v2 accepts a longer value, because some fields are silently truncated during storage and the JSON v2 response does not report that loss.

The default limits reconcile ServiceNow documentation with live PDI evidence. `source` uses `200` because the ServiceNow Event API, the PDI `em_event.source` dictionary entry, and JSON v2 read-back agree on 200, even though some Event Management field-format pages still list 100. `node` stays at `100` because the field-format docs, dictionary metadata, and live read-back agree. `metric_name` uses the observed dictionary value `1024` as the portable cap; a live PDI stored 1201 characters, but ServiceNow platform dictionary docs explain that string max length values above 254 can map to a larger physical text type, so this is not a portable API contract.

All field limits are configurable for deployments that have verified a different local schema or ServiceNow family behavior. For example, an instance that has validated longer `metric_name` storage can set `field_limits.metric_name` above `1024`. Raising limits means the exporter will send and trust those longer values; validate the target instance with read-back before relying on that behavior for identity fields.

All field limits must be positive. `field_limits.additional_info` must be at least `256` bytes so required exporter metadata has room to fit after truncation.

## Additional Info

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `additional_info.include_attributes` | glob list | empty | Optional allowlist. When non-empty, only matching attributes are copied. |
| `additional_info.exclude_attributes` | glob list | empty | Attribute names to omit. |
| `additional_info.redact_attributes` | glob list | common secret patterns | Attribute names whose values become `[REDACTED]`. |
| `additional_info.max_attributes` | integer | `128` | Maximum number of non-control attributes copied. Use `0` for no count limit. |
| `additional_info.max_value_length` | integer | `4000` | Maximum bytes per copied value. The final JSON string must still fit `field_limits.additional_info`. |

All `additional_info` values are string-valued. Attributes beginning with `servicenow.` are exporter control attributes and are not copied.

ServiceNow's [Event Management field format](https://www.servicenow.com/docs/r/it-operations-management/event-management/c_EMIntegrateRequirementEvent.html?contentId=MXxZKDzOzoP0FwsQ1IlNwA) describes `additional_info` as a JSON string and says JSON values should be strings. That is why this exporter stringifies non-string OTel values rather than sending a nested object. Strings stay unchanged, scalar numbers and bools use stable string formatting, maps and slices become compact JSON strings, and bytes encode as base64. Values that cannot be rendered safely are dropped and counted under `otel.servicenow.additional_info.dropped_by_value`.

The default value length matches the default total `additional_info` field budget. ServiceNow's [Event API](https://www.servicenow.com/docs/r/api-reference/server-api-reference/EventAPI.html) lists `additional_info` as a predefined Event field with a 4000-character maximum, so this exporter does not treat it as unbounded. Raise `field_limits.additional_info` and validate read-back on the target instance before relying on larger payloads.

The default attribute count remains `128` because it matches OpenTelemetry's [default attribute count limit](https://opentelemetry.io/docs/specs/otel/common/#attribute-limits) and because the final ServiceNow JSON string is still capped separately. Increasing only the count rarely preserves more useful data unless values are very small; prefer `additional_info.include_attributes` for important alert-enrichment keys.

ServiceNow can use Additional information keys to populate custom alert fields when a key name matches the alert field's technical name. To use that behavior, make the upstream OTel attribute name match the target alert field name, or use an upstream transform processor to copy or rename the value before this exporter runs. For example, include fields such as `user_service_owner`, `u_service_owner`, or an application-scoped custom field name only when those are the actual field names in the target ServiceNow instance.

When `additional_info.include_attributes` is non-empty, it must include those custom alert field names or they will not be sent. If a custom alert field is operationally important, tune `additional_info.max_attributes`, `additional_info.max_value_length`, and `field_limits.additional_info` so the key is not dropped before ServiceNow processes the event.

Attribute pattern options use Go `path.Match` glob syntax. Empty patterns and malformed patterns fail startup validation. `additional_info.max_attributes` must be non-negative; `0` disables the count limit. `additional_info.max_value_length` must be positive.

Default redaction patterns:

```yaml
- "*authorization*"
- "*api_key*"
- "*apikey*"
- "*client_secret*"
- "*cookie*"
- "*credential*"
- "*password*"
- "*secret*"
- "*session*"
- "*token*"
```

## Queue, Retry, And Timeout

The exporter uses Collector helper settings:

```yaml
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
  initial_interval: 5s
  max_interval: 30s
```

Use the Collector `batch` processor to cap log records before the exporter. The exporter default config uses Collector's default sending queue, but queue batching is disabled unless `sending_queue.batch` is configured. Examples that enable `sending_queue.batch` set `max_size: 100` so exporterhelper cannot merge requests beyond the intended ServiceNow JSON v2 payload cap. ServiceNow public JSON v2 docs show multi-record payloads but do not document a maximum `records` count, so tune both `send_batch_max_size` and `sending_queue.batch.max_size` with a real target instance. Queue and retry settings are validated by Collector helper config. See [Production Hardening](production-hardening.md#batching-and-backpressure) for the operational sizing guidance.

## Override Attributes

| ServiceNow field | Override attribute |
| --- | --- |
| `source` | `servicenow.source` |
| `event_class` | `servicenow.event_class` |
| `node` | `servicenow.node` |
| `resource` | `servicenow.resource` |
| `metric_name` | `servicenow.metric_name` |
| `type` | `servicenow.type` |
| `message_key` | `servicenow.message_key` |
| `severity` | configured `severity.from_attribute`, default `servicenow.severity` |
| `description` | `servicenow.description` |
| `resolution_state` | `servicenow.resolution_state` |
