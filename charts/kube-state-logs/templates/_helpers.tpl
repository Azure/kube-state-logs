{{/*
Expand the name of the chart.
*/}}
{{- define "kube-state-logs.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "kube-state-logs.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kube-state-logs.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kube-state-logs.labels" -}}
helm.sh/chart: {{ include "kube-state-logs.chart" . }}
{{ include "kube-state-logs.selectorLabels" . }}
{{- if and .Chart.AppVersion (not (contains "$" .Chart.AppVersion)) }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kube-state-logs.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kube-state-logs.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Keep synchronized with config.AllResourceList. */}}
{{- define "kube-state-logs.allResources" -}}
pod,container,service,node,deployment,job,cronjob,configmap,secret,persistentvolumeclaim,ingress,horizontalpodautoscaler,serviceaccount,endpoints,persistentvolume,resourcequota,poddisruptionbudget,storageclass,networkpolicy,replicationcontroller,limitrange,lease,role,clusterrole,rolebinding,clusterrolebinding,volumeattachment,certificatesigningrequest,namespace,daemonset,statefulset,replicaset,mutatingwebhookconfiguration,validatingwebhookconfiguration,ingressclass,priorityclass,runtimeclass,validatingadmissionpolicy,validatingadmissionpolicybinding,crd
{{- end }}

{{/* Resources collected by each node-local DaemonSet pod. */}}
{{- define "kube-state-logs.nodeResources" -}}
{{- $nodeResources := list -}}
{{- $configuredResources := .Values.config.resources | toStrings -}}
{{- if or (has "pod" $configuredResources) (has "all" $configuredResources) -}}
{{- $nodeResources = append $nodeResources "pod" -}}
{{- end -}}
{{- if or (has "container" $configuredResources) (has "all" $configuredResources) -}}
{{- $nodeResources = append $nodeResources "container" -}}
{{- end -}}
{{- $nodeResources | join "," -}}
{{- end }}

{{/* Whether advanced mode needs a node-local DaemonSet and its RBAC. */}}
{{- define "kube-state-logs.hasNodeResources" -}}
{{- if ne (include "kube-state-logs.nodeResources" .) "" -}}true{{- else -}}false{{- end -}}
{{- end }}

{{/* Whether an enabled node-local resource requests promoted node labels. */}}
{{- define "kube-state-logs.nodePromotesLabels" -}}
{{- $promotes := false -}}
{{- $resources := .Values.config.resources | toStrings -}}
{{- range $resourceConfig := .Values.config.resourceConfigs -}}
{{- $configText := toString $resourceConfig -}}
{{- $name := trim (first (splitList ":" $configText)) -}}
{{- if and (or (has $name $resources) (has "all" $resources)) (or (eq $name "pod") (eq $name "container")) (contains "promote-node-labels=" $configText) -}}
{{- $promotes = true -}}
{{- end -}}
{{- end -}}
{{- if $promotes -}}true{{- else -}}false{{- end -}}
{{- end }}

{{/*
Resources for the cluster deployment in advanced mode. Containers are handled
only after scheduling, by the DaemonSet. Pods are retained for the separately
filtered unscheduled-pod informer.
*/}}
{{- define "kube-state-logs.clusterResources" -}}
{{- $clusterResources := list -}}
{{- $configuredResources := .Values.config.resources | toStrings -}}
{{- $partitionedResources := $configuredResources -}}
{{- if has "all" $configuredResources -}}
{{- $partitionedResources = splitList "," (include "kube-state-logs.allResources" .) -}}
{{- end -}}
{{- range $partitionedResources -}}
{{- if and (ne . "pod") (ne . "container") (ne . "all") -}}
{{- $clusterResources = append $clusterResources . -}}
{{- end -}}
{{- end -}}
{{- if or (has "pod" $configuredResources) (has "all" $configuredResources) -}}
{{- $clusterResources = append $clusterResources "pod" -}}
{{- end -}}
{{- $clusterResources | join "," -}}
{{- end }}

{{/* Per-resource settings used by the node-local component. */}}
{{- define "kube-state-logs.nodeResourceConfigs" -}}
{{- $configs := list -}}
{{- $resources := .Values.config.resources | toStrings -}}
{{- range $config := .Values.config.resourceConfigs -}}
{{- $name := trim (first (splitList ":" (toString $config))) -}}
{{- if and (or (eq $name "pod") (eq $name "container")) (or (has $name $resources) (has "all" $resources)) -}}
{{- $configs = append $configs $config -}}
{{- end -}}
{{- end -}}
{{- $configs | join "," -}}
{{- end }}

{{/* Per-resource settings used by the cluster component. */}}
{{- define "kube-state-logs.clusterResourceConfigs" -}}
{{- $configs := list -}}
{{- $resources := .Values.config.resources | toStrings -}}
{{- range $config := .Values.config.resourceConfigs -}}
{{- $name := trim (first (splitList ":" (toString $config))) -}}
{{- if and (ne $name "container") (or (has $name $resources) (has "all" $resources)) -}}
{{- $configs = append $configs $config -}}
{{- end -}}
{{- end -}}
{{- $configs | join "," -}}
{{- end }}

{{/*
Convert resource name to proper snapshot name
*/}}
{{- define "kube-state-logs.resourceSnapshotName" -}}
{{- $resource := . -}}
{{- if eq $resource "pod" -}}PodSnapshot
{{- else if eq $resource "container" -}}ContainerSnapshot
{{- else if eq $resource "service" -}}ServiceSnapshot
{{- else if eq $resource "node" -}}NodeSnapshot
{{- else if eq $resource "deployment" -}}DeploymentSnapshot
{{- else if eq $resource "job" -}}JobSnapshot
{{- else if eq $resource "cronjob" -}}CronJobSnapshot
{{- else if eq $resource "configmap" -}}ConfigMapSnapshot
{{- else if eq $resource "secret" -}}SecretSnapshot
{{- else if eq $resource "statefulset" -}}StatefulSetSnapshot
{{- else if eq $resource "persistentvolumeclaim" -}}PersistentVolumeClaimSnapshot
{{- else if eq $resource "ingress" -}}IngressSnapshot
{{- else if eq $resource "horizontalpodautoscaler" -}}HorizontalPodAutoscalerSnapshot
{{- else if eq $resource "serviceaccount" -}}ServiceAccountSnapshot
{{- else if eq $resource "endpoints" -}}EndpointsSnapshot
{{- else if eq $resource "persistentvolume" -}}PersistentVolumeSnapshot
{{- else if eq $resource "resourcequota" -}}ResourceQuotaSnapshot
{{- else if eq $resource "poddisruptionbudget" -}}PodDisruptionBudgetSnapshot
{{- else if eq $resource "storageclass" -}}StorageClassSnapshot
{{- else if eq $resource "networkpolicy" -}}NetworkPolicySnapshot
{{- else if eq $resource "replicaset" -}}ReplicaSetSnapshot
{{- else if eq $resource "replicationcontroller" -}}ReplicationControllerSnapshot
{{- else if eq $resource "limitrange" -}}LimitRangeSnapshot
{{- else if eq $resource "lease" -}}LeaseSnapshot
{{- else if eq $resource "role" -}}RoleSnapshot
{{- else if eq $resource "clusterrole" -}}ClusterRoleSnapshot
{{- else if eq $resource "rolebinding" -}}RoleBindingSnapshot
{{- else if eq $resource "clusterrolebinding" -}}ClusterRoleBindingSnapshot
{{- else if eq $resource "volumeattachment" -}}VolumeAttachmentSnapshot
{{- else if eq $resource "certificatesigningrequest" -}}CertificateSigningRequestSnapshot
{{- else if eq $resource "mutatingwebhookconfiguration" -}}MutatingWebhookConfigurationSnapshot
{{- else if eq $resource "validatingwebhookconfiguration" -}}ValidatingWebhookConfigurationSnapshot
{{- else if eq $resource "ingressclass" -}}IngressClassSnapshot
{{- else -}}{{$resource | title}}Snapshot
{{- end -}}
{{- end }}

{{/*
Generate log-keys annotation from resources list (used in simple mode)
*/}}
{{- define "kube-state-logs.logKeysAnnotation" -}}
{{- $annotation := "" -}}
{{- $adxMonDestination := .Values.config.adxMonLogDestination -}}
{{- $logResources := .Values.config.resources | toStrings -}}
{{- if has "all" $logResources -}}
{{- $logResources = splitList "," (include "kube-state-logs.allResources" .) -}}
{{- end -}}
{{- range $index, $resource := $logResources -}}
{{- if $index -}}{{$annotation = printf "%s," $annotation}}{{- end -}}
{{- $snapshotName := include "kube-state-logs.resourceSnapshotName" $resource -}}
{{- $annotation = printf "%sResourceType:%s:%s:%s%s" $annotation $resource $adxMonDestination "Kube" $snapshotName -}}
{{- end -}}
{{- /* Add init_container routing to KubeContainerSnapshot when container resource is enabled */ -}}
{{- if has "container" $logResources -}}
{{- $annotation = printf "%s,ResourceType:init_container:%s:KubeContainerSnapshot" $annotation $adxMonDestination -}}
{{- end -}}
{{- /* Add CRD configurations to log-keys annotation */ -}}
{{- if .Values.config.crdConfigs -}}
{{- range .Values.config.crdConfigs -}}
{{- $resourceType := .kind | lower -}}
{{- $tableName := printf "Kube%sSnapshot" .kind -}}
{{- $annotation = printf "%s,ResourceType:%s:%s:%s" $annotation $resourceType $adxMonDestination $tableName -}}
{{- end -}}
{{- end -}}
{{- $annotation -}}
{{- end }}

{{/*
Generate log-keys annotation for node DaemonSet (advanced mode - pod and container only)
*/}}
{{- define "kube-state-logs.nodeLogKeysAnnotation" -}}
{{- $adxMonDestination := .Values.config.adxMonLogDestination -}}
{{- $resources := .Values.config.resources | toStrings -}}
{{- $routes := list -}}
{{- if or (has "pod" $resources) (has "all" $resources) -}}
{{- $routes = append $routes (printf "ResourceType:pod:%s:KubePodSnapshot" $adxMonDestination) -}}
{{- end -}}
{{- if or (has "container" $resources) (has "all" $resources) -}}
{{- $routes = append $routes (printf "ResourceType:container:%s:KubeContainerSnapshot" $adxMonDestination) -}}
{{- $routes = append $routes (printf "ResourceType:init_container:%s:KubeContainerSnapshot" $adxMonDestination) -}}
{{- end -}}
{{- $routes | join "," -}}
{{- end }}

{{/*
Generate log-keys annotation for cluster Deployment (advanced mode - all resources, including pod/container for unscheduled)
*/}}
{{- define "kube-state-logs.clusterLogKeysAnnotation" -}}
{{- $annotation := "" -}}
{{- $adxMonDestination := .Values.config.adxMonLogDestination -}}
{{- $first := true -}}
{{- $clusterResources := splitList "," (include "kube-state-logs.clusterResources" .) -}}
{{- range $resource := $clusterResources -}}
{{- if $resource -}}
{{- if not $first -}}{{$annotation = printf "%s," $annotation}}{{- end -}}
{{- $first = false -}}
{{- $snapshotName := include "kube-state-logs.resourceSnapshotName" $resource -}}
{{- $annotation = printf "%sResourceType:%s:%s:%s%s" $annotation $resource $adxMonDestination "Kube" $snapshotName -}}
{{- end -}}
{{- end -}}
{{- /* Add CRD configurations to log-keys annotation */ -}}
{{- if .Values.config.crdConfigs -}}
{{- range .Values.config.crdConfigs -}}
{{- $resourceType := .kind | lower -}}
{{- $tableName := printf "Kube%sSnapshot" .kind -}}
{{- $annotation = printf "%s,ResourceType:%s:%s:%s" $annotation $resourceType $adxMonDestination $tableName -}}
{{- end -}}
{{- end -}}
{{- $annotation -}}
{{- end }}