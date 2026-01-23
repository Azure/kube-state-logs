package resources

import (
	"context"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	"k8s.io/utils/pointer"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// NodeHandler handles collection of node metrics
type NodeHandler struct {
	utils.BaseHandler
	metricsCache  cache.ThreadSafeStore
	metricsClient metricsclientset.Interface
}

// NewNodeHandler creates a new NodeHandler
func NewNodeHandler(client kubernetes.Interface, metricsClient metricsclientset.Interface) *NodeHandler {
	return &NodeHandler{
		BaseHandler:   utils.NewBaseHandler(client),
		metricsCache:  cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		metricsClient: metricsClient,
	}
}

// SetupInformer sets up the node informer
func (h *NodeHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create node informer
	informer := factory.Core().V1().Nodes().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers node metrics from the cluster (uses cache)
func (h *NodeHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Collect all node metrics upfront for efficiency
	h.collectAllNodeMetrics(ctx)

	// Get all nodes from the cache
	nodes := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range nodes {
		node, ok := obj.(*corev1.Node)
		if !ok {
			continue
		}

		entry := h.createLogEntry(node)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	// Clear metrics cache after collection
	h.metricsCache.Replace(make(map[string]any), "")

	return entries, nil
}

// createLogEntry creates a NodeData from a node
func (h *NodeHandler) createLogEntry(node *corev1.Node) types.NodeData {
	// Get node addresses
	var internalIP, externalIP, hostname string
	if node.Status.Addresses != nil {
		for _, addr := range node.Status.Addresses {
			switch addr.Type {
			case corev1.NodeInternalIP:
				internalIP = addr.Address
			case corev1.NodeExternalIP:
				externalIP = addr.Address
			case corev1.NodeHostName:
				hostname = addr.Address
			}
		}
	}

	// Use resource utils for capacity and allocatable extraction
	capacity := utils.ExtractResourceMapExcludingCommon(node.Status.Capacity)
	allocatable := utils.ExtractResourceMapExcludingCommon(node.Status.Allocatable)

	// Extract common resources as top-level fields
	capacityCPUMillicore := utils.ExtractCPUMillicores(node.Status.Capacity)
	capacityMemoryBytes := utils.ExtractMemoryBytes(node.Status.Capacity)
	capacityPods := utils.ExtractPodsCount(node.Status.Capacity)
	allocatableCPUMillicore := utils.ExtractCPUMillicores(node.Status.Allocatable)
	allocatableMemoryBytes := utils.ExtractMemoryBytes(node.Status.Allocatable)
	allocatablePods := utils.ExtractPodsCount(node.Status.Allocatable)

	// Get node conditions in a single loop
	var ready *bool
	conditions := make(map[string]*bool)
	unschedulable := node.Spec.Unschedulable

	for _, condition := range node.Status.Conditions {
		val := utils.ConvertCoreConditionStatus(condition.Status)

		if condition.Type == corev1.NodeReady {
			ready = val
		} else {
			// Add other conditions to the map
			conditions[string(condition.Type)] = val
		}
	}

	// Determine node phase
	// See: https://kubernetes.io/docs/concepts/architecture/nodes/#node-status
	phase := "Unknown"
	if node.Status.Phase != "" {
		phase = string(node.Status.Phase)
	}

	// Get node role
	nodeRole := ""
	var roles []string
	for key := range node.Labels {
		if after, ok := strings.CutPrefix(key, "node-role.kubernetes.io/"); ok {
			role := after
			if role != "" {
				roles = append(roles, role)
			}
		}
	}
	// Sort roles and use the first one for consistency
	if len(roles) > 0 {
		sort.Strings(roles)
		nodeRole = roles[0]
	}

	// Get taints
	var taints []types.TaintData
	if node.Spec.Taints != nil {
		for _, taint := range node.Spec.Taints {
			taints = append(taints, types.TaintData{
				Key:    taint.Key,
				Value:  taint.Value,
				Effect: string(taint.Effect),
			})
		}
	}

	// Get current usage metrics from cache
	cpuUsage, memoryUsage := h.getNodeUsageFromCache(node.Name)

	data := types.NodeData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "node",
				Name:             utils.ExtractName(node),
				CreatedTimestamp: utils.ExtractCreationTimestamp(node),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(node),
				Annotations: utils.ExtractAnnotations(node),
			},
		},
		Architecture:            node.Status.NodeInfo.Architecture,
		OperatingSystem:         node.Status.NodeInfo.OperatingSystem,
		KernelVersion:           node.Status.NodeInfo.KernelVersion,
		KubeletVersion:          node.Status.NodeInfo.KubeletVersion,
		KubeProxyVersion:        node.Status.NodeInfo.KubeProxyVersion,
		ContainerRuntimeVersion: node.Status.NodeInfo.ContainerRuntimeVersion,

		// Common resources as top-level fields
		CapacityCPUMillicore:    capacityCPUMillicore,
		CapacityMemoryBytes:     capacityMemoryBytes,
		CapacityPods:            capacityPods,
		AllocatableCPUMillicore: allocatableCPUMillicore,
		AllocatableMemoryBytes:  allocatableMemoryBytes,
		AllocatablePods:         allocatablePods,

		// Other resources (excluding common ones)
		Capacity:    capacity,
		Allocatable: allocatable,
		Ready:       ready,
		Phase:       phase,

		// Current usage metrics (from metrics server)
		UsageCPUMillicore: cpuUsage,
		UsageMemoryBytes:  memoryUsage,

		// All other conditions (excluding the top-level ones)
		Conditions:        conditions,
		InternalIP:        internalIP,
		ExternalIP:        externalIP,
		Hostname:          hostname,
		Unschedulable:     &unschedulable,
		Role:              nodeRole,
		Taints:            taints,
		DeletionTimestamp: utils.ExtractDeletionTimestamp(node),
	}

	return data
}

// collectAllNodeMetrics fetches all node metrics and stores them in the cache
func (h *NodeHandler) collectAllNodeMetrics(ctx context.Context) {
	if h.metricsClient == nil {
		return
	}

	// Get metrics for all nodes
	nodeMetricsList, err := h.metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{
		TimeoutSeconds: pointer.Int64(30),
	})
	if err != nil {
		// Silently fail if metrics server is unavailable
		return
	}

	// Store in cache: nodeName -> metrics
	for i := range nodeMetricsList.Items {
		nodeMetrics := &nodeMetricsList.Items[i]
		h.metricsCache.Add(nodeMetrics.Name, nodeMetrics)
	}
}

// getNodeUsageFromCache retrieves CPU and memory usage for a node from the metrics cache
func (h *NodeHandler) getNodeUsageFromCache(nodeName string) (cpuMillicore *int64, memoryBytes *int64) {
	if obj, exists := h.metricsCache.Get(nodeName); exists {
		if nodeMetrics, ok := obj.(*metricsv1beta1.NodeMetrics); ok {
			// Extract CPU usage in millicores
			if cpuQuantity, exists := nodeMetrics.Usage[corev1.ResourceCPU]; exists {
				cpuMillicoreVal := cpuQuantity.MilliValue()
				cpuMillicore = &cpuMillicoreVal
			}

			// Extract memory usage in bytes
			if memQuantity, exists := nodeMetrics.Usage[corev1.ResourceMemory]; exists {
				memoryBytesVal := memQuantity.Value()
				memoryBytes = &memoryBytesVal
			}
		}
	}

	return cpuMillicore, memoryBytes
}
