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

## Configuration

Configure via Helm values:

```yaml
config:
  logInterval: "1m"           # How often to log resource state
  logLevel: "info"            # debug, info, warn, error
  namespaces: ""              # Empty = all namespaces
  enableEventLogging: false   # Emit immediate logs on resource creation/deletion
  resources:                  # Which resources to monitor
    - pod
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

### Event Logging

When `enableEventLogging` is set to `true`, kube-state-logs also emits an immediate log entry whenever a resource is created or deleted, without waiting for the next periodic interval. Each log entry includes an `EventType` field:

- `"snapshot"` — periodic interval log (the default behaviour)
- `"created"` — emitted immediately when a resource is first seen
- `"deleted"` — emitted immediately when a resource is removed; includes a `DeletionTimestamp` field

Events during the initial informer cache sync are suppressed to avoid flooding logs on startup.

## Example Output

A deployment logged as JSON (truncated for brevity):

```json
{
  "Timestamp": "2024-01-15T10:30:00Z",
  "ResourceType": "deployment",
  "EventType": "snapshot",
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
