# Production Hardening

This exporter should be treated as an Event Management ingestion path, not as a generic log dump. Production quality depends heavily on the upstream alert/event shape.

For the ServiceNow documentation behind endpoint, role, field, OAuth, and MID choices, see [ServiceNow Decision Sources](product-specs/servicenow-event-management-exporter.md#servicenow-decision-sources) and [the source map](references/source-map.md).

## Event Identity

Set explicit identity fields close to the source:

```text
servicenow.node
servicenow.event_class
servicenow.metric_name
servicenow.message_key
servicenow.severity
```

`message_key` controls deduplication. Avoid keys made only from broad fields such as service name or event class. Good keys are stable for the same condition and distinct across unrelated conditions.

Clear events should use the same `servicenow.message_key` as the event they close, severity `0`, and either explicit `servicenow.resolution_state=Closing` or the default clear-resolution behavior.

See the [Field Mapping lifecycle illustration](product-specs/servicenow-event-management-exporter.md#field-mapping) for optional producer labels and ownership boundaries.

## Data Safety

Use `additional_info.include_attributes` when sending logs from arbitrary applications. Without an allowlist, the exporter still redacts common secret names and enforces byte limits, but application logs can contain business data that cannot be safely inferred from attribute names.

These include, exclude, and redact patterns apply only to top-level resource and log attributes copied into `additional_info`; they do not sanitize the whole event. A matching redaction replaces the complete copied attribute value, so an allowed map or slice can still carry sensitive nested values unless its parent attribute is redacted. Patterns such as `*.body` match attribute names, not `LogRecord.Body`.

Description mapping checks `servicenow.description`, then `event.description`, the log body, and severity text. Each attribute lookup prefers a present log attribute over the same-name resource attribute, so a resource `servicenow.description` can be selected before a log `event.description`. Other mapped event fields are outside these filters. Original `otel.severity_text` may also be retained in `additional_info` when present and when metadata budgeting allows it.

Review content at its source and after upstream processing, then prefer a minimized event summary containing only approved incident detail over forwarding arbitrary log content. OpenTelemetry's [sensitive-data guidance](https://opentelemetry.io/docs/security/handling-sensitive-data/) recommends context-specific data minimization and describes processors that can modify telemetry. Use an upstream processor only when it is included in the deployed Collector distribution; this repository's local builder includes neither the transform nor redaction processor. See [Additional Info configuration](configuration.md#additional-info) for exact pattern behavior.

Treat `servicenow.message_key`, `servicenow.severity` (or the configured `severity.from_attribute`), and `servicenow.resolution_state` as trusted event-authoring inputs, even though `servicenow.*` attributes are omitted from `additional_info`. They can be supplied at either resource or log scope; for a matching key, a present log attribute takes precedence over the resource value. A non-empty message-key override sets event identity; otherwise configured message-key attributes or mapped default fields supply it. An invalid log severity override falls through to the OTel severity mapping and configured default rather than the resource override. For a Clear event, an absent or invalid resolution-state override can select the configured clear state (`Closing` by default). Review and remove or overwrite unapproved controls at both scopes before trusted event generation; removing only a log override can expose a resource-level value. Preserve approved stable identity and clear-event controls. See [the mapping and severity reference](../exporter/servicenoweventmanagementexporter/README.md#default-log-to-event-mapping) for mapping details.

ServiceNow can promote named Additional information values into custom alert fields when the key matches the alert field's technical name. Treat those keys as part of your alert contract, not as incidental log attributes. If custom alert fields drive reporting or routing, add the exact field names to `additional_info.include_attributes` and leave enough attribute and byte budget for them.

Recommended pattern:

```yaml
additional_info:
  include_attributes:
    - deployment.environment
    - service.*
    - cloud.*
    - k8s.*
    - alert.*
    - ci.*
    - user_service_owner
    - u_service_owner
  exclude_attributes:
    - "*.body"
    - "http.request.header.*"
```

If upstream telemetry uses different names, copy or rename them before export with a transform processor included in the deployed Collector distribution. ServiceNow Event Rules can enrich or alter alert processing after ingestion; they are not upstream sanitization. The exporter intentionally does not create ServiceNow custom alert fields and does not add custom fields to the Event table.

Do not log Collector configs with resolved secrets. Prefer environment variables, Collector secret providers, Kubernetes secrets, Vault, or your platform secret manager.

## Authentication

Use ServiceNow's least-privileged Event Management integration role, normally `evt_mgmt_integration`. ServiceNow documents this as the required role for direct instance JSON v2 ingestion, so the role belongs on the Basic-auth user, the bearer-token user, the OAuth Application User, or the certificate-mapped user for direct instance mode.

Recommended production default:

- Use OAuth2 client credentials through Collector `oauth2client` for direct instance ingestion when the ServiceNow environment supports it.
- Store the client ID and secret in a secret manager, Collector secret provider, Kubernetes secret, or platform equivalent. Do not check resolved values into Collector configs.
- Configure ServiceNow's OAuth Application User with the least-privileged Event Management ingestion role. On some instances, securely scoped OAuth clients also need an appropriate REST API auth scope before JSON v2 ingestion works.

Supported alternatives:

| Auth path | Best fit | Notes |
| --- | --- | --- |
| `oauth2client` | Long-running direct instance deployments | Preferred when available because the Collector obtains and refreshes short-lived access tokens. ServiceNow must have inbound client credentials enabled and an OAuth Application User configured. |
| `basicauth` | Labs, PDIs, and tightly controlled deployments | Simple and validated, but it sends the long-lived credential on every request. Prefer a dedicated integration user and rotate the password. |
| `bearertokenauth` | Validation or externally rotated token environments | The Collector injects a static bearer token. It does not refresh the token; external automation must rotate it before expiry. |
| `headers_setter` or HTTP `headers` | MID Web Server API-key mode and custom gateway headers | Keep MID API keys in secrets. Use `Authorization: Key <key>` for MID API-key mode. |
| HTTP `tls.cert_file` / `tls.key_file` | Environments that require client certificates | The exporter only presents the certificate. ServiceNow or MID must be configured to request, trust, and map the certificate. |

Direct ServiceNow instance mTLS also requires ServiceNow certificate-based authentication setup: publish the client CA, map the client certificate to the integration user, and verify the ServiceNow endpoint requests client certificates. MID mTLS is configured on the MID Web Server context and has separate keystore/truststore operations.

Static bearer token auth is useful for validation and environments with external token rotation. It is not a refresh mechanism by itself.

## Batching And Backpressure

There are two separate batching layers:

- The Collector `batch` processor shapes log records before they reach exporters. Use `send_batch_max_size` as the hard cap.
- Exporterhelper `sending_queue.batch` can merge queued requests before this exporter sends them to ServiceNow. If enabled, set `max_size` to the same hard cap so queueing cannot create larger ServiceNow POSTs than intended.

Start conservatively:

```yaml
processors:
  batch:
    timeout: 10s
    send_batch_size: 100
    send_batch_max_size: 100

exporters:
  servicenow_event_management:
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
      max_elapsed_time: 300s
```

Tune with a real sub-production ServiceNow instance before increasing batch size. ServiceNow public JSON v2 docs show multi-record payloads but do not document a maximum `records` count. The exporter therefore should not impose an arbitrary global maximum; operators need to choose a cap based on their instance, event rules, alert correlation cost, and incident response tolerance. The examples use `100` as a conservative starting point, not as a proven platform limit.

Increase capacity in this order:

1. Validate event quality and deduplication with small batches.
2. Raise Collector `batch.send_batch_size` and `send_batch_max_size` together.
3. If using queue batching, keep `sending_queue.batch.max_size` equal to the processor hard cap.
4. Increase `queue_size` only after confirming memory budget and acceptable delayed-delivery behavior.
5. Load test on a non-production ServiceNow instance and record accepted count, time to Event Management visibility, HTTP status distribution, and alert correlation side effects.

Watch ServiceNow response codes, Event Management processing lag, alert correlation behavior, Collector queue depth, exporter retry counts, and Collector memory. A larger queue can protect against short ServiceNow outages, but it also increases the number of stale events that can arrive later and the memory/disk pressure carried by the Collector.

Use `api: jsonv2` unless the deployment explicitly needs ServiceNow Event table Business Rules to run. ServiceNow documents the `business_rules` compatibility path as lower-throughput than the JSON v2 API. For MID, validate both the local MID listener and the MID Server upstream forwarding properties before using compatibility mode as a production claim.

## Request Compression

Batched Event Management JSON is highly compressible, so gzip can materially reduce bandwidth when sending high event volumes to ServiceNow. Enable it only on endpoint paths that accept compressed request bodies:

```yaml
exporters:
  servicenow_event_management:
    endpoint: https://example.service-now.com
    mode: instance
    api: jsonv2
    compression: gzip
```

The June 2026 PDI probe validated `Content-Encoding: gzip` for direct instance JSON v2, `/api/global/em/jsonv2`, with `em_event` read-back. The same probe found that direct Business Rules compatibility, `em_event.do?JSONv2&sysparm_action=insertMultiple`, does not handle gzipped request bodies in the tested PDI: ServiceNow returned HTTP 200 with a JSON error and inserted no event. MID JSON v2 compression still needs separate validation against a running MID listener; for MID Business Rules forwarding, ServiceNow's documented `insertMultiple` properties include `mid.probe.event.queue.compress=false`.

Treat compression as a route-specific production setting. Validate with read-back on the actual target instance or MID listener, and keep `api: business_rules` uncompressed unless that exact path has proven otherwise.

## Failure Behavior

- Network errors, `408`, `429`, and `5xx` are retryable. `Retry-After` on `429` and `503` is passed to Collector retry handling as a throttle delay. During bounded verification of a successful 2xx response, recognized read interruptions (`io.ErrUnexpectedEOF`, context cancellation/deadline, or a `net.Error`) are retryable only if the read has not crossed the verification limit. Unknown body read errors remain permanent.
- Other `4xx` responses are permanent and will be dropped by Collector retry logic.
- Non-empty 2xx response bodies are inspected for ServiceNow JSONv2 failure metadata. Record-level or top-level body failures, malformed JSON, non-JSON bodies, complete invalid gzip header/checksum errors, and oversized bodies that cannot be verified are treated as permanent exporter errors.
- Error messages include endpoint mode, API flavor, and status code, but omit arbitrary response bodies for data safety.

Retrying an interrupted 2xx response may replay the entire mapped Collector callback. ServiceNow might have inserted some or all events before the acknowledgment was interrupted, so replay can create duplicate Event rows and rerun Event table Business Rules. Alert `message_key` correlation does not guarantee idempotent insertion or exactly-once delivery. The exporter does not selectively resend records from a partially read response.

`io.ErrUnexpectedEOF` is a delivery-policy category, not proof of a socket fault or transience; it can also identify a truncated compressed representation. With `sending_queue` disabled, caller cancellation or deadline stops the active send and prevents further helper retries. Default asynchronous queueing detaches queued work from the original caller cancellation/deadline; a per-attempt HTTP timeout can retry while the active send context remains live.

With `sending_queue` enabled in its default asynchronous mode (`wait_for_result: false`), `ConsumeLogs` success confirms enqueueing only; it does not confirm ServiceNow acceptance. `exporterhelper` owns retries and queue processing. Keep a finite `retry_on_failure.max_elapsed_time` based on event usefulness; the default is five minutes, and `0s` allows unlimited elapsed retry time. The cutoff is checked after failed attempts before scheduling another retry, so it does not cancel an in-flight read. Keep the HTTP `timeout` finite too; its default is 30 seconds. Disabling retries gives one attempt and also disables retries for network, throttling, and server-status failures.

Separate requests with the same `message_key` have no exporter per-key or end-to-end ordering guarantee; see [Error Handling](product-specs/servicenow-event-management-exporter.md#error-handling) for a firing/Clear retry example. For order-sensitive alert lifecycles, verify the final alert state after event processing and any relevant delayed transitions; HTTP acceptance alone does not establish it. Include whether Clear events closed the expected alerts, as described in [Load Testing / What To Measure](load-testing.md#what-to-measure).

Treat repeated `429` or `503` responses as a capacity or availability signal, not just as something to hide with retries. First reduce input rate or batch size, then lengthen retry intervals or queue depth if the business can tolerate delayed events. Choose `max_elapsed_time` based on how long an event remains useful for Event Management correlation.

For permanent `4xx` responses, fix credentials, roles, token scopes, endpoint mode, or payload mapping before retrying the same traffic. Retrying permanent auth or validation failures can create unnecessary ServiceNow load without recovering data.

## Operational Pipeline Shape

Production pipelines should keep thresholding and event semantics upstream from the exporter. A typical direct-instance pipeline looks like this:

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

processors:
  transform/servicenow_fields:
    error_mode: propagate
    log_statements:
      - context: log
        statements:
          - set(attributes["servicenow.source"], "opentelemetry")
          - set(attributes["servicenow.type"], "application")
  filter/event_threshold:
    error_mode: propagate
    logs:
      log_record:
        - 'attributes["metric_value"] <= 90'
  batch:
    timeout: 10s
    send_batch_size: 100
    send_batch_max_size: 100

exporters:
  servicenow_event_management:
    endpoint: ${env:SERVICENOW_INSTANCE_URL}
    auth:
      authenticator: oauth2client/servicenow
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
      max_elapsed_time: 300s

extensions:
  oauth2client/servicenow:
    client_id: ${env:SERVICENOW_CLIENT_ID}
    client_secret: ${env:SERVICENOW_CLIENT_SECRET}
    token_url: ${env:SERVICENOW_TOKEN_URL}
    grant_type: client_credentials

service:
  extensions: [oauth2client/servicenow]
  pipelines:
    logs:
      receivers: [otlp]
      processors: [transform/servicenow_fields, filter/event_threshold, batch]
      exporters: [servicenow_event_management]
```

Use `make smoke-hec-fanout` as the repeatable demonstration harness for a richer fan-out path: Splunk HEC input, Collector enrichment, threshold filtering to ServiceNow, and all-log fan-out to Splunk HEC. Keep `sourcetype=otel:servicenow-hec-fanout` for that harness so Splunk read-back stays predictable.

For a metrics-derived event path, use [examples/servicenow-event-management-metrics-event-oauth.yaml](../examples/servicenow-event-management-metrics-event-oauth.yaml) and `make smoke-metrics-event`. That example keeps raw metric ingestion out of the exporter: the metrics pipeline filters only threshold breaches, `metricsaslogs` converts surviving datapoints into log records, and the logs pipeline sets ServiceNow event fields before export. Treat it as a breach-event example, not a complete alert lifecycle engine; production clear events should come from a stateful alerting source or a pipeline that explicitly emits `severity=0` with the same `message_key`.

## Monitoring And Runbooks

At minimum, operate the Collector with its own telemetry enabled and alert on:

- exporter send failures by status class;
- retry counts and retry latency;
- queue length or queue capacity use;
- dropped telemetry from memory limiter or permanent exporter errors;
- Collector process restarts and memory pressure;
- time from send to ServiceNow `em_event` visibility during scheduled canaries.

Runbook starting points are below; use [Troubleshooting](troubleshooting.md) for a fuller live-smoke checklist.

| Symptom | First checks |
| --- | --- |
| `401` or `403` | Auth extension wiring, token expiry, OAuth Application User, REST API auth scope, `evt_mgmt_integration` role, MID API key format. |
| `404` | Endpoint URL, `mode`, `api`, Event Management plugin state, MID listener path. |
| `429` | ServiceNow ingest capacity, batch size, input rate, retry interval, queue pressure. |
| `503` or network errors | ServiceNow availability, proxy/TLS failures, retry behavior, queue retention. |
| Events accepted but not useful | `message_key`, `node`, `resource`, `metric_name`, severity, Event Management rules, CMDB binding. |
| Splunk fan-out missing rows in the HEC fan-out harness | Confirm HEC event endpoint, `sourcetype=otel:servicenow-hec-fanout`, index, search time range, and indexing delay. |

## Rollout Checklist

- Validate against a ServiceNow development or sub-production instance.
- Confirm Event Management plugin and `evt_mgmt_integration` role.
- Prefer OAuth2 client credentials for direct instance production deployments; document any reason for Basic, static bearer, or custom header auth.
- For direct mTLS, confirm ServiceNow certificate-based authentication is publishing CA trust material and the endpoint requests client certificates.
- Confirm events appear in Event Management, not just as accepted HTTP responses.
- Verify `additional_info` is parseable JSON string data.
- Verify clear events close or update the expected alert.
- Verify batch size, queue size, retry settings, and `Retry-After` behavior under expected load.
- Confirm retention and privacy requirements for copied attributes.
- Run `SERVICENOW_HEC_FANOUT_DRY_RUN=1 make smoke-hec-fanout`, then run a live `make smoke-hec-fanout` against non-production ServiceNow and Splunk when fan-out behavior is part of the deployment.
