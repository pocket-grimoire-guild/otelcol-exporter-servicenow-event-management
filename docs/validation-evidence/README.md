# Validation Evidence

This directory stores concise, sanitized evidence from real ServiceNow and MID Server validation. Keep credentials, raw transcripts, local paths, exact run IDs, and operational setup notes out of public evidence.

Update this directory when a validation claim changes, then update [../compatibility.md](../compatibility.md) so each matrix row links to the evidence that supports it.

## Evidence Template

```text
Date:
Instance type:
ServiceNow family:
Plugin state:
Endpoint mode:
Endpoint API:
Auth extension or smoke auth path:
Commands run:
HTTP status:
Response shape:
ServiceNow UI or em_event verification:
Batching verification:
Field mapping adjustments needed:
Follow-up tests added:
```

Do not paste credentials, authorization headers, customer payloads, full request bodies containing customer data, or unredacted `additional_info` values.
