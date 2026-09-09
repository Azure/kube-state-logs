#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/branch-metadata.sh"

assert_equal() {
  if [[ "$1" != "$2" ]]; then
    printf 'Expected <%s>, got <%s>\n' "$1" "$2" >&2
    exit 1
  fi
}

while IFS='|' read -r branch_name expected; do
  assert_equal "$expected" "$(normalize_branch_name "$branch_name")"
done <<'CASES'
feature/foo|feature-foo
Feature_FOO.v2|feature-foo-v2
--topic--|topic
123|123
v1.2.3|v1-2-3
latest|latest
___|
topic/$(false)|topic-false
CASES
assert_equal '' "$(normalize_branch_name $'\xc3\xa9')"

git() {
  assert_equal 'ls-remote --heads origin' "$*"
  [[ "${REMOTE_FAILURE:-false}" != true ]] || return 1
  printf '%s\n' "$REMOTE_HEADS"
}

export GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/feature/foo GITHUB_REF_NAME=feature/foo
export GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
REMOTE_HEADS="$GITHUB_SHA refs/heads/feature/foo"
expected=$'image_tag=branch-feature-foo\nchart_version=0.0.0-branch-feature-foo'
assert_equal "$expected" "$(branch_metadata)"
assert_equal 'publish=true' "$(branch_metadata current)"
GITHUB_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
assert_equal "$expected" "$(branch_metadata)"
assert_equal 'publish=false' "$(branch_metadata current)"
REMOTE_HEADS=''
assert_equal 'publish=false' "$(branch_metadata current)"

for other_branch in feature-foo FEATURE/FOO feature_foo; do
  REMOTE_HEADS="$GITHUB_SHA refs/heads/$other_branch"
  if (branch_metadata) >/dev/null 2>&1; then
    printf 'Missed collision with %s\n' "$other_branch" >&2
    exit 1
  fi
done
REMOTE_HEADS="$GITHUB_SHA refs/heads/unrelated"
assert_equal "$expected" "$(branch_metadata)"
REMOTE_FAILURE=true
if (branch_metadata) >/dev/null 2>&1; then
  printf 'Remote failure must block publication\n' >&2
  exit 1
fi
REMOTE_FAILURE=false

for branch_name in ___ "$(printf 'a%.0s' {1..101})"; do
  GITHUB_REF_NAME="$branch_name"
  if (branch_metadata) >/dev/null 2>&1; then
    printf 'Accepted invalid branch %s\n' "$branch_name" >&2
    exit 1
  fi
done
GITHUB_REF_NAME="$(printf 'a%.0s' {1..100})"
assert_equal "image_tag=branch-$GITHUB_REF_NAME
chart_version=0.0.0-branch-$GITHUB_REF_NAME" "$(branch_metadata)"

for event_ref in 'push refs/heads/main' 'push refs/tags/v1.2.3' 'push refs/tags/v1.2.3-beta.1' 'pull_request refs/pull/12/merge'; do
  read -r GITHUB_EVENT_NAME GITHUB_REF <<< "$event_ref"
  REMOTE_FAILURE=true
  assert_equal '' "$(branch_metadata)"
done
printf 'Branch metadata tests passed\n'