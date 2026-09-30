#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$#" -ne 1 || "$1" != "metadata.yaml" ]]; then
  printf 'usage: %s metadata.yaml\n' "${BASH_SOURCE[0]}" >&2
  exit 2
fi

"${repo_root}/scripts/check-collector-tool-version.sh" mdatagen go.opentelemetry.io/collector/cmd/mdatagen
cd "${repo_root}"
# mdatagen derives generated Go package names from the metadata directory basename.
# The checkout directory can be hyphenated, so use a temporary valid alias for generation only;
# it is removed before the wrapper exits and is not a compatibility package.
package_alias='servicenoweventmanagementexporter'
if [[ -e "${package_alias}" || -L "${package_alias}" ]]; then
  printf 'temporary mdatagen package alias already exists: %s\n' "${package_alias}" >&2
  exit 1
fi
ln -s . "${package_alias}"
cleanup_package_alias() {
  rm -f -- "${package_alias}"
}
trap cleanup_package_alias EXIT
mdatagen "${package_alias}/metadata.yaml"
cleanup_package_alias
trap - EXIT
"${repo_root}/scripts/add-go-license-headers.sh"
"${repo_root}/scripts/generate-metadata.sh" normalize
