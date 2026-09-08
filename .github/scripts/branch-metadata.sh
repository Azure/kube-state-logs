#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

normalize_branch_name() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-//; s/-$//'
}

validate_unique_branch_slug() {
  local branch_name="$1" slug="$2" remote_heads="$3"
  local remote_sha remote_ref other_branch
  while read -r remote_sha remote_ref; do
    [[ "$remote_ref" == refs/heads/* ]] || continue
    other_branch="${remote_ref#refs/heads/}"
    [[ "$other_branch" != "$branch_name" && "$other_branch" != main ]] || continue
    if [[ "$(normalize_branch_name "$other_branch")" == "$slug" ]]; then
      printf "Error: branches '%s' and '%s' share tag 'branch-%s'; rename one branch.\n" "$branch_name" "$other_branch" "$slug" >&2
      return 1
    fi
  done <<< "$remote_heads"
}

branch_metadata() {
  local mode="${1:-metadata}" slug remote_heads remote_sha remote_ref
  if [[ "$GITHUB_EVENT_NAME" != push || "$GITHUB_REF" != refs/heads/* || "$GITHUB_REF" == refs/heads/main ]]; then
    [[ "$mode" != current ]] || printf 'publish=true\n'
    return 0
  fi

  slug=$(normalize_branch_name "$GITHUB_REF_NAME")
  if [[ -z "$slug" || ${#slug} -gt 100 ]]; then
    printf "Error: branch '%s' must normalize to 1-100 ASCII letters, digits or hyphens; rename the branch.\n" "$GITHUB_REF_NAME" >&2
    return 1
  fi
  remote_heads=$(git ls-remote --heads origin) || return 1
  validate_unique_branch_slug "$GITHUB_REF_NAME" "$slug" "$remote_heads" || return 1

  case "$mode" in
    metadata)
      printf 'image_tag=branch-%s\nchart_version=0.0.0-branch-%s\n' "$slug" "$slug"
      ;;
    current)
      while read -r remote_sha remote_ref; do
        if [[ "$remote_ref" == "$GITHUB_REF" && "$remote_sha" == "$GITHUB_SHA" ]]; then
          printf 'publish=true\n'
          return 0
        fi
      done <<< "$remote_heads"
      printf 'Skipping publication: branch was deleted or this commit is no longer its head.\n' >&2
      printf 'publish=false\n'
      ;;
    *)
      printf 'Unknown mode: %s\n' "$mode" >&2
      return 1
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  branch_metadata "$@"
fi