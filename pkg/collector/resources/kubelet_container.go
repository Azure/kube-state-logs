// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// KubeletContainerHandler processes container data obtained from the kubelet API.
type KubeletContainerHandler struct {
	stateCache   cache.ThreadSafeStore
	envVarFilter map[string]struct{}
}

// NewKubeletContainerHandler creates a new KubeletContainerHandler.
func NewKubeletContainerHandler(envVars []string) *KubeletContainerHandler {
	filter := make(map[string]struct{})
	for _, v := range envVars {
		filter[v] = struct{}{}
	}
	return &KubeletContainerHandler{
		stateCache:   cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		envVarFilter: filter,
	}
}

// Process transforms pods and stats into container log entries.
func (h *KubeletContainerHandler) Process(pods []corev1.Pod, stats *statsv1alpha1.Summary) ([]any, error) {
	// Build a stats lookup: podUID/containerName -> ContainerStats
	statsLookup := h.buildStatsLookup(stats)

	var entries []any
	currentStates := make(map[string]any)
	listTime := time.Now()

	for i := range pods {
		pod := &pods[i]

		// Process regular containers
		for j := range pod.Status.ContainerStatuses {
			container := &pod.Status.ContainerStatuses[j]
			containerKey := h.getContainerKey(pod.Namespace, pod.Name, container.Name)
			currentState := h.getContainerState(container)
			currentStates[containerKey] = currentState

			if currentState == ContainerStateRunning {
				entry := h.createLogEntry(pod, container, false, statsLookup)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}

			if h.isNewlyTerminated(containerKey, currentState, container) {
				entry := h.createLogEntry(pod, container, false, statsLookup)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}
		}

		// Process init containers
		for j := range pod.Status.InitContainerStatuses {
			container := &pod.Status.InitContainerStatuses[j]
			containerKey := h.getContainerKey(pod.Namespace, pod.Name, container.Name)
			currentState := h.getContainerState(container)
			currentStates[containerKey] = currentState

			if currentState == ContainerStateRunning {
				entry := h.createLogEntry(pod, container, true, statsLookup)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}

			if h.isNewlyTerminated(containerKey, currentState, container) {
				entry := h.createLogEntry(pod, container, true, statsLookup)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}
		}
	}

	h.stateCache.Replace(currentStates, "")

	return entries, nil
}

func (h *KubeletContainerHandler) buildStatsLookup(stats *statsv1alpha1.Summary) map[string]*statsv1alpha1.ContainerStats {
	lookup := make(map[string]*statsv1alpha1.ContainerStats)
	if stats == nil {
		return lookup
	}
	for i := range stats.Pods {
		podStats := &stats.Pods[i]
		for j := range podStats.Containers {
			cs := &podStats.Containers[j]
			key := fmt.Sprintf("%s/%s", podStats.PodRef.UID, cs.Name)
			lookup[key] = cs
		}
	}
	return lookup
}

func (h *KubeletContainerHandler) getContainerKey(namespace, podName, containerName string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, podName, containerName)
}

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

func (h *KubeletContainerHandler) getResourceType(isInitContainer bool) string {
	if isInitContainer {
		return "init_container"
	}
	return "container"
}

func (h *KubeletContainerHandler) isNewlyTerminated(containerKey, currentState string, container *corev1.ContainerStatus) bool {
	if currentState != ContainerStateTerminated {
		return false
	}

	if container != nil && container.State.Terminated != nil {
		if container.State.Terminated.FinishedAt.IsZero() {
			return false
		}
		oneHourAgo := time.Now().Add(-1 * time.Hour)
		if container.State.Terminated.FinishedAt.Time.Before(oneHourAgo) {
			return false
		}
	}

	if previousStateObj, exists := h.stateCache.Get(containerKey); exists {
		previousState := previousStateObj.(string)
		return previousState == ContainerStateRunning
	}

	return true
}

func (h *KubeletContainerHandler) getContainerUsage(podUID, containerName string, statsLookup map[string]*statsv1alpha1.ContainerStats) (cpuMillicore *int64, memoryBytes *int64) {
	key := fmt.Sprintf("%s/%s", podUID, containerName)
	cs, ok := statsLookup[key]
	if !ok {
		return nil, nil
	}
	if cs.CPU != nil && cs.CPU.UsageNanoCores != nil {
		val := kubelet.NanoCoresToMilliCores(*cs.CPU.UsageNanoCores)
		cpuMillicore = &val
	}
	if cs.Memory != nil && cs.Memory.WorkingSetBytes != nil {
		val := int64(*cs.Memory.WorkingSetBytes)
		memoryBytes = &val
	}
	return cpuMillicore, memoryBytes
}

func (h *KubeletContainerHandler) createLogEntry(pod *corev1.Pod, container *corev1.ContainerStatus, isInitContainer bool, statsLookup map[string]*statsv1alpha1.ContainerStats) types.ContainerData {
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

	cpuUsage, memoryUsage := h.getContainerUsage(string(pod.UID), container.Name, statsLookup)

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
		PodUID:                  string(pod.UID),
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
