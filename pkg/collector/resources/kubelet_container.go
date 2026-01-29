package resources

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	"k8s.io/utils/pointer"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// KubeletContainerHandler handles collection of container data from the kubelet API.
// Unlike ContainerHandler, this doesn't use informers since the kubelet API
// doesn't support watches - it polls the kubelet API directly.
type KubeletContainerHandler struct {
	client        *kubelet.Client
	stateCache    cache.ThreadSafeStore
	metricsCache  cache.ThreadSafeStore
	metricsClient metricsclientset.Interface
	envVarFilter  map[string]struct{}
}

// NewKubeletContainerHandler creates a new KubeletContainerHandler.
func NewKubeletContainerHandler(client *kubelet.Client, metricsClient metricsclientset.Interface, envVars []string) *KubeletContainerHandler {
	filter := make(map[string]struct{})
	for _, v := range envVars {
		filter[v] = struct{}{}
	}
	return &KubeletContainerHandler{
		client:        client,
		stateCache:    cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		metricsCache:  cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		metricsClient: metricsClient,
		envVarFilter:  filter,
	}
}

// Collect gathers container data from the kubelet API.
func (h *KubeletContainerHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	pods, err := h.client.GetPods(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to []any for processPods
	podObjs := make([]any, len(pods))
	for i := range pods {
		podObjs[i] = &pods[i]
	}

	return h.processPods(ctx, podObjs, namespaces)
}

// processPods processes a list of pods and returns container entries.
// This is similar to ContainerHandler.processPods but adapted for kubelet mode.
func (h *KubeletContainerHandler) processPods(ctx context.Context, pods []any, namespaces []string) ([]any, error) {
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
	h.metricsCache.Replace(make(map[string]any), "")

	return entries, nil
}

// getContainerKey creates a unique key for a container
func (h *KubeletContainerHandler) getContainerKey(namespace, podName, containerName string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, podName, containerName)
}

// getContainerState determines the current state of a container
func (h *KubeletContainerHandler) getContainerState(container *corev1.ContainerStatus) string {
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
func (h *KubeletContainerHandler) getResourceType(isInitContainer bool) string {
	if isInitContainer {
		return "init_container"
	}
	return "container"
}

// isNewlyTerminated checks if a container should be logged as terminated
func (h *KubeletContainerHandler) isNewlyTerminated(containerKey, currentState string, container *corev1.ContainerStatus) bool {
	if currentState != ContainerStateTerminated {
		return false
	}

	// Check if container terminated within the last hour
	if container != nil && container.State.Terminated != nil {
		if container.State.Terminated.FinishedAt.IsZero() {
			return false
		}

		oneHourAgo := time.Now().Add(-1 * time.Hour)
		if container.State.Terminated.FinishedAt.Time.Before(oneHourAgo) {
			return false
		}
	}

	// Get previous state from cache
	if previousStateObj, exists := h.stateCache.Get(containerKey); exists {
		previousState := previousStateObj.(string)
		return previousState == ContainerStateRunning
	}

	return true
}

// updateStateCache updates the state cache with current states
func (h *KubeletContainerHandler) updateStateCache(currentStates map[string]any) {
	h.stateCache.Replace(currentStates, "")
}

// createLogEntry creates a ContainerData from a pod and container status
func (h *KubeletContainerHandler) createLogEntry(pod *corev1.Pod, container *corev1.ContainerStatus, isInitContainer bool) types.ContainerData {
	if container == nil {
		return types.ContainerData{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     h.getResourceType(isInitContainer),
					Name:             "",
					CreatedTimestamp: utils.ExtractCreationTimestamp(pod),
				},
				Namespace: pod.Namespace,
			},
			PodName: pod.Name,
			State:   ContainerStateUnknown,
		}
	}

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

	if container.LastTerminationState.Terminated != nil {
		lastTerminatedReason = string(container.LastTerminationState.Terminated.Reason)
		lastTerminatedExitCode = container.LastTerminationState.Terminated.ExitCode
		if !container.LastTerminationState.Terminated.FinishedAt.IsZero() {
			lastTerminatedTimestamp = &container.LastTerminationState.Terminated.FinishedAt.Time
		}
	}

	var resourceRequests, resourceLimits map[string]string
	var requestsCPUMillicore, requestsMemoryBytes, limitsCPUMillicore, limitsMemoryBytes *int64
	if isInitContainer {
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

	var stateStarted *time.Time
	if container.State.Running != nil && !container.State.Running.StartedAt.IsZero() {
		stateStarted = &container.State.Running.StartedAt.Time
	}

	cpuUsage, memoryUsage := h.getContainerUsageFromCache(pod.Namespace, pod.Name, container.Name)

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
				Timestamp:        time.Now(),
				ResourceType:     h.getResourceType(isInitContainer),
				Name:             container.Name,
				CreatedTimestamp: utils.ExtractCreationTimestamp(pod),
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
func (h *KubeletContainerHandler) collectAllMetrics(ctx context.Context, namespaces []string) {
	if h.metricsClient == nil {
		return
	}

	if len(namespaces) == 0 {
		podMetricsList, err := h.metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{
			TimeoutSeconds: pointer.Int64(30),
		})
		if err != nil {
			return
		}

		for _, podMetrics := range podMetricsList.Items {
			for i := range podMetrics.Containers {
				containerMetrics := &podMetrics.Containers[i]
				key := h.getContainerKey(podMetrics.Namespace, podMetrics.Name, containerMetrics.Name)
				h.metricsCache.Add(key, containerMetrics)
			}
		}
	} else {
		for _, namespace := range namespaces {
			podMetricsList, err := h.metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
				TimeoutSeconds: pointer.Int64(30),
			})
			if err != nil {
				continue
			}

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
func (h *KubeletContainerHandler) getContainerUsageFromCache(namespace, podName, containerName string) (cpuMillicore *int64, memoryBytes *int64) {
	key := h.getContainerKey(namespace, podName, containerName)

	if obj, exists := h.metricsCache.Get(key); exists {
		if containerMetrics, ok := obj.(*metricsv1beta1.ContainerMetrics); ok {
			if cpuQuantity, exists := containerMetrics.Usage[corev1.ResourceCPU]; exists {
				cpuMillicoreVal := cpuQuantity.MilliValue()
				cpuMillicore = &cpuMillicoreVal
			}

			if memQuantity, exists := containerMetrics.Usage[corev1.ResourceMemory]; exists {
				memoryBytesVal := memQuantity.Value()
				memoryBytes = &memoryBytesVal
			}
		}
	}

	return cpuMillicore, memoryBytes
}
