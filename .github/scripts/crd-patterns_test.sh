#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
chart="$repo_root/charts/kube-state-logs"
expected='--crd-configs=example.com/v1:widgets:spec:**.secret\*|**.\token|**.secret"key'

for mode in simple advanced; do
  rendered=$(helm template crd-patterns "$chart" --set "deploymentMode=$mode" -f - <<'YAML'
config:
  resources:
    - crd
  crdConfigs:
    - apiVersion: example.com/v1
      resource: widgets
      customFields:
        - spec
      dropFields:
        - '**.secret\*'
        - '**.\token'
        - '**.secret"key'
YAML
  )
  actual=$(yq -rN 'select(.kind == "Deployment") | .spec.template.spec.containers[].args[] | select(test("^--crd-configs="))' <<< "$rendered")
  if [[ "$actual" != "$expected" ]]; then
    printf 'Unexpected CRD argument in %s mode: expected <%s>, got <%s>\n' "$mode" "$expected" "$actual" >&2
    exit 1
  fi
done

printf 'CRD pattern escaping tests passed\n'
