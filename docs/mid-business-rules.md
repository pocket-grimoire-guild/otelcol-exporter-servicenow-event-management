# MID Business Rules Compatibility

MID Business Rules compatibility is a ServiceNow MID Server forwarding setup. It is not a different exporter-facing URL.

The exporter still sends JSON v2 payloads to the local MID WebService listener:

```text
POST http(s)://<mid-host>:<port>/api/mid/em/jsonv2
```

The MID Server then needs upstream Event Management properties that forward those events to the Business Rules-compatible instance endpoint. ServiceNow documents this path for deployments that need Business Rules on `em_event` to run.

## Required MID Properties

Record the exact values used in the validation evidence. The ServiceNow documentation reviewed for this project calls out these properties:

| Property | Value |
| --- | --- |
| `mid.probe.event.endpoint.url` | `em_event.do?JSONv2%26sysparm_action=insertMultiple` |
| `mid.probe.event.bulk_size` | Start with `100`, then tune with target-instance validation. |
| `mid.probe.event.queue.compress` | `false` for the documented `insertMultiple` compatibility path. |

Apply these through the normal ServiceNow/MID Server property management path for the target environment, then restart the affected MID Web Server and Event Management listener contexts so the runtime uses the new forwarding behavior.

## Public Validation

This path has been validated against a ServiceNow PDI with a local Linux MID runtime. The validation used a dedicated test MID Server record and the three properties above.

The proof compared normal MID JSON v2 behavior with Business Rules-compatible forwarding, then confirmed the expected Business Rules side effect through ServiceNow read-back. Temporary validation-only Business Rules and MID property changes were removed afterward and normal MID JSON v2 behavior was revalidated. See [May 2026 PDI validation evidence](validation-evidence/2026-05-pdi-validation.md#validated-paths).

## Validation Checklist

1. Prove normal MID JSON v2 first with `SERVICENOW_MODE=mid` and `SERVICENOW_API=jsonv2`.
2. Configure the MID forwarding properties above.
3. Run the direct smoke through the MID listener with Business Rules intent:

   ```bash
   SERVICENOW_SEND=1 \
   SERVICENOW_MODE=mid \
   SERVICENOW_API=business_rules \
   SERVICENOW_INSTANCE_URL=https://mid-host.example.net:8443 \
   SERVICENOW_MID_API_KEY=... \
   make smoke-servicenow-jsonv2
   ```

4. Run a Collector-backed rich smoke against the same MID listener.
5. Read back `em_event` rows by run id and verify the customer-specific Business Rules side effect that required this compatibility path.
6. Record the MID properties, commands, HTTP status, read-back evidence, and Business Rules side effect in the validation evidence.

Do not treat a successful POST to `/api/mid/em/jsonv2` alone as Business Rules validation. It proves only that the exporter reached MID; the compatibility claim requires ServiceNow read-back evidence after the MID forwarding properties are in effect.
