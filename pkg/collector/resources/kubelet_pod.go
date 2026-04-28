// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// KubeletPodHandler processes pod data obtained from the kubelet API.
type KubeletPodHandler struct{}

// NewKubeletPodHandler creates a new KubeletPodHandler.
func NewKubeletPodHandler() *KubeletPodHandler {
	return &KubeletPodHandler{}
}

// Process transforms a slice of pods into log entries.
func (h *KubeletPodHandler) Process(pods []corev1.Pod) ([]any, error) {
	var entries []any
	listTime := time.Now()

	for i := range pods {
		entry := h.createLogEntry(&pods[i])
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a PodData from a pod.
// This mirrors PodHandler.createLogEntry to produce identical output.
func (h *KubeletPodHandler) createLogEntry(pod *corev1.Pod) types.PodData {
	qosClass := string(pod.Status.QOSClass)
	if qosClass == "" {
		qosClass = "BestEffort"
	}

	priorityClass := ""
	if pod.Spec.PriorityClassName != "" {
		priorityClass = pod.Spec.PriorityClassName
	}

	createdByKind, createdByName := utils.GetOwnerReferenceInfo(pod)

	var conditionReady, conditionInitialized, conditionScheduled, conditionContainersReady *bool
	conditions := make(map[string]*bool)

	for _, condition := range pod.Status.Conditions {
		val := utils.ConvertCoreConditionStatus(condition.Status)

		switch condition.Type {
		case corev1.PodReady:
			conditionReady = val
		case corev1.PodInitialized:
			conditionInitialized = val
		case corev1.PodScheduled:
			conditionScheduled = val
		case corev1.ContainersReady:
			conditionContainersReady = val
		default:
			conditions[string(condition.Type)] = val
		}
	}

	var totalRestartCount int32
	for _, container := range pod.Status.ContainerStatuses {
		totalRestartCount += container.RestartCount
	}

	var deletionTimestamp, startTime, initializedTime, readyTime, scheduledTime *time.Time
	if pod.DeletionTimestamp != nil {
		deletionTimestamp = &pod.DeletionTimestamp.Time
	}
	if pod.Status.StartTime != nil && !pod.Status.StartTime.IsZero() {
		startTime = &pod.Status.StartTime.Time
	}

	for _, condition := range pod.Status.Conditions {
		switch condition.Type {
		case corev1.PodInitialized:
			if condition.Status == corev1.ConditionTrue && !condition.LastTransitionTime.IsZero() {
				initializedTime = &condition.LastTransitionTime.Time
			}
		case corev1.PodReady:
			if condition.Status == corev1.ConditionTrue && !condition.LastTransitionTime.IsZero() {
				readyTime = &condition.LastTransitionTime.Time
			}
		case corev1.PodScheduled:
			if condition.Status == corev1.ConditionTrue && !condition.LastTransitionTime.IsZero() {
				scheduledTime = &condition.LastTransitionTime.Time
			}
		}
	}

	statusReason := ""
	if pod.Status.Reason != "" {
		statusReason = pod.Status.Reason
	} else {
		for _, condition := range pod.Status.Conditions {
			if condition.Status == corev1.ConditionFalse && condition.Reason != "" {
				statusReason = condition.Reason
				break
			}
		}
		if statusReason == "" {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
					statusReason = string(cs.State.Terminated.Reason)
					break
				}
			}
		}
	}

	var unschedulable *bool
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
			val := true
			unschedulable = &val
			break
		}
	}

	var podIPs []string
	if pod.Status.PodIP != "" {
		podIPs = append(podIPs, pod.Status.PodIP)
	}
	for _, ip := range pod.Status.PodIPs {
		if ip.IP != "" {
			podIPs = append(podIPs, ip.IP)
		}
	}

	var tolerations []types.TolerationData
	for _, toleration := range pod.Spec.Tolerations {
		tolerationData := types.TolerationData{
			Key:      toleration.Key,
			Value:    toleration.Value,
			Effect:   string(toleration.Effect),
			Operator: string(toleration.Operator),
		}
		if toleration.TolerationSeconds != nil {
			tolerationData.TolerationSeconds = strconv.FormatInt(*toleration.TolerationSeconds, 10)
		}
		tolerations = append(tolerations, tolerationData)
	}

	var pvcs []types.PVCData
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			readOnly := false
			for _, mount := range pod.Spec.Containers {
				for _, volumeMount := range mount.VolumeMounts {
					if volumeMount.Name == volume.Name && volumeMount.ReadOnly {
						readOnly = true
						break
					}
				}
			}
			for _, mount := range pod.Spec.InitContainers {
				for _, volumeMount := range mount.VolumeMounts {
					if volumeMount.Name == volume.Name && volumeMount.ReadOnly {
						readOnly = true
						break
					}
				}
			}
			pvcs = append(pvcs, types.PVCData{
				ClaimName: volume.PersistentVolumeClaim.ClaimName,
				ReadOnly:  readOnly,
			})
		}
	}

	overheadCPUCores := ""
	overheadMemoryBytes := ""
	if pod.Spec.Overhead != nil {
		if cpu := pod.Spec.Overhead[corev1.ResourceCPU]; !cpu.IsZero() {
			overheadCPUCores = cpu.String()
		}
		if memory := pod.Spec.Overhead[corev1.ResourceMemory]; !memory.IsZero() {
			overheadMemoryBytes = memory.String()
		}
	}

	runtimeClassName := ""
	if pod.Spec.RuntimeClassName != nil {
		runtimeClassName = *pod.Spec.RuntimeClassName
	}

	var completionTime *time.Time
	if pod.Status.Phase == corev1.PodSucceeded {
		for _, container := range pod.Status.ContainerStatuses {
			if container.State.Terminated != nil && !container.State.Terminated.FinishedAt.IsZero() {
				if completionTime == nil || container.State.Terminated.FinishedAt.Time.After(*completionTime) {
					completionTime = &container.State.Terminated.FinishedAt.Time
				}
			}
		}
		if completionTime == nil {
			now := time.Now()
			completionTime = &now
		}
	}

	containerMetrics := aggregateContainerResources(pod)

	data := types.PodData{
		ControllerCreatedResourceMetadata: types.ControllerCreatedResourceMetadata{
			NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
				NamespacedMetadata: types.NamespacedMetadata{
					BaseMetadata: types.BaseMetadata{
						Timestamp:        time.Now(),
						ResourceType:     "pod",
						Name:             utils.ExtractName(pod),
						CreatedTimestamp: utils.ExtractCreationTimestamp(pod),
					},
					Namespace: utils.ExtractNamespace(pod),
				},
				LabeledMetadata: types.LabeledMetadata{
					Labels:      utils.ExtractLabels(pod),
					Annotations: utils.ExtractAnnotations(pod),
				},
			},
			ControllerCreatedMetadata: types.ControllerCreatedMetadata{
				CreatedByKind: createdByKind,
				CreatedByName: createdByName,
			},
		},
		PodUID:                    string(pod.UID),
		NodeName:                  pod.Spec.NodeName,
		HostIP:                    pod.Status.HostIP,
		PodIP:                     pod.Status.PodIP,
		Phase:                     string(pod.Status.Phase),
		QoSClass:                  qosClass,
		Priority:                  pod.Spec.Priority,
		PriorityClass:             priorityClass,
		Ready:                     conditionReady,
		Initialized:               conditionInitialized,
		Scheduled:                 conditionScheduled,
		ContainersReady:           conditionContainersReady,
		PodScheduled:              conditionScheduled,
		Conditions:                conditions,
		RestartCount:              totalRestartCount,
		DeletionTimestamp:         deletionTimestamp,
		StartTime:                 startTime,
		InitializedTime:           initializedTime,
		ReadyTime:                 readyTime,
		ScheduledTime:             scheduledTime,
		StatusReason:              statusReason,
		Unschedulable:             unschedulable,
		RestartPolicy:             string(pod.Spec.RestartPolicy),
		ServiceAccount:            pod.Spec.ServiceAccountName,
		SchedulerName:             pod.Spec.SchedulerName,
		OverheadCPUCores:          overheadCPUCores,
		OverheadMemoryBytes:       overheadMemoryBytes,
		RuntimeClassName:          runtimeClassName,
		PodIPs:                    podIPs,
		Tolerations:               tolerations,
		NodeSelectors:             pod.Spec.NodeSelector,
		PersistentVolumeClaims:    pvcs,
		CompletionTime:            completionTime,
		ContainerCount:            containerMetrics.ContainerCount,
		InitContainerCount:        containerMetrics.InitContainerCount,
		TotalRequestsCPUMillicore: containerMetrics.TotalRequestsCPUMillicore,
		TotalRequestsMemoryBytes:  containerMetrics.TotalRequestsMemoryBytes,
		TotalLimitsCPUMillicore:   containerMetrics.TotalLimitsCPUMillicore,
		TotalLimitsMemoryBytes:    containerMetrics.TotalLimitsMemoryBytes,
	}

	return data
}
