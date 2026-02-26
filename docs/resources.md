# Supported Resources

This page lists all Kubernetes resources that kube-state-logs can monitor.

## Available Resources

The following resources are available for monitoring. See [Default Resources](#default-resources) for which are enabled by default.

### Workloads

| Resource                 | Config Name                | Description                           |
|--------------------------|----------------------------|---------------------------------------|
| Pod                      | `pod`                      | Individual pod state and status       |
| Container                | `container`                | Container-level metrics within pods   |
| Deployment               | `deployment`               | Deployment state and replica counts   |
| StatefulSet              | `statefulset`              | StatefulSet state and replica counts  |
| DaemonSet                | `daemonset`                | DaemonSet state and scheduling        |
| ReplicaSet               | `replicaset`               | ReplicaSet state and replica counts   |
| ReplicationController    | `replicationcontroller`    | Legacy replication controller state   |
| Job                      | `job`                      | Job completion and status             |
| CronJob                  | `cronjob`                  | Scheduled job configuration           |

### Networking

| Resource                 | Config Name                | Description                           |
|--------------------------|----------------------------|---------------------------------------|
| Service                  | `service`                  | Service endpoints and configuration   |
| Endpoints                | `endpoints`                | Service endpoint addresses            |
| EndpointSlice            | `endpointslice`            | Scalable service endpoint slices      |
| Ingress                  | `ingress`                  | Ingress rules and backends            |
| IngressClass             | `ingressclass`             | Ingress controller configuration      |
| NetworkPolicy            | `networkpolicy`            | Network access policies               |

### Configuration & Storage

| Resource                 | Config Name                | Description                           |
|--------------------------|----------------------------|---------------------------------------|
| ConfigMap                | `configmap`                | Configuration data                    |
| Secret                   | `secret`                   | Secret metadata (not values)          |
| PersistentVolume         | `persistentvolume`         | Cluster storage volumes               |
| PersistentVolumeClaim    | `persistentvolumeclaim`    | Volume claims by pods                 |
| StorageClass             | `storageclass`             | Storage provisioner configuration     |
| VolumeAttachment         | `volumeattachment`         | Volume attachment to nodes            |
| LimitRange               | `limitrange`               | Resource limit defaults               |
| ResourceQuota            | `resourcequota`            | Namespace resource quotas             |

### RBAC & Security

| Resource                 | Config Name                | Description                           |
|--------------------------|----------------------------|---------------------------------------|
| ServiceAccount           | `serviceaccount`           | Service account configuration         |
| Role                     | `role`                     | Namespace-scoped permissions          |
| ClusterRole              | `clusterrole`              | Cluster-scoped permissions            |
| RoleBinding              | `rolebinding`              | Role to subject bindings              |
| ClusterRoleBinding       | `clusterrolebinding`       | ClusterRole to subject bindings       |

### Cluster Resources

| Resource                              | Config Name                          | Description                           |
|---------------------------------------|--------------------------------------|---------------------------------------|
| Node                                  | `node`                               | Node status and capacity              |
| Namespace                             | `namespace`                          | Namespace state and status            |
| Lease                                 | `lease`                              | Leader election and node heartbeats   |
| PriorityClass                         | `priorityclass`                      | Pod scheduling priority               |
| RuntimeClass                          | `runtimeclass`                       | Container runtime configuration       |
| CertificateSigningRequest             | `certificatesigningrequest`          | CSR state and approval                |
| PodDisruptionBudget                   | `poddisruptionbudget`                | Disruption budget status              |
| HorizontalPodAutoscaler               | `horizontalpodautoscaler`            | HPA state and scaling metrics         |

### Admission Control

| Resource                              | Config Name                          | Description                           |
|---------------------------------------|--------------------------------------|---------------------------------------|
| MutatingWebhookConfiguration          | `mutatingwebhookconfiguration`       | Mutating admission webhooks           |
| ValidatingWebhookConfiguration        | `validatingwebhookconfiguration`     | Validating admission webhooks         |
| ValidatingAdmissionPolicy             | `validatingadmissionpolicy`          | CEL-based admission policies          |
| ValidatingAdmissionPolicyBinding      | `validatingadmissionpolicybinding`   | Policy to resource bindings           |

### Custom Resources (CRDs)

In addition to built-in resources, kube-state-logs can monitor arbitrary Custom Resource Definitions. See [Configuration](#custom-resource-configuration) below.

## Configuration

### Default Resources

The following resources are enabled by default in the Helm chart:

- `pod`
- `container`
- `deployment`
- `job`
- `cronjob`
- `statefulset`
- `node`
- `namespace`
- `crd`
- `horizontalpodautoscaler`
- `replicaset`

To monitor additional resources, explicitly list all desired resources in your configuration.

### Selecting Resources

Specify which resources to monitor in your Helm values:

```yaml
config:
  resources:
    - pod
    - deployment
    - node
    - service
```

Or using `--set`:

```bash
helm install kube-state-logs ./charts/kube-state-logs \
  --set 'config.resources={pod,deployment,node,service}'
```

### Per-Resource Intervals

Set different logging intervals for specific resources:

```yaml
config:
  logInterval: "1m"        # Default interval
  resourceConfigs:
    - "deployment:30s"     # Log deployments every 30 seconds
    - "node:5m"            # Log nodes every 5 minutes
```

### Custom Resource Configuration

Monitor CRDs by specifying their API version, resource name, and fields to capture:

```yaml
config:
  crdConfigs:
    - apiVersion: "msi-acrpull.microsoft.com/v1"
      resource: "acrpullbindings"
      kind: "AcrPullBinding"
      customFields:
        - "spec.acrServer"
        - "spec.managedIdentityResourceID"
        - "status.lastTokenRefreshTime"
```

## Resource Notes

### Container Resource

The `container` resource provides container-level metrics and requires the Kubernetes Metrics Server to be installed for CPU/memory usage data. Configure which environment variables to capture:

```yaml
config:
  containerEnvVars:
    - "GOMAXPROCS"
    - "MY_APP_VERSION"
```

### Secret Resource

For security, the `secret` resource only logs metadata (name, namespace, type, labels, annotations) - **secret values are never logged**.

### Node Resource

The `node` resource includes capacity, allocatable resources, conditions, and addresses. When Metrics Server is available, it also includes actual CPU/memory usage.

## Event Logging

By default kube-state-logs only emits periodic snapshots. When **event logging** is enabled, an immediate log entry is also produced whenever a resource is created or deleted.

### Enabling Event Logging

```yaml
config:
  enableEventLogging: true
```

Or via the CLI flag:

```
--enable-event-logging
```

### EventType Field

Every log entry includes an `EventType` field:

| Value        | Meaning                                      |
|--------------|----------------------------------------------|
| `snapshot`   | Periodic interval log (default behaviour)    |
| `created`    | Emitted immediately on resource creation     |
| `deleted`    | Emitted immediately on resource deletion     |

### DeletionTimestamp

All log entries include an optional `DeletionTimestamp` field (omitted when empty). For `deleted` events this is populated with the Kubernetes deletion timestamp of the object if available.

### Sync Suppression

Events are suppressed until the initial informer cache sync completes. This prevents a flood of `created` events when the application starts and first populates its cache.
