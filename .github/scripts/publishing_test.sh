#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
workflow="$repo_root/.github/workflows/ci.yml"
version_script=$(yq '.jobs.helm-publish.steps[] | select(.name == "Get version") | .run' "$workflow")
package_script=$(yq '.jobs.helm-publish.steps[] | select(.name == "Package Helm chart") | .run' "$workflow")
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

assert_equal() {
  if [[ "$1" != "$2" ]]; then
    printf 'Expected <%s>, got <%s>\n' "$1" "$2" >&2
    exit 1
  fi
}

while read -r ref_name commit_sha expected_version expected_image expected_policy expected_app; do
  mkdir -p "$temp_dir/charts"
  cp -R "$repo_root/charts/kube-state-logs" "$temp_dir/charts/"
  cd "$temp_dir"
  export GITHUB_REF_NAME="$ref_name" GITHUB_SHA="$commit_sha" GITHUB_OUTPUT="$temp_dir/output"
  export GITHUB_REF="refs/heads/$ref_name" BRANCH_CHART_VERSION='' BRANCH_IMAGE_TAG=''
  if [[ "$ref_name" == v* ]]; then
    GITHUB_REF="refs/tags/$ref_name"
  elif [[ "$ref_name" != main ]]; then
    BRANCH_CHART_VERSION=0.0.0-branch-feature-foo
    BRANCH_IMAGE_TAG=branch-feature-foo
  fi
  bash <<< "$version_script"
  export CHART_VERSION
  CHART_VERSION=$(sed -n 's/^version=//p' "$GITHUB_OUTPUT" | tail -n 1)
  assert_equal "$expected_version" "$CHART_VERSION"
  bash <<< "$package_script"
  archive="kube-state-logs-$CHART_VERSION.tgz"
  assert_equal "$expected_version" "$(helm show chart "$archive" | yq '.version')"
  assert_equal "$expected_app" "$(helm show chart "$archive" | yq '.appVersion')"
  assert_equal "$expected_image" "$(helm show values "$archive" | yq '.image.tag')"
  assert_equal "$expected_policy" "$(helm show values "$archive" | yq '.image.pullPolicy')"
  helm lint charts/kube-state-logs
  rendered=$(helm template kube-state-logs "$archive")
  assert_equal "ghcr.io/azure/kube-state-logs:$expected_image" "$(yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].image' <<< "$rendered")"
  assert_equal "$expected_policy" "$(yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].imagePullPolicy' <<< "$rendered")"
  printf 'Passed packaging: %s (%s)\n' "$ref_name" "$commit_sha"
done <<'CASES'
feature/foo aaaaaaa 0.0.0-branch-feature-foo branch-feature-foo Always sha-aaaaaaa
feature/foo bbbbbbb 0.0.0-branch-feature-foo branch-feature-foo Always sha-bbbbbbb
main aaaaaaa 0.0.0-main.aaaaaaa latest IfNotPresent 0.0.0-main.aaaaaaa
v1.2.3 aaaaaaa 1.2.3 v1.2.3 IfNotPresent 1.2.3
v1.2.3-beta.1 aaaaaaa 1.2.3-beta.1 v1.2.3-beta.1 IfNotPresent 1.2.3-beta.1
CASES