# Configuration Reference

This page documents the `servicenow_event_management` exporter configuration. The exporter embeds Collector `confighttp.ClientConfig`, so standard HTTP client options such as `endpoint`, `auth`, `headers`, `tls`, `proxy_url`, `compression`, `compression_params`, `read_buffer_size`, `write_buffer_size`, and `timeout` are available.

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

## HTTP Request Compression

The exporter does not enable HTTP request-body compression by default. It exposes Collector HTTP client compression through `confighttp.ClientConfig`, so deployments can opt in:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    mode: instance
    api: jsonv2
    compression: gzip
```

Use compression only for endpoint paths that have been validated with the target ServiceNow environment:

| `mode` | `api` | `compression: gzip` guidance |
| --- | --- | --- |
| `instance` | `jsonv2` | Supported in the June 2026 PDI probe: `/api/global/em/jsonv2` accepted `Content-Encoding: gzip` and the event read back from `em_event`. Validate on a customer-like instance before relying on this as a production default. |
| `instance` | `business_rules` | Do not use gzip. The tested PDI's `em_event.do?JSONv2&sysparm_action=insertMultiple` path ignored `Content-Encoding`; gzipped bytes returned HTTP 200 with `Request JSON object for insert cannot be null.` and inserted no row, while plain JSON succeeded. |
| `mid` | `jsonv2` | Not yet validated. Test the local MID Web Server listener before enabling gzip. |
| `mid` | `business_rules` | Keep uncompressed unless a target-specific validation proves otherwise. The documented MID forwarding properties for the `insertMultiple` compatibility path include `mid.probe.event.queue.compress=false`. |

Collector HTTP compression is configured per exporter instance. If one pipeline needs compressed direct JSON v2 and another needs uncompressed Business Rules compatibility, configure two separate `servicenow_event_management` exporters.

## Authentication

Use Collector HTTP client auth features instead of exporter-owned credential flows. Basic, bearer token, OAuth2 client credentials, MID API keys, custom headers, TLS settings, and client certificates should be configured through `auth`, `headers`, and `tls`.

The ServiceNow instance JSON v2 documentation lists `evt_mgmt_integration` as the required role. Whichever direct-instance ServiceNow identity your auth path resolves to must have that role for Event Management ingestion: the Basic-auth user, the bearer-token user, the OAuth Application User used by client credentials, or the user mapped from a client certificate. MID Web Server auth is a separate client-to-MID concern; follow the MID Server setup docs for the MID-to-instance account and roles.

For direct ServiceNow mTLS, first complete ServiceNow certificate-based authentication setup on the instance. The exporter can present a client certificate with `tls.cert_file` and `tls.key_file`, but those settings only work as authentication after the ServiceNow endpoint requests and trusts client certificates.

## Message Key

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `message_key.format` | string | `legacy` | `legacy` joins values with `separator`; `sha256_v1` emits a framed SHA-256 key for generated identities. |
| `message_key.attributes` | list of strings | empty | Attribute names used to build `message_key` when every listed attribute is present and non-empty. |
| `message_key.separator` | string | `|` | Separator used only by the `legacy` format when joining configured attributes or default identity fields. |

Omitting `message_key.format`, or setting it to an empty string, preserves `legacy`; the factory default explicitly selects `legacy`. Unknown formats fail configuration validation. Explicit non-empty `servicenow.message_key` always wins, with log attributes taking precedence over resource attributes and the existing OTel string conversion. In `legacy`, configured values and the default fields are joined with `message_key.separator`, empty default fields are removed, and the resulting key follows the usual byte limit and truncation metadata behavior. Delimiter characters inside values are not escaped, and truncation notices do not preserve identity. Keep identity values stable, avoid the separator inside legacy components, and keep the complete joined key within `field_limits.message_key` when using this format.

If `message_key.attributes` is set but any listed attribute is missing or empty, the exporter falls back as a whole to the default identity fields: `source`, `node`, `type`, `resource`, and `metric_name`. Missing configured attribute names are normally recorded in `additional_info`, but this diagnostic is best-effort if the auxiliary metadata byte budget is exhausted. In either format, a present empty log attribute shadows the resource value and counts as missing. `message_key.separator` cannot be only whitespace; leave it unset to use the default separator. The separator does not affect `sha256_v1` and non-default separators remain accepted in that format.

`sha256_v1` is an opt-in generated-key format for deployments that need tuple boundaries and empty default-field positions preserved before applying ServiceNow field limits. With all configured attributes present, it hashes the ordered attribute-name/value pairs. Otherwise, including when no attribute list is configured, it hashes the five full mapped default-field pairs in the order `source`, `node`, `type`, `resource`, `metric_name`, including empty values. It uses the existing attribute string conversion and log-over-resource lookup. Severity, timestamps, body, and other attributes do not affect the generated identity unless selected in `message_key.attributes`. Generated keys are 79 ASCII bytes and are computed before any event-field truncation. This representation is exporter-defined and is not ServiceNow's native key algorithm.

In `sha256_v1`, explicit producer-supplied keys remain unchanged when their byte length fits `field_limits.message_key`; explicit keys are neither escaped, prefixed, nor hashed. An explicit key above that configured byte budget fails mapping with a sanitized permanent error before transport. This mode never truncates a generated key or explicit key. Set `field_limits.message_key` to at least `79` for this mode. A fitting explicit key may be longer than 1,024 bytes when an operator raises the limit after validating the target instance. One offending oversized explicit key rejects the entire mapped Collector callback batch, so otherwise valid records in that same batch are not sent and the mapping error is not retried. With queueing enabled, exporter-helper processing surfaces that error, so it may not be returned to the original `ConsumeLogs` caller.

Opting into `sha256_v1` changes generated event identities. Before cutover, close or drain alerts under the old keys, or keep an explicitly managed old-key route for their update and clear lifecycle. Account for queued or replayed records as well; restarting a Collector does not close old alerts. Changing configured attribute names or order, or entering or leaving the configured-attribute fallback path, can change identity in either format. Use the same format, attribute order and names, values, and byte-budget policy across replicas and for firing and clear records. Keep explicit upstream keys when identity must interoperate across pipelines. This exporter does not dual-send or migrate existing alerts automatically.

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
| `field_limits.message_key` | `1024` (minimum `79` with `sha256_v1`) |
| `field_limits.description` | `4000` |
| `field_limits.additional_info` | `4000` |
| `field_limits.resolution_state` | `40` |

The exporter truncates fields to these byte limits before transport and normally records truncated field names under `otel.servicenow.truncated_fields` in `additional_info`. This diagnostic is best-effort when the auxiliary metadata byte budget is exhausted. In `legacy`, this includes `message_key`; truncation notices report that bytes were lost but do not preserve the original identity. In `sha256_v1`, generated keys are already 79 bytes and are not truncated, while oversized explicit keys fail mapping instead of being shortened. This is intentional even when ServiceNow JSON v2 accepts a longer value, because some fields are silently truncated during storage and the JSON v2 response does not report that loss.

The default limits reconcile ServiceNow documentation with live PDI evidence. `source` uses `200` because the ServiceNow Event API, the PDI `em_event.source` dictionary entry, and JSON v2 read-back agree on 200, even though some Event Management field-format pages still list 100. `node` stays at `100` because the field-format docs, dictionary metadata, and live read-back agree. `metric_name` uses the observed dictionary value `1024` as the portable cap; a live PDI stored 1201 characters, but ServiceNow platform dictionary docs explain that string max length values above 254 can map to a larger physical text type, so this is not a portable API contract.

All field limits are configurable for deployments that have verified a different local schema or ServiceNow family behavior. For example, an instance that has validated longer `metric_name` storage can set `field_limits.metric_name` above `1024`. Raising limits means the exporter will send and trust those longer values; validate the target instance with read-back before relying on that behavior for identity fields. `field_limits.message_key` must be at least `79` in `sha256_v1` and has no exporter-side upper cap. Raising it lets fitting explicit keys longer than `1,024` bytes pass unchanged, so validate the target before relying on that behavior.

All field limits must be positive. `field_limits.additional_info` must be at least `256` bytes so the pinned exporter metadata envelope can fit after truncation. The budget applies to the encoded `additional_info` JSON text, including keys and JSON escaping, before the text is escaped as a string in the outer request JSON. Other exporter diagnostics are best-effort if their complete encoded entries do not fit.

## Additional Info

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `additional_info.include_attributes` | glob list | empty | Optional allowlist. When non-empty, only matching attributes are copied. |
| `additional_info.exclude_attributes` | glob list | empty | Attribute names to omit. |
| `additional_info.redact_attributes` | glob list | common secret patterns | Attribute names whose values become `[REDACTED]`. |
| `additional_info.max_attributes` | integer | `128` | Maximum number of non-control attributes copied. Use `0` for no count limit. |
| `additional_info.max_value_length` | integer | `4000` | Maximum bytes per copied value. The final JSON string must still fit `field_limits.additional_info`. |

All `additional_info` values are string-valued. Attributes beginning with `servicenow.` are exporter control attributes and are not copied. The exact key `otel.servicenow.additional_info.metadata_reduced` is also reserved. If a producer value under that key passes filtering, rendering, redaction, and the attribute-count limit, it is counted once as a dropped optional entry by normal drop accounting and never copied as the marker. No configuration change is needed for other producers; producers relying on this exact key for enrichment must rename it to retain the value.

ServiceNow's [Event Management field format](https://www.servicenow.com/docs/r/it-operations-management/event-management/c_EMIntegrateRequirementEvent.html?contentId=MXxZKDzOzoP0FwsQ1IlNwA) describes `additional_info` as a JSON string and says JSON values should be strings. That is why this exporter stringifies non-string OTel values rather than sending a nested object. Strings stay unchanged, scalar numbers and bools use stable string formatting, maps and slices become compact JSON strings, and bytes encode as base64. Values that cannot be rendered safely are dropped and normally counted under `otel.servicenow.additional_info.dropped_by_value`; the counter is best-effort and may be omitted when metadata reduction leaves insufficient room.

The default value length matches the default total `additional_info` field budget. ServiceNow's Event API documentation lists `additional_info` as a predefined Event field with a 4000-character maximum, so this exporter does not treat it as unbounded. Raise `field_limits.additional_info` and validate read-back on the target instance before relying on larger payloads.

If required exporter metadata still exceeds the byte budget after optional attributes are shed, the event is sent with a compact envelope. The exporter keeps `otel.signal`, the original numeric severity when specified, `otel.servicenow.additional_info.metadata_reduced="true"`, and the computed `otel.servicenow.additional_info.dropped_attributes` count when nonzero. It then considers complete metadata entries by priority: original severity text, configured-fallback strategy, missing identity names, truncated field names, dropped-by-count, redacted-attribute, dropped-by-value, and truncated-value counts. Each entry is retained only when its complete JSON-encoded form fits. The marker indicates that some exporter metadata is omitted; it does not enumerate omitted keys or count events. It is absent during ordinary successful packing. The pinned envelope is at most 192 encoded bytes with the lowest signed 32-bit severity and largest nonnegative 64-bit drop count, so the supported minimum budget of 256 bytes always fits it. Numeric severity, message-key selection, and mapped ServiceNow event fields are unchanged.

ServiceNow can use Additional information keys to populate custom alert fields when a key name matches the alert field's technical name. To use that behavior, make the upstream OTel attribute name match the target alert field name, or use a transform processor included in the deployed Collector distribution to copy or rename the value before this exporter runs. For example, include fields such as `user_service_owner`, `u_service_owner`, or an application-scoped custom field name only when those are the actual field names in the target ServiceNow instance.

When `additional_info.include_attributes` is non-empty, it must include those custom alert field names or they will not be sent. If a custom alert field is operationally important, tune `additional_info.max_attributes`, `additional_info.max_value_length`, and `field_limits.additional_info` so the key is not dropped before ServiceNow processes the event.

Attribute pattern options use Go [`path.Match`](https://pkg.go.dev/path#Match) glob syntax. The exporter lowercases attribute names and patterns and trims pattern whitespace, then matches the complete top-level attribute name. For example, the default `*authorization*` matches both `authorization` and `headers.authorization` but not the literal name `headers/authorization`, because `*` does not cross `/`; `headers/authorization` can match that complete slash-containing name. Neither pattern form searches inside a map or slice. If a `redact_attributes` pattern matches a parent such as `headers`, the entire copied parent value becomes `[REDACTED]`; there is no recursive nested-key matching.

These filters govern only copied resource and log attributes. They do not filter mapped ServiceNow fields, the log body or resulting `description`, or exporter-generated metadata. Original `otel.severity_text` is included when present on the log record and retained only if the metadata entry fits the configured budget. Attributes prefixed with `servicenow.` are omitted from enrichment; recognized controls such as `servicenow.message_key`, `servicenow.severity` (or configured `severity.from_attribute`), and `servicenow.resolution_state` still participate in mapping. See [Data Safety guidance](production-hardening.md#data-safety) for the trust boundary and input precedence. Empty patterns and malformed patterns fail startup validation. `additional_info.max_attributes` must be non-negative; `0` disables the count limit. `additional_info.max_value_length` must be positive.

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

### Selected Trace Exception Recipe

The separate [trace exception recipe](../examples/servicenow-event-management-trace-exception-oauth.yaml) and [dedicated builder manifest](../examples/collector-builder-trace-exception.yaml) form an opt-in Collector Builder distribution, not part of the default manifest. It pins Collector and contrib modules to v0.153.0 and env/file/YAML providers to v1.59.0. The manifest pins exporter v0.2.0 for consumers after that tag is available from the Go module proxy; use `make test-trace-exception` to exercise the recipe against the current checkout. Selection is deliberately narrow: resource `service.name`, `host.name`, and `deployment.environment.name` must each be nonempty strings; the span status must be Error; and an event must have name `exception` plus a string `exception.type` exactly equal to `SelectedFailure`. This synthetic rule does not assert that every exception means an operational alert.

The recipe trusts those three identity fields only from the resource, and carries producer-supplied native trace/span IDs and instrumentation-scope values into `additional_info` when present. Neither the identity fields nor native context are authenticated or inherently secret-free; deploy this pipeline only where the upstream source is trusted and apply the normal data-minimization controls. It removes producer span fields and replaces span host/environment with the resource values before the connector chooses dimensions. The connector copies span attributes and unconditionally writes `exception.stacktrace`, so a downstream log allowlist removes the copied stacktrace, producer ServiceNow controls, and other unapproved values before mapping. The example fixes source/class/type/metric/severity/resolution/description; resource is derived from trusted `service.name` and node from trusted `host.name`. Its allowlist retains the fixed rule and provenance, trusted identity, selected exception type, and native IDs/scope fields in `additional_info`.

The fixed rule groups operations by service, host, and environment. Its `sha256_v1` key hashes the ordered fields `alert.rule_id`, `service.name`, `host.name`, `deployment.environment.name`, and `exception.type`. It intentionally omits `service.namespace`; deployments where that distinguishes otherwise identical services must require and hash namespace too. Any prior-key cutover is operator-owned, and enabling this recipe does not migrate existing alerts. Each selected exception occurrence is emitted independently; the recipe has no pending/firing/clear/recovery state or duplicate suppression. It also defines no missing-data policy, rate threshold, aggregation, backend ordering, or compensation for sampled-out traces. Transform errors propagate, so the focused gate checks that a failed parse returns OTLP HTTP 503 before any ServiceNow request.

### Native Log Context

The exporter does not automatically copy native log trace IDs, span IDs, or instrumentation-scope name and version into `additional_info`. A producer can copy them into ordinary attributes upstream, or use the optional [log-context overlay](../examples/servicenow-event-management-log-context.yaml), which contains the canonical four OTTL statements. It copies only nonzero IDs and nonempty scope fields. The ID values are lowercase 32-character trace IDs and 16-character span IDs in hexadecimal. The overlay leaves native context, the body, ServiceNow control attributes, and the default condition identity unchanged.

Apply the checked-in [Basic example](../examples/servicenow-event-management-exporter.yaml) first, then the overlay:

```bash
otelcol --config examples/servicenow-event-management-exporter.yaml \
  --config examples/servicenow-event-management-log-context.yaml
```

The overlay is not standalone. It requires a distribution that includes the contrib `transformprocessor`; the default `builder-config.yaml` does not include it. From the repository root, use the pinned MikeFarah `yq` CLI to make a temporary manifest copy, resolve its local exporter and output paths, add Transform v0.153.0, and build:

```bash
repo_root="$(pwd)"
recipe_dir="$(mktemp -d "${TMPDIR:-/tmp}/servicenow-log-context.XXXXXX")"
output_path="$recipe_dir/otelcol-servicenow-event-management-dev"
cp builder-config.yaml "$recipe_dir/builder-config.yaml"
RECIPE_REPO_ROOT="$repo_root" RECIPE_OUTPUT_PATH="$output_path" \
  yq -i '(.dist.output_path) = strenv(RECIPE_OUTPUT_PATH)
   | (.exporters[] | select(.import == "github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management/exporter/servicenoweventmanagementexporter") | .path) = strenv(RECIPE_REPO_ROOT)
   | .processors += [{"gomod":"github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor v0.153.0"}]' \
  "$recipe_dir/builder-config.yaml"
./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder v0.153.0
.tools/bin/builder --config "$recipe_dir/builder-config.yaml"
collector="$output_path/otelcol-servicenow-event-management-dev"
SERVICENOW_INSTANCE_URL=https://example.service-now.com \
SERVICENOW_USERNAME=synthetic-user \
SERVICENOW_PASSWORD=synthetic-password \
  "$collector" validate \
    --config examples/servicenow-event-management-exporter.yaml \
    --config examples/servicenow-event-management-log-context.yaml
```

Config sources merge maps, while a later source replaces a conflicting list by default. The overlay therefore supplies the full `logs.processors` list, preserving the Basic example's `memory_limiter` and `batch` around the Transform processor. Adapt that list deliberately when using another pipeline layout; do not expect processors to be appended automatically. See the [Collector v0.153.0 configuration merge behavior](https://github.com/open-telemetry/opentelemetry-collector/blob/v0.153.0/confmap/README.md#experimental-append-merging-strategy-for-lists).

Each copy statement checks that the destination getters for both log and resource attributes are `nil`. The Transform processor uses `error_mode: ignore`, so a statement error is logged and later statements continue without deliberately dropping the log. Existing non-nil producer values are preserved, including empty strings, numbers, and booleans; when both levels contain a value, the exporter continues to prefer the log attribute.

The historical fixed-timestamp recipe run used Collector v0.151.0 and found that an empty AnyValue (`value: {}`) and a JSON-null AnyValue (`value: null`) both compare equal to `nil` in this pdata path. This is retained as historical evidence, not a v0.153.0 rerun. When both destination getters are nil, a populated native source replaces either value. With no native source, the existing empty value is exported as an empty string. A null or empty log-level destination still shadows a non-nil resource value because the guard requires both destination getters to be nil; the recipe does not preserve mere key existence.

These names are ordinary attributes, not reserved exporter controls or a ServiceNow trace schema. They pass through the same include, exclude, redaction, count, value-length, and total encoded-byte policies as other resource and log attributes. Add the four names to an existing allowlist without removing its current entries; for example, retain the existing pattern while adding the context names:

```yaml
additional_info:
  include_attributes:
    - "existing.customer.pattern"
    - trace_id
    - span_id
    - instrumentation_scope.name
    - instrumentation_scope.version
```

Exclusions still win, and redacted values use the fixed `[REDACTED]` marker. Added attributes can displace other optional enrichment under count or byte limits, and reduced-metadata fallback may omit all optional attributes; the exporter gives these four names no retention priority. Audit `message_key.attributes` before enabling the overlay: a context name in that list can change identity when it becomes present and completes a configured key that previously fell back to default fields. Leave diagnostic context out of `message_key.attributes` unless that identity change is intended.

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
