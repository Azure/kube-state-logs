// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package types

import (
	"encoding/json"
	"time"
)

// BaseMetadata contains the core metadata fields common to all resources
type BaseMetadata struct {
	Timestamp         time.Time  `json:"Timestamp"`
	ResourceType      string     `json:"ResourceType"`
	Name              string     `json:"Name"`
	CreatedTimestamp  time.Time  `json:"CreatedTimestamp"`
	EventType         string     `json:"EventType"`
	DeletionTimestamp *time.Time `json:"DeletionTimestamp,omitempty"`
}

// NamespacedMetadata extends BaseMetadata with namespace information for namespaced resources
type NamespacedMetadata struct {
	BaseMetadata
	Namespace string `json:"Namespace"`
}

// LabeledMetadata extends metadata with labels and annotations for resources that support them
type LabeledMetadata struct {
	Labels      map[string]string `json:"Labels"`
	Annotations map[string]string `json:"Annotations"`
}

// ControllerCreatedMetadata contains information about resources created by controllers
type ControllerCreatedMetadata struct {
	CreatedByKind string `json:"CreatedByKind"`
	CreatedByName string `json:"CreatedByName"`
}

// ClusterScopedMetadata combines base metadata with labels for cluster-scoped resources
type ClusterScopedMetadata struct {
	BaseMetadata
	LabeledMetadata
}

// NamespacedLabeledMetadata combines namespaced metadata with labels for most namespaced resources
type NamespacedLabeledMetadata struct {
	NamespacedMetadata
	LabeledMetadata
}

// ControllerCreatedResourceMetadata combines namespaced, labeled, and controller-created metadata
type ControllerCreatedResourceMetadata struct {
	NamespacedLabeledMetadata
	ControllerCreatedMetadata
}

// DeploymentData represents deployment-specific metrics (matching kube-state-metrics)
type DeploymentData struct {
	NamespacedLabeledMetadata
	// Replica counts (matching kube-state-metrics)
	DesiredReplicas     int32 `json:"DesiredReplicas"`
	CurrentReplicas     int32 `json:"CurrentReplicas"`
	ReadyReplicas       int32 `json:"ReadyReplicas"`
	AvailableReplicas   int32 `json:"AvailableReplicas"`
	UnavailableReplicas int32 `json:"UnavailableReplicas"`
	UpdatedReplicas     int32 `json:"UpdatedReplicas"`

	// Deployment status (matching kube-state-metrics)
	ObservedGeneration int64 `json:"ObservedGeneration"`
	CollisionCount     int32 `json:"CollisionCount"`

	// Strategy info (matching kube-state-metrics)
	StrategyType                        string `json:"StrategyType"`
	StrategyRollingUpdateMaxSurge       int32  `json:"StrategyRollingUpdateMaxSurge"`
	StrategyRollingUpdateMaxUnavailable int32  `json:"StrategyRollingUpdateMaxUnavailable"`

	// Conditions (matching kube-state-metrics)
	ConditionAvailable      *bool `json:"ConditionAvailable"`
	ConditionProgressing    *bool `json:"ConditionProgressing"`
	ConditionReplicaFailure *bool `json:"ConditionReplicaFailure"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// Spec fields (matching kube-state-metrics)
	Paused                  bool  `json:"Paused"`
	MinReadySeconds         int32 `json:"MinReadySeconds"`
	RevisionHistoryLimit    int32 `json:"RevisionHistoryLimit"`
	ProgressDeadlineSeconds int32 `json:"ProgressDeadlineSeconds"`

	// Metadata (matching kube-state-metrics)
	MetadataGeneration int64 `json:"MetadataGeneration"`
}

// PodData represents pod-specific metrics (matching kube-state-metrics)
type PodData struct {
	ControllerCreatedResourceMetadata
	// Basic pod info
	NodeName      string `json:"NodeName"`
	HostIP        string `json:"HostIP"`
	PodIP         string `json:"PodIP"`
	Phase         string `json:"Phase"`
	QoSClass      string `json:"QoSClass"`
	Priority      *int32 `json:"Priority"`
	PriorityClass string `json:"PriorityClass"`

	// Pod conditions
	Ready           *bool `json:"Ready"`
	Initialized     *bool `json:"Initialized"`
	Scheduled       *bool `json:"Scheduled"`
	ContainersReady *bool `json:"ContainersReady"`
	PodScheduled    *bool `json:"PodScheduled"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// Pod status
	RestartCount int32 `json:"RestartCount"`

	// Missing from KSM
	StartTime              *time.Time        `json:"StartTime"`
	InitializedTime        *time.Time        `json:"InitializedTime"`
	ReadyTime              *time.Time        `json:"ReadyTime"`
	ScheduledTime          *time.Time        `json:"ScheduledTime"`
	StatusReason           string            `json:"StatusReason"`
	Unschedulable          *bool             `json:"Unschedulable"`
	RestartPolicy          string            `json:"RestartPolicy"`
	ServiceAccount         string            `json:"ServiceAccount"`
	SchedulerName          string            `json:"SchedulerName"`
	OverheadCPUCores       string            `json:"OverheadCPUCores"`
	OverheadMemoryBytes    string            `json:"OverheadMemoryBytes"`
	RuntimeClassName       string            `json:"RuntimeClassName"`
	PodIPs                 []string          `json:"PodIPs"`
	Tolerations            []TolerationData  `json:"Tolerations"`
	NodeSelectors          map[string]string `json:"NodeSelectors"`
	PersistentVolumeClaims []PVCData         `json:"PersistentVolumeClaims"`
	CompletionTime         *time.Time        `json:"CompletionTime"`

	// Aggregated container resources (using scheduler logic: max of init, sum of regular)
	ContainerCount            int32  `json:"ContainerCount"`
	InitContainerCount        int32  `json:"InitContainerCount"`
	TotalRequestsCPUMillicore *int64 `json:"TotalRequestsCPUMillicore"`
	TotalRequestsMemoryBytes  *int64 `json:"TotalRequestsMemoryBytes"`
	TotalLimitsCPUMillicore   *int64 `json:"TotalLimitsCPUMillicore"`
	TotalLimitsMemoryBytes    *int64 `json:"TotalLimitsMemoryBytes"`
}

// TolerationData represents pod toleration information
type TolerationData struct {
	Key               string `json:"Key"`
	Value             string `json:"Value"`
	Effect            string `json:"Effect"`
	Operator          string `json:"Operator"`
	TolerationSeconds string `json:"TolerationSeconds"`
}

// PVCData represents persistent volume claim information
type PVCData struct {
	ClaimName string `json:"ClaimName"`
	ReadOnly  bool   `json:"ReadOnly"`
}

// ContainerData represents container-specific metrics (matching kube-state-metrics)
type ContainerData struct {
	NamespacedMetadata
	// Basic container info
	Image       string `json:"Image"`
	ImageID     string `json:"ImageID"`
	ContainerID string `json:"ContainerID"`
	PodName     string `json:"PodName"`
	NodeName    string `json:"NodeName"`

	// Container state
	Ready        *bool  `json:"Ready"`
	RestartCount int32  `json:"RestartCount"`
	State        string `json:"State"`

	// Current state details
	StateRunning    *bool `json:"StateRunning"`
	StateWaiting    *bool `json:"StateWaiting"`
	StateTerminated *bool `json:"StateTerminated"`

	// Waiting state details
	WaitingReason  string `json:"WaitingReason"`
	WaitingMessage string `json:"WaitingMessage"`

	// Running state details
	StartedAt *time.Time `json:"StartedAt"`

	// Terminated state details
	ExitCode      int32      `json:"ExitCode"`
	Reason        string     `json:"Reason"`
	Message       string     `json:"Message"`
	FinishedAt    *time.Time `json:"FinishedAt"`
	StartedAtTerm *time.Time `json:"StartedAtTerm"`

	// Resource requests/limits
	ResourceRequests map[string]string `json:"ResourceRequests"`
	ResourceLimits   map[string]string `json:"ResourceLimits"`

	// Specific CPU and memory resource fields
	RequestsCPUMillicore *int64 `json:"RequestsCPUMillicore"`
	RequestsMemoryBytes  *int64 `json:"RequestsMemoryBytes"`
	LimitsCPUMillicore   *int64 `json:"LimitsCPUMillicore"`
	LimitsMemoryBytes    *int64 `json:"LimitsMemoryBytes"`

	// Missing from KSM
	LastTerminatedReason    string     `json:"LastTerminatedReason"`
	LastTerminatedExitCode  int32      `json:"LastTerminatedExitCode"`
	LastTerminatedTimestamp *time.Time `json:"LastTerminatedTimestamp"`
	StateStarted            *time.Time `json:"StateStarted"`

	// Current usage metrics (point-in-time)
	UsageCPUMillicore *int64 `json:"UsageCPUMillicore"`
	UsageMemoryBytes  *int64 `json:"UsageMemoryBytes"`

	// EnvironmentVariables captures a filtered set of environment variables from the container spec.
	// Only variables explicitly configured via --container-envvars are included.
	EnvironmentVariables map[string]string `json:"EnvironmentVariables,omitempty"`
}

// ServiceData represents service-specific metrics (matching kube-state-metrics)
type ServiceData struct {
	NamespacedLabeledMetadata
	// Basic service info
	Type           string            `json:"Type"`
	ClusterIP      string            `json:"ClusterIP"`
	ExternalIP     string            `json:"ExternalIP"`
	LoadBalancerIP string            `json:"LoadBalancerIP"`
	Ports          []ServicePortData `json:"Ports"`
	Selector       map[string]string `json:"Selector"`

	// Service status
	EndpointsCount int `json:"EndpointsCount"`

	// Load balancer info
	LoadBalancerIngress []LoadBalancerIngressData `json:"LoadBalancerIngress"`

	// Session affinity
	SessionAffinity string `json:"SessionAffinity"`

	// External name
	ExternalName string `json:"ExternalName"`

	// Missing from KSM
	ExternalTrafficPolicy                 string `json:"ExternalTrafficPolicy"`
	SessionAffinityClientIPTimeoutSeconds int32  `json:"SessionAffinityClientIPTimeoutSeconds"`

	// Additional KSM fields we should track
	AllocateLoadBalancerNodePorts *bool    `json:"AllocateLoadBalancerNodePorts"`
	LoadBalancerClass             *string  `json:"LoadBalancerClass"`
	LoadBalancerSourceRanges      []string `json:"LoadBalancerSourceRanges"`
}

// ServicePortData represents service port information
type ServicePortData struct {
	Name       string `json:"Name"`
	Protocol   string `json:"Protocol"`
	Port       int32  `json:"Port"`
	TargetPort int32  `json:"TargetPort"`
	NodePort   int32  `json:"NodePort"`
}

// LoadBalancerIngressData represents load balancer ingress information
type LoadBalancerIngressData struct {
	IP       string `json:"IP"`
	Hostname string `json:"Hostname"`
}

// NodeData represents node-specific metrics (matching kube-state-metrics)
type NodeData struct {
	ClusterScopedMetadata
	// Basic node info
	Architecture            string `json:"Architecture"`
	OperatingSystem         string `json:"OperatingSystem"`
	KernelVersion           string `json:"KernelVersion"`
	KubeletVersion          string `json:"KubeletVersion"`
	KubeProxyVersion        string `json:"KubeProxyVersion"`
	ContainerRuntimeVersion string `json:"ContainerRuntimeVersion"`

	// Node status - common resources as top-level fields
	CapacityCPUMillicore    *int64 `json:"CapacityCPUMillicore"`
	CapacityMemoryBytes     *int64 `json:"CapacityMemoryBytes"`
	CapacityPods            *int64 `json:"CapacityPods"`
	AllocatableCPUMillicore *int64 `json:"AllocatableCPUMillicore"`
	AllocatableMemoryBytes  *int64 `json:"AllocatableMemoryBytes"`
	AllocatablePods         *int64 `json:"AllocatablePods"`

	// Other capacity and allocatable resources (excluding CPU, Memory, Pods)
	Capacity    map[string]string `json:"Capacity"`
	Allocatable map[string]string `json:"Allocatable"`
	Ready       *bool             `json:"Ready"`
	Phase       string            `json:"Phase"`

	// Current usage metrics (from metrics server)
	UsageCPUMillicore *int64 `json:"UsageCPUMillicore"`
	UsageMemoryBytes  *int64 `json:"UsageMemoryBytes"`

	// Node addresses
	InternalIP string `json:"InternalIP"`
	ExternalIP string `json:"ExternalIP"`
	Hostname   string `json:"Hostname"`

	// Node status details
	Unschedulable *bool `json:"Unschedulable"`

	// Missing from KSM
	Role   string      `json:"Role"`
	Taints []TaintData `json:"Taints"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`
}

// TaintData represents node taint information
type TaintData struct {
	Key    string `json:"Key"`
	Value  string `json:"Value"`
	Effect string `json:"Effect"`
}

// ReplicaSetData represents replicaset-specific metrics (matching kube-state-metrics)
type ReplicaSetData struct {
	ControllerCreatedResourceMetadata
	// Replica counts
	DesiredReplicas      int32 `json:"DesiredReplicas"`
	CurrentReplicas      int32 `json:"CurrentReplicas"`
	ReadyReplicas        int32 `json:"ReadyReplicas"`
	AvailableReplicas    int32 `json:"AvailableReplicas"`
	FullyLabeledReplicas int32 `json:"FullyLabeledReplicas"`

	// Replicaset status
	ObservedGeneration int64 `json:"ObservedGeneration"`

	// Most common conditions (for easy access)
	ConditionAvailable      *bool `json:"ConditionAvailable"`
	ConditionProgressing    *bool `json:"ConditionProgressing"`
	ConditionReplicaFailure *bool `json:"ConditionReplicaFailure"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`
}

// StatefulSetData represents statefulset-specific metrics (matching kube-state-metrics)
type StatefulSetData struct {
	NamespacedLabeledMetadata
	// Replica counts
	DesiredReplicas int32 `json:"DesiredReplicas"`
	CurrentReplicas int32 `json:"CurrentReplicas"`
	ReadyReplicas   int32 `json:"ReadyReplicas"`
	UpdatedReplicas int32 `json:"UpdatedReplicas"`

	// Statefulset status
	ObservedGeneration int64  `json:"ObservedGeneration"`
	CurrentRevision    string `json:"CurrentRevision"`
	UpdateRevision     string `json:"UpdateRevision"`

	// Most common conditions (for easy access)
	ConditionAvailable      *bool `json:"ConditionAvailable"`
	ConditionProgressing    *bool `json:"ConditionProgressing"`
	ConditionReplicaFailure *bool `json:"ConditionReplicaFailure"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// Statefulset specific
	ServiceName         string `json:"ServiceName"`
	PodManagementPolicy string `json:"PodManagementPolicy"`
	UpdateStrategy      string `json:"UpdateStrategy"`
}

// DaemonSetData represents daemonset-specific metrics (matching kube-state-metrics)
type DaemonSetData struct {
	NamespacedLabeledMetadata
	// Replica counts
	DesiredNumberScheduled int32 `json:"DesiredNumberScheduled"`
	CurrentNumberScheduled int32 `json:"CurrentNumberScheduled"`
	NumberReady            int32 `json:"NumberReady"`
	NumberAvailable        int32 `json:"NumberAvailable"`
	NumberUnavailable      int32 `json:"NumberUnavailable"`
	NumberMisscheduled     int32 `json:"NumberMisscheduled"`
	UpdatedNumberScheduled int32 `json:"UpdatedNumberScheduled"`

	// Daemonset status
	ObservedGeneration int64 `json:"ObservedGeneration"`

	// Most common conditions (for easy access)
	ConditionAvailable      *bool `json:"ConditionAvailable"`
	ConditionProgressing    *bool `json:"ConditionProgressing"`
	ConditionReplicaFailure *bool `json:"ConditionReplicaFailure"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// Daemonset specific
	UpdateStrategy     string `json:"UpdateStrategy"`
	MetadataGeneration int64  `json:"MetadataGeneration"`
	CollisionCount     *int32 `json:"CollisionCount"`
}

// NamespaceData represents namespace-specific metrics (matching kube-state-metrics)
type NamespaceData struct {
	ClusterScopedMetadata
	// Namespace status
	Phase string `json:"Phase"`

	// Most common conditions (for easy access)
	ConditionActive      *bool `json:"ConditionActive"`
	ConditionTerminating *bool `json:"ConditionTerminating"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// Namespace specific
	Finalizers []string `json:"Finalizers"`
}

// JobData represents job-specific metrics (matching kube-state-metrics)
type JobData struct {
	ControllerCreatedResourceMetadata
	// Job status
	ActivePods    int32 `json:"ActivePods"`
	SucceededPods int32 `json:"SucceededPods"`
	FailedPods    int32 `json:"FailedPods"`

	// Job spec
	Completions           *int32 `json:"Completions"`
	Parallelism           *int32 `json:"Parallelism"`
	BackoffLimit          int32  `json:"BackoffLimit"`
	ActiveDeadlineSeconds *int64 `json:"ActiveDeadlineSeconds"`

	// Job conditions
	ConditionComplete *bool `json:"ConditionComplete"`
	ConditionFailed   *bool `json:"ConditionFailed"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// Job specific
	JobType string `json:"JobType"` // "Job" or "CronJob"
	Suspend *bool  `json:"Suspend"`
}

// CronJobData represents cronjob-specific metrics (matching kube-state-metrics)
type CronJobData struct {
	NamespacedLabeledMetadata
	// CronJob spec
	Schedule                   string `json:"Schedule"`
	ConcurrencyPolicy          string `json:"ConcurrencyPolicy"`
	Suspend                    *bool  `json:"Suspend"`
	SuccessfulJobsHistoryLimit *int32 `json:"SuccessfulJobsHistoryLimit"`
	FailedJobsHistoryLimit     *int32 `json:"FailedJobsHistoryLimit"`

	// CronJob status
	ActiveJobsCount int32 `json:"ActiveJobsCount"`

	// Last execution info
	LastScheduleTime *time.Time `json:"LastScheduleTime"`
	NextScheduleTime *time.Time `json:"NextScheduleTime"`

	// Conditions
	ConditionActive *bool `json:"ConditionActive"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`
}

// ConfigMapData represents configmap-specific metrics (matching kube-state-metrics)
type ConfigMapData struct {
	NamespacedLabeledMetadata
	// ConfigMap specific
	DataKeys []string `json:"DataKeys"`
}

// SecretData represents secret-specific metrics (matching kube-state-metrics)
type SecretData struct {
	NamespacedLabeledMetadata
	// Secret specific
	Type     string   `json:"Type"`
	DataKeys []string `json:"DataKeys"`
}

// PersistentVolumeClaimData represents persistentvolumeclaim-specific metrics (matching kube-state-metrics)
type PersistentVolumeClaimData struct {
	NamespacedLabeledMetadata
	// PVC spec
	AccessModes      []string `json:"AccessModes"`
	StorageClassName *string  `json:"StorageClassName"`
	VolumeName       string   `json:"VolumeName"`

	// PVC status
	Phase    string            `json:"Phase"`
	Capacity map[string]string `json:"Capacity"`

	// Conditions
	ConditionPending *bool `json:"ConditionPending"`
	ConditionBound   *bool `json:"ConditionBound"`
	ConditionLost    *bool `json:"ConditionLost"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// PVC specific
	RequestStorage string `json:"RequestStorage"`
	UsedStorage    string `json:"UsedStorage"`
}

// IngressData represents ingress-specific metrics (matching kube-state-metrics)
type IngressData struct {
	NamespacedLabeledMetadata
	// Ingress spec
	IngressClassName *string `json:"IngressClassName"`
	LoadBalancerIP   string  `json:"LoadBalancerIP"`

	// Ingress status
	LoadBalancerIngress []LoadBalancerIngressData `json:"LoadBalancerIngress"`

	// Ingress rules
	Rules []IngressRuleData `json:"Rules"`

	// TLS configuration
	TLS []IngressTLSData `json:"TLS"`

	// Conditions
	ConditionLoadBalancerReady *bool `json:"ConditionLoadBalancerReady"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`
}

// IngressRuleData represents ingress rule information
type IngressRuleData struct {
	Host  string            `json:"Host"`
	Paths []IngressPathData `json:"Paths"`
}

// IngressPathData represents ingress path information
type IngressPathData struct {
	Path     string `json:"Path"`
	PathType string `json:"PathType"`
	Service  string `json:"Service"`
	Port     string `json:"Port"`
}

// IngressTLSData represents ingress TLS configuration
type IngressTLSData struct {
	Hosts      []string `json:"Hosts"`
	SecretName string   `json:"SecretName"`
}

// HorizontalPodAutoscalerData represents horizontalpodautoscaler-specific metrics (matching kube-state-metrics)
type HorizontalPodAutoscalerData struct {
	NamespacedLabeledMetadata
	// HPA spec
	MinReplicas                       *int32 `json:"MinReplicas"`
	MaxReplicas                       int32  `json:"MaxReplicas"`
	TargetCPUUtilizationPercentage    *int32 `json:"TargetCPUUtilizationPercentage"`
	TargetMemoryUtilizationPercentage *int32 `json:"TargetMemoryUtilizationPercentage"`

	// HPA status
	CurrentReplicas                    int32  `json:"CurrentReplicas"`
	DesiredReplicas                    int32  `json:"DesiredReplicas"`
	CurrentCPUUtilizationPercentage    *int32 `json:"CurrentCPUUtilizationPercentage"`
	CurrentMemoryUtilizationPercentage *int32 `json:"CurrentMemoryUtilizationPercentage"`

	// Conditions
	ConditionAbleToScale    *bool `json:"ConditionAbleToScale"`
	ConditionScalingActive  *bool `json:"ConditionScalingActive"`
	ConditionScalingLimited *bool `json:"ConditionScalingLimited"`

	// All other conditions (excluding the top-level ones)
	Conditions map[string]*bool `json:"Conditions"`

	// HPA specific
	ScaleTargetRef  string `json:"ScaleTargetRef"`
	ScaleTargetKind string `json:"ScaleTargetKind"`
}

// ServiceAccountData represents serviceaccount-specific metrics (matching kube-state-metrics)
type ServiceAccountData struct {
	NamespacedLabeledMetadata
	// ServiceAccount specific
	Secrets          []string `json:"Secrets"`
	ImagePullSecrets []string `json:"ImagePullSecrets"`

	// ServiceAccount specific
	AutomountServiceAccountToken *bool `json:"AutomountServiceAccountToken"`
}

// EndpointsData represents endpoints-specific metrics (matching kube-state-metrics)
type EndpointsData struct {
	NamespacedLabeledMetadata
	// Endpoints specific
	Addresses []EndpointAddressData `json:"Addresses"`
	Ports     []EndpointPortData    `json:"Ports"`

	// Endpoints specific
	Ready *bool `json:"Ready"`
}

// EndpointAddressData represents endpoint address information
type EndpointAddressData struct {
	IP        string `json:"IP"`
	Hostname  string `json:"Hostname"`
	NodeName  string `json:"NodeName"`
	TargetRef string `json:"TargetRef"`
}

// EndpointPortData represents endpoint port information
type EndpointPortData struct {
	Name     string `json:"Name"`
	Protocol string `json:"Protocol"`
	Port     int32  `json:"Port"`
}

// PersistentVolumeData represents persistentvolume-specific metrics (matching kube-state-metrics)
type PersistentVolumeData struct {
	ClusterScopedMetadata
	// PersistentVolume specific
	CapacityBytes          int64  `json:"CapacityBytes"`
	AccessModes            string `json:"AccessModes"`
	ReclaimPolicy          string `json:"ReclaimPolicy"`
	Status                 string `json:"Status"`
	StorageClassName       string `json:"StorageClassName"`
	VolumeMode             string `json:"VolumeMode"`
	VolumePluginName       string `json:"VolumePluginName"`
	PersistentVolumeSource string `json:"PersistentVolumeSource"`

	// PersistentVolume specific
	IsDefaultClass bool `json:"IsDefaultClass"`
}

// ResourceQuotaData represents resourcequota-specific metrics (matching kube-state-metrics)
type ResourceQuotaData struct {
	NamespacedLabeledMetadata
	// ResourceQuota specific
	Hard map[string]int64 `json:"Hard"`
	Used map[string]int64 `json:"Used"`

	// ResourceQuota specific
	Scopes []string `json:"Scopes"`
}

// PodDisruptionBudgetData represents poddisruptionbudget-specific metrics (matching kube-state-metrics)
type PodDisruptionBudgetData struct {
	NamespacedLabeledMetadata
	// PodDisruptionBudget specific
	MinAvailable             int32 `json:"MinAvailable"`
	MaxUnavailable           int32 `json:"MaxUnavailable"`
	CurrentHealthy           int32 `json:"CurrentHealthy"`
	DesiredHealthy           int32 `json:"DesiredHealthy"`
	ExpectedPods             int32 `json:"ExpectedPods"`
	DisruptionsAllowed       int32 `json:"DisruptionsAllowed"`
	TotalReplicas            int32 `json:"TotalReplicas"`
	DisruptionAllowed        bool  `json:"DisruptionAllowed"`
	StatusCurrentHealthy     int32 `json:"StatusCurrentHealthy"`
	StatusDesiredHealthy     int32 `json:"StatusDesiredHealthy"`
	StatusExpectedPods       int32 `json:"StatusExpectedPods"`
	StatusDisruptionsAllowed int32 `json:"StatusDisruptionsAllowed"`
	StatusTotalReplicas      int32 `json:"StatusTotalReplicas"`
	StatusDisruptionAllowed  bool  `json:"StatusDisruptionAllowed"`
}

// CRDData represents CRD-specific metrics
type CRDData struct {
	NamespacedLabeledMetadata
	// CRD specific
	APIVersion   string         `json:"APIVersion"`
	CustomFields map[string]any `json:"-"` // Don't serialize this field normally
}

// MarshalJSON implements custom JSON marshaling for CRDData
// This flattens the CustomFields to the top level of the JSON output
func (c CRDData) MarshalJSON() ([]byte, error) {
	// Create a map to hold all the fields
	result := make(map[string]any)

	// Marshal the embedded struct to get its fields
	type Alias CRDData
	alias := Alias(c)
	alias.CustomFields = nil // Clear custom fields to avoid recursion

	// Use json.Marshal to get the base fields
	baseJSON, err := json.Marshal(alias)
	if err != nil {
		return nil, err
	}

	// Unmarshal base fields into our result map
	if err := json.Unmarshal(baseJSON, &result); err != nil {
		return nil, err
	}

	// Add custom fields at the top level, avoiding key collisions
	for key, value := range c.CustomFields {
		if _, exists := result[key]; !exists {
			result[key] = value
		}
	}

	// Marshal the final result
	return json.Marshal(result)
}

// StorageClassData represents storageclass-specific metrics (matching kube-state-metrics)
type StorageClassData struct {
	ClusterScopedMetadata
	// StorageClass specific
	Provisioner          string            `json:"Provisioner"`
	ReclaimPolicy        string            `json:"ReclaimPolicy"`
	VolumeBindingMode    string            `json:"VolumeBindingMode"`
	AllowVolumeExpansion bool              `json:"AllowVolumeExpansion"`
	Parameters           map[string]string `json:"Parameters"`
	MountOptions         []string          `json:"MountOptions"`
	AllowedTopologies    map[string]any    `json:"AllowedTopologies"`

	// StorageClass specific
	IsDefaultClass bool `json:"IsDefaultClass"`
}

// NetworkPolicyData represents networkpolicy-specific metrics (matching kube-state-metrics)
type NetworkPolicyData struct {
	NamespacedLabeledMetadata
	// NetworkPolicy specific
	PolicyTypes  []string                   `json:"PolicyTypes"`
	IngressRules []NetworkPolicyIngressRule `json:"IngressRules"`
	EgressRules  []NetworkPolicyEgressRule  `json:"EgressRules"`
}

// NetworkPolicyIngressRule represents an ingress rule in a network policy
type NetworkPolicyIngressRule struct {
	Ports []NetworkPolicyPort `json:"Ports"`
	From  []NetworkPolicyPeer `json:"From"`
}

// NetworkPolicyEgressRule represents an egress rule in a network policy
type NetworkPolicyEgressRule struct {
	Ports []NetworkPolicyPort `json:"Ports"`
	To    []NetworkPolicyPeer `json:"To"`
}

// NetworkPolicyPort represents a port in a network policy rule
type NetworkPolicyPort struct {
	Protocol string `json:"Protocol"`
	Port     int32  `json:"Port"`
	EndPort  int32  `json:"EndPort"`
}

// NetworkPolicyPeer represents a peer in a network policy rule
type NetworkPolicyPeer struct {
	PodSelector       map[string]string `json:"PodSelector"`
	NamespaceSelector map[string]string `json:"NamespaceSelector"`
	IPBlock           map[string]any    `json:"IPBlock"`
}

// ReplicationControllerData represents replicationcontroller-specific metrics (matching kube-state-metrics)
type ReplicationControllerData struct {
	NamespacedLabeledMetadata
	// ReplicationController specific
	DesiredReplicas      int32 `json:"DesiredReplicas"`
	CurrentReplicas      int32 `json:"CurrentReplicas"`
	ReadyReplicas        int32 `json:"ReadyReplicas"`
	AvailableReplicas    int32 `json:"AvailableReplicas"`
	FullyLabeledReplicas int32 `json:"FullyLabeledReplicas"`

	// ReplicationController specific
	ObservedGeneration int64 `json:"ObservedGeneration"`
}

// LimitRangeData represents limitrange-specific metrics (matching kube-state-metrics)
type LimitRangeData struct {
	NamespacedLabeledMetadata
	// LimitRange specific
	Limits []LimitRangeItem `json:"Limits"`
}

// LimitRangeItem represents a limit range item
type LimitRangeItem struct {
	Type                 string            `json:"Type"`
	ResourceType         string            `json:"ResourceType"`
	ResourceName         string            `json:"ResourceName"`
	Min                  map[string]string `json:"Min"`
	Max                  map[string]string `json:"Max"`
	Default              map[string]string `json:"Default"`
	DefaultRequest       map[string]string `json:"DefaultRequest"`
	MaxLimitRequestRatio map[string]string `json:"MaxLimitRequestRatio"`
}

// CertificateSigningRequestData represents certificatesigningrequest-specific metrics
type CertificateSigningRequestData struct {
	ClusterScopedMetadata
	// CertificateSigningRequest specific
	Status            string   `json:"Status"`
	SignerName        string   `json:"SignerName"`
	ExpirationSeconds *int32   `json:"ExpirationSeconds"`
	Usages            []string `json:"Usages"`
}

// PolicyRule represents a policy rule in RBAC
type PolicyRule struct {
	APIGroups       []string `json:"APIGroups"`
	Resources       []string `json:"Resources"`
	Verbs           []string `json:"Verbs"`
	ResourceNames   []string `json:"ResourceNames"`
	NonResourceURLs []string `json:"NonResourceURLs"`
}

// RoleData represents role-specific metrics
type RoleData struct {
	NamespacedLabeledMetadata
	// Role specific
	Rules []PolicyRule `json:"Rules"`
}

// ClusterRoleData represents clusterrole-specific metrics
type ClusterRoleData struct {
	ClusterScopedMetadata
	// ClusterRole specific
	Rules []PolicyRule `json:"Rules"`
}

// RoleRef represents a role reference in RBAC
type RoleRef struct {
	APIGroup string `json:"APIGroup"`
	Kind     string `json:"Kind"`
	Name     string `json:"Name"`
}

// Subject represents a subject in RBAC
type Subject struct {
	Kind      string `json:"Kind"`
	APIGroup  string `json:"APIGroup"`
	Name      string `json:"Name"`
	Namespace string `json:"Namespace"`
}

// RoleBindingData represents rolebinding-specific metrics
type RoleBindingData struct {
	NamespacedLabeledMetadata
	// RoleBinding specific
	RoleRef  RoleRef   `json:"RoleRef"`
	Subjects []Subject `json:"Subjects"`
}

// ClusterRoleBindingData represents clusterrolebinding-specific metrics
type ClusterRoleBindingData struct {
	ClusterScopedMetadata
	// ClusterRoleBinding specific
	RoleRef  RoleRef   `json:"RoleRef"`
	Subjects []Subject `json:"Subjects"`
}

// IngressClassData represents ingressclass-specific metrics
type IngressClassData struct {
	ClusterScopedMetadata
	// IngressClass specific
	Controller string `json:"Controller"`
	IsDefault  bool   `json:"IsDefault"`
}

// LeaseData represents lease-specific metrics
type LeaseData struct {
	NamespacedLabeledMetadata
	// Lease specific
	HolderIdentity       string     `json:"HolderIdentity"`
	LeaseDurationSeconds int32      `json:"LeaseDurationSeconds"`
	RenewTime            *time.Time `json:"RenewTime"`
	AcquireTime          *time.Time `json:"AcquireTime"`
	LeaseTransitions     int32      `json:"LeaseTransitions"`
}

// WebhookData represents webhook-specific metrics
type WebhookData struct {
	BaseMetadata
	// Webhook specific
	Name                    string                  `json:"Name"`
	ClientConfig            WebhookClientConfigData `json:"ClientConfig"`
	Rules                   []WebhookRuleData       `json:"Rules"`
	FailurePolicy           string                  `json:"FailurePolicy"`
	MatchPolicy             string                  `json:"MatchPolicy"`
	NamespaceSelector       map[string]string       `json:"NamespaceSelector"`
	ObjectSelector          map[string]string       `json:"ObjectSelector"`
	SideEffects             string                  `json:"SideEffects"`
	TimeoutSeconds          *int32                  `json:"TimeoutSeconds"`
	AdmissionReviewVersions []string                `json:"AdmissionReviewVersions"`
}

// WebhookClientConfigData represents webhook client config-specific metrics
type WebhookClientConfigData struct {
	BaseMetadata
	// WebhookClientConfig specific
	URL      string              `json:"URL"`
	Service  *WebhookServiceData `json:"Service"`
	CABundle []byte              `json:"CABundle"`
}

// WebhookServiceData represents webhook service-specific metrics
type WebhookServiceData struct {
	BaseMetadata
	// WebhookService specific
	Namespace string `json:"Namespace"`
	Name      string `json:"Name"`
	Path      string `json:"Path"`
	Port      int32  `json:"Port"`
}

// WebhookRuleData represents webhook rule-specific metrics
type WebhookRuleData struct {
	BaseMetadata
	// WebhookRule specific
	APIGroups   []string `json:"APIGroups"`
	APIVersions []string `json:"APIVersions"`
	Resources   []string `json:"Resources"`
	Scope       string   `json:"Scope"`
}

// MutatingWebhookConfigurationData represents mutatingwebhookconfiguration-specific metrics
type MutatingWebhookConfigurationData struct {
	ClusterScopedMetadata
	// MutatingWebhookConfiguration specific
	Webhooks []WebhookData `json:"Webhooks"`
}

// PriorityClassData represents priorityclass-specific metrics
type PriorityClassData struct {
	ClusterScopedMetadata
	// PriorityClass specific
	Value            int32  `json:"Value"`
	GlobalDefault    bool   `json:"GlobalDefault"`
	Description      string `json:"Description"`
	PreemptionPolicy string `json:"PreemptionPolicy"`
}

// RuntimeClassData represents runtimeclass-specific metrics
type RuntimeClassData struct {
	ClusterScopedMetadata
	// RuntimeClass specific
	Handler string `json:"Handler"`
}

// VolumeAttachmentData represents volumeattachment-specific metrics
type VolumeAttachmentData struct {
	ClusterScopedMetadata
	// VolumeAttachment specific
	Attacher   string `json:"Attacher"`
	VolumeName string `json:"VolumeName"`
	NodeName   string `json:"NodeName"`
	Attached   bool   `json:"Attached"`
}

// ValidatingAdmissionPolicyData represents validatingadmissionpolicy-specific metrics
type ValidatingAdmissionPolicyData struct {
	ClusterScopedMetadata
	FailurePolicy      string   `json:"FailurePolicy"`
	MatchConstraints   []string `json:"MatchConstraints"`
	Validations        []string `json:"Validations"`
	AuditAnnotations   []string `json:"AuditAnnotations"`
	MatchConditions    []string `json:"MatchConditions"`
	Variables          []string `json:"Variables"`
	ParamKind          string   `json:"ParamKind"`
	ObservedGeneration int64    `json:"ObservedGeneration"`
	TypeChecking       string   `json:"TypeChecking"`
	ExpressionWarnings []string `json:"ExpressionWarnings"`
}

// ValidatingAdmissionPolicyBindingData represents validatingadmissionpolicybinding-specific metrics
type ValidatingAdmissionPolicyBindingData struct {
	ClusterScopedMetadata
	PolicyName         string   `json:"PolicyName"`
	ParamRef           string   `json:"ParamRef"`
	MatchResources     []string `json:"MatchResources"`
	ValidationActions  []string `json:"ValidationActions"`
	ObservedGeneration int64    `json:"ObservedGeneration"`
}

// ValidatingWebhookConfigurationData represents validatingwebhookconfiguration-specific metrics
type ValidatingWebhookConfigurationData struct {
	ClusterScopedMetadata
	// ValidatingWebhookConfiguration specific
	Webhooks []WebhookData `json:"Webhooks"`
}
