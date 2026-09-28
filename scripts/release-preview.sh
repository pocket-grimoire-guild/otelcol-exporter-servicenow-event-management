#!/usr/bin/env bash
set -euo pipefail

readonly EXPECTED_REPO='pocket-grimoire-guild/otelcol-exporter-servicenow-event-management'
readonly EXPECTED_TAG='v0.2.0'
readonly EXPECTED_TITLE='v0.2.0 — development/public preview'

fail() {
  printf 'release-preview: %s\n' "$*" >&2
  exit 1
}

[[ "${GH_REPO:-}" == "$EXPECTED_REPO" ]] || fail "GH_REPO must be $EXPECTED_REPO"
[[ "${RELEASE_TAG:-}" == "$EXPECTED_TAG" ]] || fail "RELEASE_TAG must be $EXPECTED_TAG"
[[ -n "${RELEASE_SHA:-}" ]] || fail 'RELEASE_SHA is required'
[[ -n "${GH_TOKEN:-}" ]] || fail 'GH_TOKEN is required'

command -v git >/dev/null || fail 'git is required'
command -v gh >/dev/null || fail 'gh is required'
command -v jq >/dev/null || fail 'jq is required'

origin_url="$(git config --get remote.origin.url || true)"
case "$origin_url" in
  "https://github.com/$EXPECTED_REPO"|"https://github.com/$EXPECTED_REPO.git") ;;
  *) fail 'origin must be the public HTTPS repository for the approved project' ;;
esac

head_commit="$(git rev-parse --verify 'HEAD^{commit}')" || fail 'HEAD is not a commit'
local_tag_commit="$(git rev-parse --verify "refs/tags/${EXPECTED_TAG}^{commit}")" || fail "local tag $EXPECTED_TAG is missing or invalid"
[[ "$head_commit" == "$RELEASE_SHA" ]] || fail 'checked-out commit does not match RELEASE_SHA'
[[ "$local_tag_commit" == "$RELEASE_SHA" ]] || fail 'local tag does not peel to RELEASE_SHA'
[[ -z "$(git status --porcelain)" ]] || fail 'working tree must be clean'

check_remote_tag() {
  local refs object ref direct_sha='' peeled_sha='' remote_commit
  refs="$(git ls-remote --tags origin "refs/tags/${EXPECTED_TAG}" "refs/tags/${EXPECTED_TAG}^{}")" || fail 'could not read the public remote tag'
  while IFS=$'\t' read -r object ref; do
    case "$ref" in
      "refs/tags/$EXPECTED_TAG") direct_sha="$object" ;;
      "refs/tags/${EXPECTED_TAG}^{}") peeled_sha="$object" ;;
    esac
  done <<< "$refs"
  [[ -n "$direct_sha" ]] || fail "public remote tag $EXPECTED_TAG is missing"
  remote_commit="${peeled_sha:-$direct_sha}"
  [[ "$remote_commit" == "$RELEASE_SHA" ]] || fail 'public remote tag does not peel to RELEASE_SHA'
}

check_remote_tag

git fetch --no-tags origin '+refs/heads/main:refs/remotes/origin/main' || fail 'could not fetch origin/main'
git merge-base --is-ancestor "$RELEASE_SHA" refs/remotes/origin/main || fail 'tagged commit is not merged into origin/main'

readonly NOTES_FILE='docs/releases/v0.2.0.md'
[[ -s "$NOTES_FILE" ]] || fail "maintained notes file $NOTES_FILE is missing or empty"
grep -q '[^[:space:]]' "$NOTES_FILE" || fail 'maintained notes contain no non-whitespace content'
if grep -Eq '^Source commit: ' "$NOTES_FILE"; then
  fail 'maintained notes must not contain a Source commit marker'
fi

releases="$(gh api --paginate --slurp "/repos/${EXPECTED_REPO}/releases?per_page=100")" || fail 'could not list releases; refusing to publish'
if ! jq -e 'type == "array" and length > 0 and all(.[]; type == "array") and all(.[][]; type == "object" and (.tag_name | type == "string"))' <<< "$releases" >/dev/null; then
  fail 'release list response has an unexpected shape; refusing to publish'
fi
matching_releases="$(jq -c --arg tag "$EXPECTED_TAG" '[.[][] | select(.tag_name == $tag)]' <<< "$releases")" || fail 'could not parse the release list'
matching_count="$(jq -r 'length' <<< "$matching_releases")" || fail 'could not count matching releases'

case "$matching_count" in
  0)
    ;;
  1)
    if jq -e --arg tag "$EXPECTED_TAG" --arg marker "Source commit: $RELEASE_SHA" \
      '.[0].tag_name == $tag and .[0].draft == false and .[0].prerelease == true and (((.[0].body // "") | split("\n") | map(select(startswith("Source commit:"))) == [$marker]))' \
      <<< "$matching_releases" >/dev/null; then
      printf 'release-preview: matching preview already exists for %s at %s\n' "$EXPECTED_TAG" "$RELEASE_SHA"
      exit 0
    fi
    fail 'a release for this tag exists but is not the matching published preview; refusing to edit it'
    ;;
  *)
    fail 'multiple releases exist for this tag; refusing to publish'
    ;;
esac

notes_copy="$(mktemp)"
trap 'rm -f "$notes_copy"' EXIT
cat "$NOTES_FILE" > "$notes_copy"
printf '\nSource commit: %s\n' "$RELEASE_SHA" >> "$notes_copy"

check_remote_tag
gh release create "$EXPECTED_TAG" \
  --repo "$EXPECTED_REPO" \
  --verify-tag \
  --prerelease \
  --latest=false \
  --title "$EXPECTED_TITLE" \
  --notes-file "$notes_copy"
