#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

tmpdir=""
cleanup() {
  if [[ -n "${tmpdir}" ]]; then
    rm -rf "${tmpdir}"
  fi
}
trap cleanup EXIT

failures=0

log() {
  printf '==> %s\n' "$*"
}

skip() {
  printf 'skip: %s\n' "$*"
}

record_failure() {
  printf '%s\n' "$*" >&2
  failures=$((failures + 1))
}

is_external_link() {
  local target="$1"
  case "${target}" in
    http://*|https://*|mailto:*|app://*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

trim_markdown_target() {
  local target="$1"
  target="${target#<}"
  target="${target%>}"
  target="${target%%#*}"
  target="${target%%\?*}"
  printf '%s\n' "${target}"
}

check_markdown_links() {
  log "check markdown links"

  local file dir entry line_no match target path resolved
  while IFS= read -r file; do
    dir="$(dirname "${file}")"
    while IFS= read -r entry; do
      line_no="${entry%%:*}"
      match="${entry#*:}"
      target="${match#*](}"
      target="${target%)}"

      if [[ "${target}" == \#* ]] || is_external_link "${target}"; then
        continue
      fi

      path="$(trim_markdown_target "${target}")"
      if [[ -z "${path}" ]]; then
        continue
      fi

      if [[ "${path}" == /* ]]; then
        resolved="${path}"
      else
        resolved="${dir}/${path}"
      fi

      if [[ ! -e "${resolved}" ]]; then
        record_failure "${file}:${line_no}: broken local markdown link: ${target}"
      fi
    done < <(grep -n -o -E '\[[^][]+\]\([^)]+\)' "${file}" || true)
  done < <(git ls-files '*.md')
}

check_yaml_fences() {
  if ! command -v yq >/dev/null 2>&1; then
    skip "yq not installed; skipping markdown YAML fence validation"
    return
  fi

  log "validate markdown YAML fences"
  tmpdir="$(mktemp -d)"

  local file base line line_no fence info lang block_file block_start in_yaml
  printf -v fence '\140\140\140'
  while IFS= read -r file; do
    base="$(basename "${file}")"
    in_yaml=false
    block_start=0
    block_file=""
    line_no=0

    while IFS= read -r line || [[ -n "${line}" ]]; do
      line_no=$((line_no + 1))
      if [[ "${in_yaml}" == "true" ]]; then
        if [[ "${line}" == "${fence}"* ]]; then
          if ! yq . "${block_file}" >/dev/null; then
            record_failure "${file}:${block_start}: invalid YAML fenced block"
          fi
          in_yaml=false
          block_file=""
          block_start=0
        else
          printf '%s\n' "${line}" >>"${block_file}"
        fi
        continue
      fi

      if [[ "${line}" == "${fence}"* ]]; then
        info="${line:3}"
        read -r lang _ <<<"${info}"
        case "${lang}" in
          yaml|yml)
            in_yaml=true
            block_start="${line_no}"
            block_file="${tmpdir}/${base}.${line_no}.yaml"
            : >"${block_file}"
            ;;
        esac
      fi
    done <"${file}"

    if [[ "${in_yaml}" == "true" ]]; then
      record_failure "${file}:${block_start}: unterminated YAML fenced block"
    fi
  done < <(git ls-files '*.md')
}

check_markdown_links
check_yaml_fences

if (( failures > 0 )); then
  printf 'documentation checks failed with %d issue(s)\n' "${failures}" >&2
  exit 1
fi

log "documentation checks complete"
