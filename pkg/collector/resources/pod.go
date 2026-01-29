package resources

import (
	"context"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// PodHandler handles collection of pod metrics
type PodHandler struct {
	utils.BaseHandler
}

// containerResourceAggregation holds aggregated resource values for a pod's containers
type containerResourceAggregation struct {
	ContainerCount            int32
	InitContainerCount        int32
	TotalRequestsCPUMillicore *int64
	TotalRequestsMemoryBytes  *int64
	TotalLimitsCPUMillicore   *int64
	TotalLimitsMemoryBytes    *int64
}

// NewPodHandler creates a new PodHandler
func NewPodHandler(client kubernetes.Interface) *PodHandler {
	return &PodHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the pod informer
func (h *PodHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create pod informer
	informer := factory.Core().V1().Pods().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers pod metrics from the cluster (uses cache)
func (h *PodHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all pods from the cache
	pods := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range pods {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, pod.Namespace) {
			continue
		}

		entry := h.createLogEntry(pod)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a PodData from a pod
func (h *PodHandler) createLogEntry(pod *corev1.Pod) types.PodData {
	return CreatePodLogEntry(pod)
}

// CreatePodLogEntry creates a PodData from a pod.
// This is exported for use by the kubelet handler.
func CreatePodLogEntry(pod *corev1.Pod) types.PodData {
	// Determine QoS class
	qosClass := string(pod.Status.QOSClass)
	if qosClass == "" {
		qosClass = "BestEffort" // Default QoS class when not set
		// See: https://kubernetes.io/docs/concepts/workloads/pods/pod-qos/#qos-classes
	}

	// Get priority class
	priorityClass := ""
	if pod.Spec.PriorityClassName != "" {
		priorityClass = pod.Spec.PriorityClassName
	}

	createdByKind, createdByName := utils.GetOwnerReferenceInfo(pod)

	// Check conditions in a single loop
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
			// Add unknown conditions to the map
			conditions[string(condition.Type)] = val
		}
	}

	// Calculate total restart count
	var totalRestartCount int32
	for _, container := range pod.Status.ContainerStatuses {
		totalRestartCount += container.RestartCount
	}

	// Get timestamps
	var deletionTimestamp, startTime, initializedTime, readyTime, scheduledTime *time.Time
	if pod.DeletionTimestamp != nil {
		deletionTimestamp = &pod.DeletionTimestamp.Time
	}
	if pod.Status.StartTime != nil && !pod.Status.StartTime.IsZero() {
		startTime = &pod.Status.StartTime.Time
	}

	// Get condition timestamps
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

	// Get status reason - match kube-state-metrics logic
	// See: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-phase
	statusReason := ""
	if pod.Status.Reason != "" {
		statusReason = pod.Status.Reason
	} else {
		// Check conditions for reason
		for _, condition := range pod.Status.Conditions {
			if condition.Status == corev1.ConditionFalse && condition.Reason != "" {
				statusReason = condition.Reason
				break
			}
		}
		// Check container statuses for terminated reasons
		if statusReason == "" {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
					statusReason = string(cs.State.Terminated.Reason)
					break
				}
			}
		}
	}

	// Get unschedulable status
	var unschedulable *bool
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
			val := true
			unschedulable = &val
			break
		}
	}

	// Get pod IPs
	var podIPs []string
	if pod.Status.PodIP != "" {
		podIPs = append(podIPs, pod.Status.PodIP)
	}
	for _, ip := range pod.Status.PodIPs {
		if ip.IP != "" {
			podIPs = append(podIPs, ip.IP)
		}
	}

	// Get tolerations
	var tolerations []types.TolerationData
	for _, toleration := range pod.Spec.Tolerations {
		tolerationData := types.TolerationData{
			Key:      toleration.Key,
			Value:    toleration.Value,
			Effect:   string(toleration.Effect),
			Operator: string(toleration.Operator),
		}

		// Add toleration seconds if present
		if toleration.TolerationSeconds != nil {
			tolerationData.TolerationSeconds = strconv.FormatInt(*toleration.TolerationSeconds, 10)
		}

		tolerations = append(tolerations, tolerationData)
	}

	// Get PVC info
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

	// Get overhead
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

	// Get runtime class name
	runtimeClassName := ""
	if pod.Spec.RuntimeClassName != nil {
		runtimeClassName = *pod.Spec.RuntimeClassName
	}

	// Get completion time (when pod phase is Succeeded)
	var completionTime *time.Time
	if pod.Status.Phase == corev1.PodSucceeded {
		// For succeeded pods, look for the latest container termination time
		for _, container := range pod.Status.ContainerStatuses {
			if container.State.Terminated != nil && !container.State.Terminated.FinishedAt.IsZero() {
				if completionTime == nil || container.State.Terminated.FinishedAt.Time.After(*completionTime) {
					completionTime = &container.State.Terminated.FinishedAt.Time
				}
			}
		}
		// If no container termination time found, use current time as fallback
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

// aggregateContainerResources calculates total pod resources using Kubernetes scheduler logic:
// - Init containers: max resource (they run sequentially)
// - Regular containers: sum resources (they run in parallel)
func aggregateContainerResources(pod *corev1.Pod) containerResourceAggregation {
	result := containerResourceAggregation{
		ContainerCount:     int32(len(pod.Spec.Containers)),
		InitContainerCount: int32(len(pod.Spec.InitContainers)),
	}

	var maxInitCPU, maxInitMemory, maxInitCPULimit, maxInitMemoryLimit int64
	for _, container := range pod.Spec.InitContainers {
		if cpu := utils.ExtractCPUMillicores(container.Resources.Requests); cpu != nil && *cpu > maxInitCPU {
			maxInitCPU = *cpu
		}
		if memory := utils.ExtractMemoryBytes(container.Resources.Requests); memory != nil && *memory > maxInitMemory {
			maxInitMemory = *memory
		}
		if cpu := utils.ExtractCPUMillicores(container.Resources.Limits); cpu != nil && *cpu > maxInitCPULimit {
			maxInitCPULimit = *cpu
		}
		if memory := utils.ExtractMemoryBytes(container.Resources.Limits); memory != nil && *memory > maxInitMemoryLimit {
			maxInitMemoryLimit = *memory
		}
	}

	var sumRegularCPU, sumRegularMemory, sumRegularCPULimit, sumRegularMemoryLimit int64
	for _, container := range pod.Spec.Containers {
		if cpu := utils.ExtractCPUMillicores(container.Resources.Requests); cpu != nil {
			sumRegularCPU += *cpu
		}
		if memory := utils.ExtractMemoryBytes(container.Resources.Requests); memory != nil {
			sumRegularMemory += *memory
		}
		if cpu := utils.ExtractCPUMillicores(container.Resources.Limits); cpu != nil {
			sumRegularCPULimit += *cpu
		}
		if memory := utils.ExtractMemoryBytes(container.Resources.Limits); memory != nil {
			sumRegularMemoryLimit += *memory
		}
	}

	// Total is the max of (init container max, regular container sum)
	// Following Kubernetes scheduler logic: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/#resources
	// Only set if there are any containers (init or regular)
	if result.ContainerCount > 0 || result.InitContainerCount > 0 {
		if maxInitCPU > sumRegularCPU {
			result.TotalRequestsCPUMillicore = &maxInitCPU
		} else {
			result.TotalRequestsCPUMillicore = &sumRegularCPU
		}

		if maxInitMemory > sumRegularMemory {
			result.TotalRequestsMemoryBytes = &maxInitMemory
		} else {
			result.TotalRequestsMemoryBytes = &sumRegularMemory
		}

		if maxInitCPULimit > sumRegularCPULimit {
			result.TotalLimitsCPUMillicore = &maxInitCPULimit
		} else {
			result.TotalLimitsCPUMillicore = &sumRegularCPULimit
		}

		if maxInitMemoryLimit > sumRegularMemoryLimit {
			result.TotalLimitsMemoryBytes = &maxInitMemoryLimit
		} else {
			result.TotalLimitsMemoryBytes = &sumRegularMemoryLimit
		}
	}

	return result
}
