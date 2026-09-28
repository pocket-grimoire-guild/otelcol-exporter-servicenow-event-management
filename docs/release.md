# Release Process

This project is intended to be consumed as a Collector Builder component pinned by Go module tag.

## Before A Release

1. Confirm the canonical repository and module paths are final before selecting the release version.
2. Confirm `go.mod`, `builder-config.yaml`, examples, registry metadata, generated metadata, verification checks, and docs all use that final module path.
3. Confirm code-level publication blockers remain closed, including bounded 2xx JSONv2 response parsing so per-record ServiceNow failures are not silently accepted.
4. Confirm the supported Collector and Go version policy. Either update to the intended current Collector baseline or document the pinned baseline in release notes and support docs.
5. Confirm the standalone support model: named maintainers, security reporting path, release cadence, ServiceNow API compatibility expectations, and config deprecation policy.
6. Run:

   ```bash
   make verify
   ```

7. Record the current compatibility matrix in [docs/compatibility.md](compatibility.md).
8. For any release described as production-ready or stable, complete and record all release validation gates:

   - Instance JSON v2 ingestion through a customer-like development or sub-production instance, not only a PDI.
   - Direct instance mTLS/certificate-based authentication if release notes claim that path is validated.
   - Tested maximum batch size and observed ServiceNow response shape.

   If any gate is still open, keep the release language at development/public-preview stability and list the gap in the GitHub release notes.

9. Select the release version and date, then update [CHANGELOG.md](../CHANGELOG.md) and versioned manifest examples.
10. After release review, create and push the selected tag:

   ```bash
   git tag "$RELEASE_VERSION"
   git push origin "$RELEASE_VERSION"
   ```

11. Create a GitHub release with:

   - Collector Builder manifest snippet.
   - Supported Collector version.
   - Supported endpoint modes, API flavors, and auth paths.
   - Validation status, including any incomplete release validation gates.

## Release Checklist

- `make verify` passes.
- `go test -race ./...` passes.
- Collector Builder build succeeds.
- Checked-in examples validate.
- 2xx JSONv2 response bodies with per-record failure statuses are handled safely and covered by tests.
- Changelog has date and version.
- README Collector Builder snippet uses the release tag.
- `docs/compatibility.md` is current.
- Supported Collector and Go versions are explicit.
- Security reporting and maintainer/support expectations are explicit.
- Production/stable releases have customer-like instance and batch-size validation evidence, plus direct mTLS evidence if that path is advertised as validated.
- Any breaking config changes are highlighted.

## Versioning

- Patch releases: bug fixes, docs, tests, compatibility updates.
- Minor releases before `v1.0.0`: new config or behavior, possible breaking changes with explicit changelog notes.
- `v1.0.0`: only after customer-like instance validation, batch-size validation, and sustained external usage.
