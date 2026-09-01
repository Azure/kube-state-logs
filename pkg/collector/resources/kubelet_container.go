// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// KubeletContainerHandler handles collection of container data from shared
// kubelet pod and stats snapshots.
type KubeletContainerHandler struct {
	source            kubelet.SnapshotSource
	stateCache        cache.ThreadSafeStore
	envVarFilter      map[string]struct{}
	labelSelector     labels.Selector
	fieldSelector     fields.Selector
	nodeLabelPromoter nodeLabelPromoter
}

// NewKubeletContainerHandler creates a new KubeletContainerHandler.
func NewKubeletContainerHandler(source kubelet.SnapshotSource, client kubernetes.Interface, nodeName string, envVars []string, promotedNodeLabels ...string) *KubeletContainerHandler {
	filter := make(map[string]struct{})
	for _, variable := range envVars {
		filter[variable] = struct{}{}
	}
	promoter := newNodeLabelPromoter(promotedNodeLabels)
	promoter.useDirectLookup(client, nodeName)
	return &KubeletContainerHandler{
		source:            source,
		stateCache:        cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		envVarFilter:      filter,
		labelSelector:     labels.Everything(),
		fieldSelector:     fields.Everything(),
		nodeLabelPromoter: promoter,
	}
}

// SetSelectors configures pod label and field filtering for container snapshots.
func (h *KubeletContainerHandler) SetSelectors(labelSelector labels.Selector, fieldSelector fields.Selector) {
	if labelSelector == nil {
		labelSelector = labels.Everything()
	}
	if fieldSelector == nil {
		fieldSelector = fields.Everything()
	}
	h.labelSelector = labelSelector
	h.fieldSelector = fieldSelector
}

// Collect gathers container data from the kubelet API.
func (h *KubeletContainerHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	snapshot, err := h.source.GetSnapshot(ctx, true)
	if err != nil {
		return nil, err
	}
	if snapshot.StatsError != nil {
		klog.Warningf("Failed to collect container usage from kubelet: %v", snapshot.StatsError)
	}
	return h.processPods(snapshot.Pods, snapshot.Stats, namespaces), nil
}

func (h *KubeletContainerHandler) processPods(pods []corev1.Pod, stats *kubelet.StatsSummary, namespaces []string) []any {
	entries := make([]any, 0)
	currentStates := make(map[string]any)
	statsLookup := h.buildStatsLookup(stats)
	listTime := time.Now()

	for i := range pods {
		pod := &pods[i]
		if !utils.ShouldIncludeNamespace(namespaces, pod.Namespace) {
			continue
		}
		if !matchesPodSelectors(pod, h.labelSelector, h.fieldSelector) {
			continue
		}

		nodeLabels := h.nodeLabelPromoter.labelsForNode(pod.Spec.NodeName)
		for j := range pod.Status.ContainerStatuses {
			container := &pod.Status.ContainerStatuses[j]
			containerKey := h.getContainerKey(string(pod.UID), pod.Namespace, pod.Name, container.Name)
			currentState := h.getContainerState(container)
			currentStates[containerKey] = currentState

			if currentState == ContainerStateRunning || h.isNewlyTerminated(containerKey, currentState, container) {
				entry := h.createLogEntry(pod, container, false, statsLookup, nodeLabels)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}
		}

		for j := range pod.Status.InitContainerStatuses {
			container := &pod.Status.InitContainerStatuses[j]
			containerKey := h.getContainerKey(string(pod.UID), pod.Namespace, pod.Name, container.Name)
			currentState := h.getContainerState(container)
			currentStates[containerKey] = currentState

			if currentState == ContainerStateRunning || h.isNewlyTerminated(containerKey, currentState, container) {
				entry := h.createLogEntry(pod, container, true, statsLookup, nodeLabels)
				entry.Timestamp = listTime
				entries = append(entries, entry)
			}
		}
	}

	h.stateCache.Replace(currentStates, "")
	return entries
}

func (h *KubeletContainerHandler) buildStatsLookup(stats *kubelet.StatsSummary) map[string]*kubelet.ContainerStats {
	lookup := make(map[string]*kubelet.ContainerStats)
	if stats == nil {
		return lookup
	}
	for i := range stats.Pods {
		podStats := &stats.Pods[i]
		for j := range podStats.Containers {
			containerStats := &podStats.Containers[j]
			lookup[fmt.Sprintf("%s/%s", podStats.PodRef.UID, containerStats.Name)] = containerStats
		}
	}
	return lookup
}

func (h *KubeletContainerHandler) getContainerKey(podUID, namespace, podName, containerName string) string {
	return fmt.Sprintf("%s/%s/%s/%s", podUID, namespace, podName, containerName)
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
	if currentState != ContainerStateTerminated || container == nil || container.State.Terminated == nil {
		return false
	}
	if container.State.Terminated.FinishedAt.IsZero() {
		return false
	}
	if container.State.Terminated.FinishedAt.Time.Before(time.Now().Add(-time.Hour)) {
		return false
	}

	if previousStateValue, exists := h.stateCache.Get(containerKey); exists {
		previousState, ok := previousStateValue.(string)
		return ok && previousState == ContainerStateRunning
	}
	return true
}

func (h *KubeletContainerHandler) getContainerUsage(podUID, containerName string, statsLookup map[string]*kubelet.ContainerStats) (cpuMillicore *int64, memoryBytes *int64) {
	containerStats, exists := statsLookup[fmt.Sprintf("%s/%s", podUID, containerName)]
	if !exists {
		return nil, nil
	}
	if containerStats.CPU != nil && containerStats.CPU.UsageNanoCores != nil {
		value := kubelet.NanoCoresToMilliCores(*containerStats.CPU.UsageNanoCores)
		cpuMillicore = &value
	}
	if containerStats.Memory != nil && containerStats.Memory.WorkingSetBytes != nil {
		value := int64(*containerStats.Memory.WorkingSetBytes)
		memoryBytes = &value
	}
	return cpuMillicore, memoryBytes
}

func (h *KubeletContainerHandler) createLogEntry(pod *corev1.Pod, container *corev1.ContainerStatus, isInitContainer bool, statsLookup map[string]*kubelet.ContainerStats, nodeLabels map[string]string) types.ContainerData {
	if container == nil {
		return types.ContainerData{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     h.getResourceType(isInitContainer),
					CreatedTimestamp: utils.ExtractCreationTimestamp(pod),
				},
				Namespace: pod.Namespace,
			},
			PodName:    pod.Name,
			PodUID:     string(pod.UID),
			NodeName:   pod.Spec.NodeName,
			NodeLabels: nodeLabels,
			State:      ContainerStateUnknown,
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
		stateRunning = boolPointer(true)
		if !container.State.Running.StartedAt.IsZero() {
			startedAt = &container.State.Running.StartedAt.Time
		}
	} else if container.State.Waiting != nil {
		state = ContainerStateWaiting
		stateWaiting = boolPointer(true)
		waitingReason = container.State.Waiting.Reason
		waitingMessage = container.State.Waiting.Message
	} else if container.State.Terminated != nil {
		state = ContainerStateTerminated
		stateTerminated = boolPointer(true)
		exitCode = container.State.Terminated.ExitCode
		reason = container.State.Terminated.Reason
		message = container.State.Terminated.Message
		if !container.State.Terminated.FinishedAt.IsZero() {
			finishedAt = &container.State.Terminated.FinishedAt.Time
		}
		if !container.State.Terminated.StartedAt.IsZero() {
			startedAtTerm = &container.State.Terminated.StartedAt.Time
		}
	}

	if container.LastTerminationState.Terminated != nil {
		lastTerminatedReason = container.LastTerminationState.Terminated.Reason
		lastTerminatedExitCode = container.LastTerminationState.Terminated.ExitCode
		if !container.LastTerminationState.Terminated.FinishedAt.IsZero() {
			lastTerminatedTimestamp = &container.LastTerminationState.Terminated.FinishedAt.Time
		}
	}

	var imagePullPolicy string
	var resourceRequests, resourceLimits map[string]string
	var requestsCPUMillicore, requestsMemoryBytes, limitsCPUMillicore, limitsMemoryBytes *int64
	var envVars []corev1.EnvVar
	containerSpecs := pod.Spec.Containers
	if isInitContainer {
		containerSpecs = pod.Spec.InitContainers
	}
	for i := range containerSpecs {
		containerSpec := &containerSpecs[i]
		if containerSpec.Name != container.Name {
			continue
		}
		imagePullPolicy = string(containerSpec.ImagePullPolicy)
		resourceRequests = utils.ExtractResourceMapExcludingCPUMemory(containerSpec.Resources.Requests)
		resourceLimits = utils.ExtractResourceMapExcludingCPUMemory(containerSpec.Resources.Limits)
		requestsCPUMillicore = utils.ExtractCPUMillicores(containerSpec.Resources.Requests)
		requestsMemoryBytes = utils.ExtractMemoryBytes(containerSpec.Resources.Requests)
		limitsCPUMillicore = utils.ExtractCPUMillicores(containerSpec.Resources.Limits)
		limitsMemoryBytes = utils.ExtractMemoryBytes(containerSpec.Resources.Limits)
		envVars = containerSpec.Env
		break
	}

	var stateStarted *time.Time
	if container.State.Running != nil && !container.State.Running.StartedAt.IsZero() {
		stateStarted = &container.State.Running.StartedAt.Time
	}
	cpuUsage, memoryUsage := h.getContainerUsage(string(pod.UID), container.Name, statsLookup)

	var environmentVariables map[string]string
	for _, envVar := range envVars {
		if _, included := h.envVarFilter[envVar.Name]; included {
			if environmentVariables == nil {
				environmentVariables = make(map[string]string)
			}
			environmentVariables[envVar.Name] = envVar.Value
		}
	}

	return types.ContainerData{
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
		ImagePullPolicy:         imagePullPolicy,
		ImageID:                 container.ImageID,
		ContainerID:             container.ContainerID,
		PodName:                 pod.Name,
		PodUID:                  string(pod.UID),
		NodeName:                pod.Spec.NodeName,
		NodeLabels:              nodeLabels,
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
}

func boolPointer(value bool) *bool {
	return &value
}
