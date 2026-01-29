# AGENTS.md - AI Agent Guidelines

This document provides guidance for AI agents working with the kube-state-logs codebase.

## Project Overview

**kube-state-logs** is a Kubernetes cluster state logger that outputs structured JSON logs. It's inspired by [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) but produces logs instead of Prometheus metrics.

- **Language**: Go 1.25+
- **Primary Dependencies**: k8s.io/client-go, k8s.io/api, k8s.io/apimachinery
- **Deployment**: Helm chart in `charts/kube-state-logs/`

## Project Structure

```
├── main.go                     # Application entrypoint
├── pkg/
│   ├── collector/
│   │   ├── collector.go        # Main collector orchestration
│   │   ├── logger.go           # Logging utilities
│   │   └── resources/          # Resource handlers (one per K8s resource type)
│   │       ├── deployment.go
│   │       ├── pod.go
│   │       ├── node.go
│   │       └── ...
│   ├── config/
│   │   └── config.go           # Configuration parsing and validation
│   ├── interfaces/
│   │   └── logger.go           # Interface definitions
│   ├── types/
│   │   └── types.go            # Data structures for log entries
│   └── utils/                  # Shared utilities
│       ├── handler_boilerplate.go
│       ├── informer_utils.go
│       ├── k8s_field_extraction.go
│       └── ...
├── charts/kube-state-logs/     # Helm chart for deployment
└── docs/                       # Additional documentation
```

## Code Patterns & Conventions

### Adding a New Resource Handler

Each Kubernetes resource type has a dedicated handler in `pkg/collector/resources/`. Follow this pattern:

1. **Create the handler file** (e.g., `myresource.go`):
   - Define a `MyResourceHandler` struct embedding `utils.BaseHandler`
   - Implement `NewMyResourceHandler(client kubernetes.Interface)` constructor
   - Implement `SetupInformer()` to configure the Kubernetes informer
   - Implement `Collect()` to gather and format resource data

2. **Create the data type** in `pkg/types/types.go`:
   - Define `MyResourceData` struct with appropriate metadata embedding
   - Use JSON tags with PascalCase field names (e.g., `json:"FieldName"`)

3. **Create tests** in `myresource_test.go`:
   - Use table-driven tests
   - Test informer setup and data collection logic

### Handler Interface

All resource handlers must implement the `interfaces.ResourceHandler` interface:

```go
type ResourceHandler interface {
    SetupInformer(factory informers.SharedInformerFactory, logger Logger, resyncPeriod time.Duration) error
    Collect(ctx context.Context, namespaces []string) ([]any, error)
}
```

### Data Types Pattern

Log entry types follow a composition pattern using embedded structs:

- `BaseMetadata` - Core fields (Timestamp, ResourceType, Name, CreatedTimestamp)
- `NamespacedMetadata` - Adds Namespace field
- `LabeledMetadata` - Adds Labels and Annotations maps
- `NamespacedLabeledMetadata` - Combines namespaced + labeled
- `ControllerCreatedResourceMetadata` - Adds CreatedByKind/CreatedByName

### Utility Functions

Common utilities in `pkg/utils/`:

- `utils.NewBaseHandler()` - Base handler constructor
- `utils.SafeGetStoreList()` - Safe informer cache access
- `utils.ShouldIncludeNamespace()` - Namespace filtering
- `utils.ConvertConditionStatus()` - Convert K8s condition status to boolean pointer

## Testing

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run specific package tests
go test -v ./pkg/collector/resources/...

# Run specific test
go test -v -run TestDeploymentHandler ./pkg/collector/resources/
```

### Test Patterns

- Use table-driven tests with `[]struct{ name string; ... }` slices
- Mock Kubernetes clients using fake clientsets from `k8s.io/client-go/kubernetes/fake`
- **Always run `go test ./...` and `gofmt -l .` before submitting changes** - CI will fail if tests don't pass or code isn't formatted

## Building & Development

### Docker Image & FIPS Compliance

The Dockerfile uses **Azure Linux** with **CGO enabled** for FIPS (Federal Information Processing Standards) compliance:

- **Build stage**: `mcr.microsoft.com/oss/go/microsoft/golang:1.25-fips-azurelinux3.0`
- **Runtime stage**: `mcr.microsoft.com/azurelinux/base/core:3.0`
- **CGO**: Enabled (`CGO_ENABLED=1`) to use FIPS-validated crypto libraries

### Updating Go Version

When updating the Go version, the following files must be changed:

1. **`go.mod`** - Update the `go` directive (e.g., `go 1.25.0`)
2. **`Dockerfile`** - Update the builder image tag (e.g., `golang:1.25-fips-azurelinux3.0`)
3. **`.github/workflows/ci.yml`** - Update `go-version` in both `fmt` and `test` jobs
4. **`AGENTS.md`** - Update the documented Go version in Project Overview

Example version update:
```bash
# go.mod
go 1.26.0

# Dockerfile
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.26-fips-azurelinux3.0 AS builder

# .github/workflows/ci.yml (in both fmt and test jobs)
go-version: '1.26'
```

### Updating Kubernetes API Version

The Kubernetes client libraries must be updated together as they share version numbers. Use `go get` commands rather than editing `go.mod` directly:

```bash
# Update all k8s.io dependencies to a new version (e.g., v0.34.0)
go get k8s.io/api@v0.34.0 k8s.io/apimachinery@v0.34.0 k8s.io/client-go@v0.34.0 k8s.io/metrics@v0.34.0

# Clean up and update go.sum
go mod tidy
```

**Important notes:**
- All `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, and `k8s.io/metrics` should use the same version
- `k8s.io/klog/v2` and `k8s.io/utils` are versioned independently and usually don't need updating
- After updating, run tests to ensure compatibility: `go test ./...`
- Check the [Kubernetes client-go compatibility matrix](https://github.com/kubernetes/client-go#compatibility-matrix) for supported Kubernetes versions

### Local Build

```bash
# Build Docker image
make build

# Build for multiple platforms
make build-multi

# Push to registry
make push
```

### Environment Variables

- `IMAGE_NAME` - Docker image name (default: `kube-state-logs`)
- `IMAGE_TAG` - Docker image tag (default: `latest`)
- `REGISTRY` - Container registry (default: `ghcr.io/azure/`)

## Configuration

Key configuration options (set via Helm values or environment):

- `deploymentMode` - Deployment architecture: `simple` (single Deployment) or `advanced` (DaemonSet + Deployment)
- `logInterval` - How often to log resource state (default: `1m`)
- `logLevel` - Log verbosity: debug, info, warn, error
- `namespaces` - Comma-separated namespaces to monitor (empty = all)
- `resources` - Which resource types to monitor

### Command-Line Flags for Node Filtering

- `--node=<name>` - Filter pods to only those scheduled on this node (used by DaemonSet in advanced mode)
- `--track-unscheduled-pods` - Only collect pods not yet scheduled to a node (used by Deployment in advanced mode)

## Common Tasks

### Adding Support for a New Kubernetes Resource

1. Create handler in `pkg/collector/resources/newresource.go`
2. Define data type in `pkg/types/types.go`
3. Register handler in `pkg/collector/collector.go` by adding to the `handlers` map in `registerHandlers()`:
   ```go
   handlers := map[string]interfaces.ResourceHandler{
       // ... existing handlers ...
       "newresource": resources.NewNewResourceHandler(c.client),
   }
   ```
4. Add tests in `pkg/collector/resources/newresource_test.go`
5. Update Helm chart RBAC in `charts/kube-state-logs/templates/rbac.yaml` if new permissions needed (update both simple and advanced mode sections)
6. **For CamelCase resources** (e.g., `PodDisruptionBudget`, `NetworkPolicy`): Add a mapping in the `resourceSnapshotName` helper in `charts/kube-state-logs/templates/_helpers.tpl` to convert the lowercase resource name to the proper PascalCase snapshot name (e.g., `poddisruptionbudget` → `PodDisruptionBudgetSnapshot`)
7. Document the new resource in `docs/resources.md` under the appropriate category
8. If the resource should be enabled by default, add it to `config.resources` in `charts/kube-state-logs/values.yaml` and update docs accordingly.

### Working with Deployment Modes

The Helm chart supports two deployment modes controlled by `deploymentMode`:

- **simple**: Single Deployment with all resources (default)
- **advanced**: DaemonSet for pod/container + Deployment for other resources

When modifying Helm templates:
- `templates/deployment.yaml` - Handles both simple mode (full deployment) and advanced mode (cluster deployment)
- `templates/daemonset.yaml` - Only rendered in advanced mode, handles pod/container with node filtering
- `templates/rbac.yaml` - Contains conditional RBAC for both modes (simple uses single set, advanced uses separate -node and -cluster RBAC)
- `templates/_helpers.tpl` - Contains helpers for resource filtering (`clusterResources`) and ADX-Mon annotations (`nodeLogKeysAnnotation`, `clusterLogKeysAnnotation`)

### Modifying Log Output Format

1. Update the relevant struct in `pkg/types/types.go`
2. Modify `createLogEntry()` in the resource handler
3. Update corresponding tests

### Adding New Configuration Options

1. Add field to `Config` struct in `pkg/config/config.go`
2. Add parsing logic in config.go
3. Update Helm chart `values.yaml` and templates
4. Update documentation

## Important Notes

- **Code Style**: Write idiomatic Go code and ensure all code is properly formatted with `gofmt`
- **JSON Field Names**: Use PascalCase in JSON tags to match existing output format
- **Informers**: Always use informer cache via `SafeGetStoreList()`, not direct API calls
- **Namespace Filtering**: Use `utils.ShouldIncludeNamespace()` for consistent behavior
- **Error Handling**: Log errors with context using `klog` package
- **Nil Checks**: Kubernetes API objects often have nil pointers; always check before dereferencing

## CI/CD Pipeline

The project uses GitHub Actions (`.github/workflows/ci.yml`) for continuous integration:

- **On Pull Requests**: Runs `gofmt` check, unit tests, and Helm lint
- **On Push to `main`**: Builds and publishes Docker images and Helm charts to GHCR
- **On Version Tags (`v*`)**: Creates versioned releases with proper semver tags

See [docs/releasing.md](docs/releasing.md) for full versioning strategy and release process.

## Related Documentation

- [README.md](README.md) - Project overview and installation
- [docs/adx-mon-integration.md](docs/adx-mon-integration.md) - ADX-Mon log collection setup
- [docs/releasing.md](docs/releasing.md) - Release process
- [SUPPORT.md](SUPPORT.md) - Support information
- [SECURITY.md](SECURITY.md) - Security policy
