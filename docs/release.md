# Release Process

This repository has a published `v0.1.0` baseline. The prepared `v0.2.0` release is a development/public preview, not a production-ready or stable release. Its exact notes are maintained in [docs/releases/v0.2.0.md](releases/v0.2.0.md).

## Prepare v0.2.0

Before tagging, keep the versioned release notes, `CHANGELOG.md`, README, compatibility matrix, security policy, registry metadata, and all Collector Builder manifests consistent at `v0.2.0`. Keep `go.mod` at its declared Go `1.25.0` minimum; CI and the release workflow select Go `1.25.7` because the metrics distribution hit an OTTL linker issue with `1.25.0`. Collector component and tool modules remain `v0.153.0`, and stable modules remain `v1.59.0`.

The existing `ci` workflow installs the pinned validation tools and runs `make verify`. The tag workflow calls that same workflow from the tagged commit. This gate uses local tests and fakes; it does not repeat historical PDI or MID validation. The dated ServiceNow evidence and remaining gaps are listed in [docs/compatibility.md](compatibility.md) and called out in the release notes.

## Publish the v0.2.0 Preview

1. Open a protected pull request with the complete release preparation. Wait for its required reviews and checks, then merge it to `main`. The release workflow must be present on the merged commit.
2. After the merge, choose the validated commit on `main` and create and push the exact `v0.2.0` tag at that commit. Do not create a GitHub release manually; pushing the tag starts `.github/workflows/release.yml`.
3. The workflow runs the reusable `ci` workflow first. Its publish job runs only after `make verify` succeeds. The job checks that the repository is the approved public repository, the checked-out commit and local and remote tag all resolve to the same SHA, that the tagged commit is an ancestor of `origin/main`, and that the maintained release notes are present.
4. The publish job uses the workflow's `GITHUB_TOKEN` with `contents: write` only for the release operation. Checkout credentials are not persisted. The workflow creates a prerelease titled `v0.2.0 — development/public preview`, marks it as not the latest release, and appends the full source commit SHA to a temporary copy of the maintained notes. It does not move or delete tags, edit releases, or use a personal access token.
5. Confirm the Actions run and published release before announcing the Go module version to consumers. The README's tagged builder manifest is usable once `v0.2.0` is available from the public Go module proxy.

The workflow is intentionally limited to `v0.2.0`. It treats one existing non-draft prerelease as complete only when its tag and exact `Source commit: <SHA>` body line match the pushed commit. A draft, full release, mismatched SHA, duplicate release, API-listing failure, or any failed guard stops publication. If verification, permissions, or a guard fails, report the failed run and resolve it through a reviewed release plan; do not bypass the gate, publish manually, or move/delete the tag.

## Preview Scope

The preview supports the logs signal only. Metrics-derived and selected trace-exception recipes convert chosen upstream telemetry to logs; the optional log-context overlay is also upstream processing. None adds native metrics or traces export or defines alert recovery/lifecycle behavior. Historical PDI evidence does not establish current customer-like instance behavior, alert lifecycle, or production readiness. Customer-like non-PDI validation, tested maximum batch/request sizes, and direct instance mTLS remain open.

For future releases, update the version-specific workflow guard, manifest pins, and maintained release notes in a reviewed pull request before tagging. Keep the Go module minimum distinct from the selected build patch, preserve dated validation evidence, and document compatibility or config changes in the changelog.
