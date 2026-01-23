# kube-state-logs

Logs Kubernetes cluster state as structured JSON. Inspired by [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics), but outputs logs instead of Prometheus metrics.

## Acknowledgment

This project is heavily inspired by [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics), the official Kubernetes project that exposes cluster state as Prometheus metrics. We aim to provide similar resource coverage and calculated metrics, but in a log-based format for environments that prefer structured logs over time-series metrics.

## AI-Assisted Development Notice

**Transparency Notice**: This project was primarily developed with the assistance of AI tools. While the core concepts, architecture decisions, and requirements were human-defined, the majority of the implementation code, documentation, and testing was generated with AI assistance. We believe in being transparent about this development approach and welcome contributions from both human developers and AI-assisted workflows.

## Installation

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
  resources:                  # Which resources to monitor
    - pod
    - deployment
    - node
    - service
```

See [charts/kube-state-logs/values.yaml](charts/kube-state-logs/values.yaml) for all options.

## How It Works

kube-state-logs watches Kubernetes resources and logs their current state as JSON at the configured interval. Each resource type gets one log line per object, per interval.

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
