// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"fmt"
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

// Container state constants
const (
	ContainerStateRunning    = "running"
	ContainerStateWaiting    = "waiting"
	ContainerStateTerminated = "terminated"
	ContainerStateUnknown    = "unknown"
)

// ContainerHandler handles collection of container metrics
type ContainerHandler struct {
	utils.BaseHandler
	stateCache    cache.ThreadSafeStore
	metricsCache  cache.ThreadSafeStore
	metricsClient metricsclientset.Interface
	envVarFilter  map[string]struct{}
}

// NewContainerHandler creates a new ContainerHandler
func NewContainerHandler(client kubernetes.Interface, metricsClient metricsclientset.Interface, envVars []string) *ContainerHandler {
	filter := make(map[string]struct{})
	for _, v := range envVars {
		filter[v] = struct{}{}
	}
	return &ContainerHandler{
		BaseHandler:   utils.NewBaseHandler(client),
		stateCache:    cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		metricsCache:  cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		metricsClient: metricsClient,
		envVarFilter:  filter,
	}
}

// SetupInformer sets up the pod informer (containers are accessed through pods)
func (h *ContainerHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create pod informer (containers are accessed through pods)
	informer := factory.Core().V1().Pods().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers container metrics from the cluster (uses cache)
func (h *ContainerHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	// Get all pods from the cache
	pods := utils.SafeGetStoreList(h.GetInformer())
	return h.processPods(ctx, pods, namespaces)
}

// processPods processes a list of pods and returns container entries
func (h *ContainerHandler) processPods(ctx context.Context, pods []any, namespaces []string) ([]any, error) {
	var entries []any
	currentStates := make(map[string]any)
	listTime := time.Now()

	// Collect all metrics upfront for efficiency
	h.collectAllMetrics(ctx, namespaces)

	for _, obj := range pods {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, pod.Namespace) {
			continue
		}

		// Process regular containers
		for _, container := range pod.Status.ContainerStatuses {
			containerKey := h.getContainerKey(pod.Namespace, pod.Name, container.Name)
			currentState := h.getContainerState(&container)
			currentStates[containerKey] = currentState

			// Always log running containers
			if currentState == ContainerStateRunning {
				entry := h.createLogEntry(pod, &container, false)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}

			// Log newly terminated containers
			if h.isNewlyTerminated(containerKey, currentState, &container) {
				entry := h.createLogEntry(pod, &container, false)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}
		}

		// Process init containers
		for _, container := range pod.Status.InitContainerStatuses {
			containerKey := h.getContainerKey(pod.Namespace, pod.Name, container.Name)
			currentState := h.getContainerState(&container)
			currentStates[containerKey] = currentState

			// Always log running init containers
			if currentState == ContainerStateRunning {
				entry := h.createLogEntry(pod, &container, true)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}

			// Log newly terminated init containers
			if h.isNewlyTerminated(containerKey, currentState, &container) {
				entry := h.createLogEntry(pod, &container, true)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}
		}
	}

	// Update state cache and cleanup deleted containers
	h.updateStateCache(currentStates)

	// Clear the metrics cache for the next collection.
	// No reason to hold onto old metrics in memory.
	h.metricsCache.Replace(make(map[string]any), "")

	return entries, nil
}

// getContainerKey creates a unique key for a container
func (h *ContainerHandler) getContainerKey(namespace, podName, containerName string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, podName, containerName)
}

// getContainerState determines the current state of a container
func (h *ContainerHandler) getContainerState(container *corev1.ContainerStatus) string {
	if container.State.Running != nil {
		return ContainerStateRunning
	} else if container.State.Waiting != nil {
		return ContainerStateWaiting
	} else if container.State.Terminated != nil {
		return ContainerStateTerminated
	}
	return ContainerStateUnknown
}

// getResourceType determines the resource type based on container type
func (h *ContainerHandler) getResourceType(isInitContainer bool) string {
	if isInitContainer {
		return "init_container"
	}
	return "container"
}

// isNewlyTerminated checks if a container should be logged as terminated
func (h *ContainerHandler) isNewlyTerminated(containerKey, currentState string, container *corev1.ContainerStatus) bool {
	if currentState != ContainerStateTerminated {
		return false
	}

	// Check if container terminated within the last hour
	if container != nil && container.State.Terminated != nil {
		if container.State.Terminated.FinishedAt.IsZero() {
			return false // No finish time, skip
		}

		// Only log if terminated within the last hour
		oneHourAgo := time.Now().Add(-1 * time.Hour)
		if container.State.Terminated.FinishedAt.Time.Before(oneHourAgo) {
			return false // Too old, skip
		}
	}

	// Get previous state from cache
	if previousStateObj, exists := h.stateCache.Get(containerKey); exists {
		previousState := previousStateObj.(string)
		// Log if it transitioned from running to terminated
		return previousState == ContainerStateRunning
	}

	// Log if we haven't seen this container before (first time seeing a terminated container)
	return true
}

// updateStateCache updates the state cache with current states and cleans up deleted containers
func (h *ContainerHandler) updateStateCache(currentStates map[string]any) {
	h.stateCache.Replace(currentStates, "")
}

// createLogEntry creates a ContainerData from a pod and container status
func (h *ContainerHandler) createLogEntry(pod *corev1.Pod, container *corev1.ContainerStatus, isInitContainer bool) types.ContainerData {
	// Handle nil container case
	if container == nil {
		return types.ContainerData{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:         time.Now(),
					ResourceType:      h.getResourceType(isInitContainer),
					Name:              "",
					CreatedTimestamp:  utils.ExtractCreationTimestamp(pod),
					EventType:         "snapshot",
					DeletionTimestamp: utils.ExtractDeletionTimestamp(pod),
				},
				Namespace: pod.Namespace,
			},
			PodName: pod.Name,
			State:   ContainerStateUnknown,
		}
	}

	// Determine container state
	state := ContainerStateUnknown
	var stateRunning, stateWaiting, stateTerminated *bool

	var waitingReason, waitingMessage string
	var startedAt, finishedAt, startedAtTerm *time.Time
	var exitCode int32
	var reason, message string
	var lastTerminatedReason string
	var lastTerminatedExitCode int32
	var lastTerminatedTimestamp *time.Time

	if container.State.Running != nil {
		state = ContainerStateRunning
		val := true
		stateRunning = &val
		if !container.State.Running.StartedAt.IsZero() {
			startedAt = &container.State.Running.StartedAt.Time
		}
	} else if container.State.Waiting != nil {
		state = ContainerStateWaiting
		val := true
		stateWaiting = &val
		waitingReason = string(container.State.Waiting.Reason)
		waitingMessage = container.State.Waiting.Message
	} else if container.State.Terminated != nil {
		state = ContainerStateTerminated
		val := true
		stateTerminated = &val
		exitCode = container.State.Terminated.ExitCode
		reason = string(container.State.Terminated.Reason)
		message = container.State.Terminated.Message
		if !container.State.Terminated.FinishedAt.IsZero() {
			finishedAt = &container.State.Terminated.FinishedAt.Time
		}
		if !container.State.Terminated.StartedAt.IsZero() {
			startedAtTerm = &container.State.Terminated.StartedAt.Time
		}
	}

	// Get last terminated state
	if container.LastTerminationState.Terminated != nil {
		lastTerminatedReason = string(container.LastTerminationState.Terminated.Reason)
		lastTerminatedExitCode = container.LastTerminationState.Terminated.ExitCode
		if !container.LastTerminationState.Terminated.FinishedAt.IsZero() {
			lastTerminatedTimestamp = &container.LastTerminationState.Terminated.FinishedAt.Time
		}
	}

	// Extract resource requests and limits from pod spec
	var resourceRequests, resourceLimits map[string]string
	var requestsCPUMillicore, requestsMemoryBytes, limitsCPUMillicore, limitsMemoryBytes *int64
	if isInitContainer {
		// Look in init containers for init container resources
		for _, containerSpec := range pod.Spec.InitContainers {
			if containerSpec.Name == container.Name {
				resourceRequests = utils.ExtractResourceMapExcludingCPUMemory(containerSpec.Resources.Requests)
				resourceLimits = utils.ExtractResourceMapExcludingCPUMemory(containerSpec.Resources.Limits)
				requestsCPUMillicore = utils.ExtractCPUMillicores(containerSpec.Resources.Requests)
				requestsMemoryBytes = utils.ExtractMemoryBytes(containerSpec.Resources.Requests)
				limitsCPUMillicore = utils.ExtractCPUMillicores(containerSpec.Resources.Limits)
				limitsMemoryBytes = utils.ExtractMemoryBytes(containerSpec.Resources.Limits)
				break
			}
		}
	} else {
		// Look in regular containers for regular container resources
		for _, containerSpec := range pod.Spec.Containers {
			if containerSpec.Name == container.Name {
				resourceRequests = utils.ExtractResourceMapExcludingCPUMemory(containerSpec.Resources.Requests)
				resourceLimits = utils.ExtractResourceMapExcludingCPUMemory(containerSpec.Resources.Limits)
				requestsCPUMillicore = utils.ExtractCPUMillicores(containerSpec.Resources.Requests)
				requestsMemoryBytes = utils.ExtractMemoryBytes(containerSpec.Resources.Requests)
				limitsCPUMillicore = utils.ExtractCPUMillicores(containerSpec.Resources.Limits)
				limitsMemoryBytes = utils.ExtractMemoryBytes(containerSpec.Resources.Limits)
				break
			}
		}
	}

	// Get state started time (when container first started)
	var stateStarted *time.Time
	if container.State.Running != nil && !container.State.Running.StartedAt.IsZero() {
		stateStarted = &container.State.Running.StartedAt.Time
	}

	// Get container usage metrics
	cpuUsage, memoryUsage := h.getContainerUsageFromCache(pod.Namespace, pod.Name, container.Name)

	// Capture selected environment variables if filter configured
	var environmentVariables map[string]string
	if len(h.envVarFilter) > 0 {
		var envVars []corev1.EnvVar
		if isInitContainer {
			for _, cs := range pod.Spec.InitContainers {
				if cs.Name == container.Name {
					envVars = cs.Env
					break
				}
			}
		} else {
			for _, cs := range pod.Spec.Containers {
				if cs.Name == container.Name {
					envVars = cs.Env
					break
				}
			}
		}
		filtered := make(map[string]string)
		for _, ev := range envVars {
			if _, ok := h.envVarFilter[ev.Name]; ok {
				filtered[ev.Name] = ev.Value
			}
		}
		if len(filtered) > 0 {
			environmentVariables = filtered
		}
	}

	data := types.ContainerData{
		NamespacedMetadata: types.NamespacedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:         time.Now(),
				ResourceType:      h.getResourceType(isInitContainer),
				Name:              container.Name,
				CreatedTimestamp:  utils.ExtractCreationTimestamp(pod),
				EventType:         "snapshot",
				DeletionTimestamp: utils.ExtractDeletionTimestamp(pod),
			},
			Namespace: pod.Namespace,
		},
		Image:                   container.Image,
		ImageID:                 container.ImageID,
		ContainerID:             container.ContainerID,
		PodName:                 pod.Name,
		NodeName:                pod.Spec.NodeName,
		Ready:                   &container.Ready,
		RestartCount:            container.RestartCount,
		State:                   state,
		StateRunning:            stateRunning,
		StateWaiting:            stateWaiting,
		StateTerminated:         stateTerminated,
		WaitingReason:           waitingReason,
		WaitingMessage:          waitingMessage,
		StartedAt:               startedAt,
		ExitCode:                exitCode,
		Reason:                  reason,
		Message:                 message,
		FinishedAt:              finishedAt,
		StartedAtTerm:           startedAtTerm,
		ResourceRequests:        resourceRequests,
		ResourceLimits:          resourceLimits,
		RequestsCPUMillicore:    requestsCPUMillicore,
		RequestsMemoryBytes:     requestsMemoryBytes,
		LimitsCPUMillicore:      limitsCPUMillicore,
		LimitsMemoryBytes:       limitsMemoryBytes,
		LastTerminatedReason:    lastTerminatedReason,
		LastTerminatedExitCode:  lastTerminatedExitCode,
		LastTerminatedTimestamp: lastTerminatedTimestamp,
		StateStarted:            stateStarted,
		UsageCPUMillicore:       cpuUsage,
		UsageMemoryBytes:        memoryUsage,
		EnvironmentVariables:    environmentVariables,
	}

	return data
}

// collectAllMetrics retrieves all pod metrics for the given namespaces and stores them in cache
func (h *ContainerHandler) collectAllMetrics(ctx context.Context, namespaces []string) {
	if h.metricsClient == nil {
		return
	}

	// If namespaces is empty, we need to collect metrics from all namespaces
	if len(namespaces) == 0 {
		// Get metrics from all namespaces by using empty namespace (cluster-wide)
		podMetricsList, err := h.metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{
			TimeoutSeconds: pointer.Int64(30),
		})
		if err != nil {
			// If cluster-wide query fails, return
			return
		}

		// Store in cache: namespace/podName/containerName -> metrics
		for _, podMetrics := range podMetricsList.Items {
			for i := range podMetrics.Containers {
				containerMetrics := &podMetrics.Containers[i]
				key := h.getContainerKey(podMetrics.Namespace, podMetrics.Name, containerMetrics.Name)
				h.metricsCache.Add(key, containerMetrics)
			}
		}
	} else {
		// Get metrics from specific namespaces
		for _, namespace := range namespaces {
			// Get all pod metrics for this namespace
			podMetricsList, err := h.metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
				TimeoutSeconds: pointer.Int64(30),
			})
			if err != nil {
				// Continue with other namespaces if this one fails
				continue
			}

			// Store in cache: namespace/podName/containerName -> metrics
			for _, podMetrics := range podMetricsList.Items {
				for i := range podMetrics.Containers {
					containerMetrics := &podMetrics.Containers[i]
					key := h.getContainerKey(podMetrics.Namespace, podMetrics.Name, containerMetrics.Name)
					h.metricsCache.Add(key, containerMetrics)
				}
			}
		}
	}
}

// getContainerUsageFromCache retrieves CPU and memory usage for a container from the metrics cache
func (h *ContainerHandler) getContainerUsageFromCache(namespace, podName, containerName string) (cpuMillicore *int64, memoryBytes *int64) {
	key := h.getContainerKey(namespace, podName, containerName)

	if obj, exists := h.metricsCache.Get(key); exists {
		if containerMetrics, ok := obj.(*metricsv1beta1.ContainerMetrics); ok {
			// Extract CPU usage in millicores
			if cpuQuantity, exists := containerMetrics.Usage[corev1.ResourceCPU]; exists {
				cpuMillicoreVal := cpuQuantity.MilliValue()
				cpuMillicore = &cpuMillicoreVal
			}

			// Extract memory usage in bytes
			if memQuantity, exists := containerMetrics.Usage[corev1.ResourceMemory]; exists {
				memoryBytesVal := memQuantity.Value()
				memoryBytes = &memoryBytesVal
			}
		}
	}

	return cpuMillicore, memoryBytes
}
