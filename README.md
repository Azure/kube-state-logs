# kube-state-logs

Logs Kubernetes cluster state as structured JSON. Inspired by [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics), but outputs logs instead of Prometheus metrics.

## Acknowledgment

This project is heavily inspired by [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics), the official Kubernetes project that exposes cluster state as Prometheus metrics. We aim to provide similar resource coverage and calculated metrics, but in a log-based format for environments that prefer structured logs over time-series metrics.

## AI-Assisted Development Notice

**Transparency Notice**: This project was primarily developed with the assistance of AI tools. While the core concepts, architecture decisions, and requirements were human-defined, the majority of the implementation code, documentation, and testing was generated with AI assistance. We believe in being transparent about this development approach and welcome contributions from both human developers and AI-assisted workflows.

## Installation

Requires Helm 3.x (tested with v3.20.0). Helm 4 compatibility has not yet been validated.

```bash
helm install kube-state-logs oci://ghcr.io/azure/kube-state-logs/charts/kube-state-logs \
  --version 1.0.0 \
  --namespace monitoring \
  --create-namespace
```

### Installing from Source

If you prefer to install from a local checkout:

```bash
helm install kube-state-logs ./charts/kube-state-logs \
  --namespace monitoring \
  --create-namespace
```

## Deployment Modes

kube-state-logs supports two deployment modes, inspired by [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics):

### Simple Mode (Default)

A single Deployment monitors all configured resources. Best for smaller clusters or when simplicity is preferred.

```yaml
deploymentMode: simple  # This is the default
```

### Advanced Mode

Uses a DaemonSet for pod/container resources (one pod per node) and a separate Deployment for all other resources. Each DaemonSet pod only logs pods scheduled on its local node, reducing API server load on large clusters. The Deployment also tracks unscheduled pods.

```yaml
deploymentMode: advanced
```

**Advanced mode creates:**
- `<release>-node` DaemonSet: Logs pods/containers on the local node using `--node` flag
- `<release>-cluster` Deployment: Logs all other resources + unscheduled pods using `--track-unscheduled-pods` flag

**Separate RBAC:** Each component gets its own ServiceAccount and ClusterRole with minimal required permissions.

By default, node collectors use kubelet polling to avoid pod watches and source
container usage directly from each node. To use node-filtered Kubernetes
informers and metrics-server instead, set `daemonset.useKubeletAPI: false`.

```yaml
deploymentMode: advanced
daemonset:
  useKubeletAPI: true
  kubeletPort: 10250
  # Verification uses the projected service-account CA by default.
  kubeletInsecureSkipVerify: false
```

Kubelet mode reads `/pods` and `/stats/summary` with the DaemonSet pod's
rotating service-account token. It does not query metrics-server. Because this
is interval-based snapshot polling, a pod that starts and disappears entirely
between two polls may not be observed; reduce `config.logInterval` when that
tradeoff matters. Kubelet mode requires the `KubeletFineGrainedAuthz` feature,
which is enabled by default in Kubernetes 1.33 and later, so `/pods` can be
authorized through the least-privilege `nodes/pods` subresource. On older
clusters, or clusters where that feature is disabled, set
`daemonset.useKubeletAPI: false` to use informer mode. Set
`kubeletInsecureSkipVerify: true` only when kubelet serving certificates cannot
be verified, and only on trusted cluster networks.

Both chart workloads select Linux nodes by default because the published image
supports Linux only. Override `nodeSelector.kubernetes.io/os` when using a
custom image that supports another operating system.

Kubelet responses are limited to 10 MiB each to reduce buffering and decoding
memory pressure on node collectors. Larger responses are rejected; use
`daemonset.useKubeletAPI: false` on nodes that exceed this limit. The limit is
not a total memory bound: decoded objects and cached snapshots also consume
memory, so size the DaemonSet memory limit for the workload.

In advanced mode, scheduled pod and container snapshots are collected only on
nodes where the DaemonSet runs. A custom `nodeSelector` intentionally limits
that coverage; when pod collection is enabled, the cluster Deployment continues
to collect unscheduled pods.

**Separate resource limits:** DaemonSet pods use smaller defaults since they only track local pods:

```yaml
daemonset:
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 64Mi
```

## Configuration

Configure via Helm values:

```yaml
config:
  logInterval: "1m"             # How often to log resource state
  logLevel: "info"              # debug, info, warn, error
  namespaces: ""                # Empty = all namespaces
  configmapIncludeValues: false # ConfigMap data values
  resourceConfigs:              # Per-resource settings; interval is optional
    - "pod:promote-node-labels=topology.kubernetes.io/zone"
    - "container:promote-node-labels=topology.kubernetes.io/zone"
  resources:                    # Which resources to monitor
    - pod
    - container
    - deployment
    - node
    - service
```

See [charts/kube-state-logs/values.yaml](charts/kube-state-logs/values.yaml) for all options.

## Supported Resources

kube-state-logs can monitor 40+ Kubernetes resource types including pods, deployments, nodes, services, configmaps, RBAC resources, and more. Custom Resource Definitions (CRDs) are also supported.

See [docs/resources.md](docs/resources.md) for the complete list and configuration options.

## ADX-Mon Integration

If you're using [ADX-Mon](https://github.com/Azure/adx-mon) for log collection to Azure Data Explorer, see [docs/adx-mon-integration.md](docs/adx-mon-integration.md) for setup instructions.

We welcome contributions to add support for other log collection solutions (e.g., Fluentd, Vector, Loki, OpenTelemetry). If you'd like to add integration support for another system, please open an issue or pull request.

## How It Works

kube-state-logs watches Kubernetes resources and logs their current state as JSON at the configured interval. Each resource type gets one log line per object, per interval.

### Readiness

The HTTP endpoint `GET /readyz` on port `8080` returns `503 Service Unavailable`
until all enabled built-in informer caches have completed their initial
synchronization, including node-filtered/unscheduled pod caches and dependency
caches. It returns `200 OK` once collection loops start, and becomes unready on
shutdown. Built-in informer setup or cache-sync failures prevent readiness.

Both the Deployment and DaemonSet use this endpoint as a readiness probe.
Unknown built-in resource names are fatal configuration errors. Built-in
informer list/watch requests that return `404 Not Found`, `401 Unauthorized`,
or `403 Forbidden` terminate the collector with an error, including failures
in dependency caches (such as Endpoints for Services). Transient API failures
continue to retry.

Configured CRDs are not skipped when their APIs are missing at startup.
Their informers keep retrying with client-go backoff, including after
`404`, `401`, or `403` responses, so installing the CRD or fixing its RBAC
allows synchronization without restarting the collector. CRD caches do not
gate readiness: each CRD starts collecting after its own cache synchronizes,
without blocking built-in resources or other CRDs. A CRD-only collector becomes
ready even when none of its configured CRDs are available yet.
Readiness probe failures themselves do not restart the container.
This checks initial cache synchronization, not ongoing watch freshness.
Kubelet-only collectors have no informer caches to wait for, so they become
ready when their collection loops start; the probe does not check kubelet
polling success.

### Liveness

The HTTP endpoint `GET /livez` on port `8080` returns `200 OK` whenever the
health probe server can respond, including while caches are synchronizing or
the collector is shutting down. It does not check cache readiness, Kubernetes
API availability, CRD availability, or kubelet polling success, so those
conditions do not trigger liveness restarts. It checks HTTP responsiveness,
not progress of individual collection loops.

Both the Deployment and DaemonSet probe `/livez` every 10 seconds, with a
5-second timeout. Three consecutive failures trigger a container restart.
The probe server starts before informer synchronization, so slow cache
initialization does not require a startup probe.

## Example Output

A deployment logged as JSON (truncated for brevity):

```json
{
  "Timestamp": "2024-01-15T10:30:00Z",
  "ResourceType": "deployment",
  "Name": "my-app",
  "Namespace": "default",
  "CreatedTimestamp": "2024-01-10T08:00:00Z",
  "Labels": {"app": "my-app"},
  "Annotations": {"deployment.kubernetes.io/revision": "3"},
  "DesiredReplicas": 3,
  "CurrentReplicas": 3,
  "ReadyReplicas": 3,
  "AvailableReplicas": 3,
  "UnavailableReplicas": 0,
  "UpdatedReplicas": 3,
  "ObservedGeneration": 5,
  "StrategyType": "RollingUpdate",
  "ConditionAvailable": true,
  "ConditionProgressing": true,
  "Paused": false
}
```

## Building

```bash
make build
```

## License

[MIT](LICENSE)

## Support

See [SUPPORT.md](SUPPORT.md) for support information.

## Security

See [SECURITY.md](SECURITY.md) for security policy and reporting vulnerabilities.

## Code of Conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for our code of conduct.

## Releasing

See [docs/releasing.md](docs/releasing.md) for versioning strategy and release process.


## Trademarks
This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft trademarks or logos is subject to and must follow [Microsoft’s Trademark & Brand Guidelines](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks/usage/general). Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship. Any use of third-party trademarks or logos are subject to those third-party’s policies.
