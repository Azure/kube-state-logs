# ADX-Mon Integration

This guide explains how to configure kube-state-logs to work with [ADX-Mon](https://github.com/Azure/adx-mon), a log collection and ingestion system for Azure Data Explorer (ADX).

## Overview

When ADX-Mon integration is enabled, kube-state-logs adds special annotations to its pods that tell ADX-Mon:
- Which logs to scrape
- How to parse them (JSON)
- Where to send them (your ADX database/table)
- How to route different resource types to different tables

## Enabling ADX-Mon Support

Add the following to your Helm values:

```yaml
config:
  enableADXMonAnnotations: true
  adxMonLogDestination: "your-database-name"
```

Or use `--set` flags with `helm install`:

```bash
helm install kube-state-logs ./charts/kube-state-logs \
  --namespace monitoring \
  --create-namespace \
  --set config.enableADXMonAnnotations=true \
  --set config.adxMonLogDestination="MyKubernetesDB"
```

### Full Example

```yaml
config:
  logInterval: "1m"
  logLevel: "info"
  
  # Enable ADX-Mon integration
  enableADXMonAnnotations: true
  adxMonLogDestination: "MyKubernetesDB"
  
  # Resources to monitor
  resources:
    - pod
    - deployment
    - node
    - service
```

## What This Does

When `enableADXMonAnnotations: true`, the Helm chart adds the following annotations to the kube-state-logs pod:

```yaml
annotations:
  adx-mon/scrape: "true"
  adx-mon/log-parsers: "json"
  adx-mon/log-destination: "<adxMonLogDestination>:KubeStateLogs"
  adx-mon/log-keys: "ResourceType:pod:<database>:KubePodSnapshot,ResourceType:deployment:<database>:KubeDeploymentSnapshot,..."
```

### Annotation Details

| Annotation                        | Purpose                                                      |
|-----------------------------------|--------------------------------------------------------------|
| `adx-mon/scrape`                  | Tells ADX-Mon to collect logs from this pod                  |
| `adx-mon/log-parsers`             | Specifies JSON parsing for structured logs                   |
| `adx-mon/log-destination`         | Default destination database and table                       |
| `adx-mon/log-keys`                | Routes logs to specific tables based on `ResourceType` field |

### Log Routing

The `log-keys` annotation enables ADX-Mon to route each resource type to its own table in ADX:

| Resource Type        | ADX Table Name                    |
|----------------------|-----------------------------------|
| pod                  | KubePodSnapshot                   |
| deployment           | KubeDeploymentSnapshot            |
| node                 | KubeNodeSnapshot                  |
| service              | KubeServiceSnapshot               |
| configmap            | KubeConfigMapSnapshot             |
| poddisruptionbudget  | KubePodDisruptionBudgetSnapshot   |
| ...                  | Kube{ResourceName}Snapshot        |

This allows you to query each resource type efficiently in ADX:

```kusto
// Query all pods
KubePodSnapshot
| take 5

// Query deployments with issues
KubeDeploymentSnapshot
| take 5
```

## CRD Support

If you're monitoring Custom Resource Definitions (CRDs), they are also included in the log-keys annotation:

```yaml
config:
  enableADXMonAnnotations: true
  adxMonLogDestination: "MyKubernetesDB"
  crdConfigs:
    - apiVersion: "msi-acrpull.microsoft.com/v1"
      resource: "acrpullbindings"
      kind: "AcrPullBinding"
      customFields:
        - "spec.acrServer"
        - "status.lastTokenRefreshTime"
```

This adds a routing entry like:
```
ResourceType:acrpullbinding:MyKubernetesDB:KubeAcrPullBindingSnapshot
```

## Prerequisites

- ADX-Mon's collector deployed in your cluster, correctly pointing to an ADX-Mon ingestor
- An Azure Data Explorer database configured as a destination

## Troubleshooting

### Verify Annotations

Check that the annotations are correctly applied:

```bash
kubectl get pods -l app.kubernetes.io/name=kube-state-logs -o jsonpath='{.items[0].metadata.annotations}' | jq
```

### Validate Log Format

Ensure logs are valid JSON:

```bash
kubectl logs -l app.kubernetes.io/name=kube-state-logs --tail=10
```

Each line should be a complete JSON object with a `ResourceType` field.

### Check ADX-Mon is Scraping

Verify ADX-Mon is collecting logs.
