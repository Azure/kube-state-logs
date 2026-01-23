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
Generate log-keys annotation from resources list
*/}}
{{- define "kube-state-logs.logKeysAnnotation" -}}
{{- $annotation := "" -}}
{{- $adxMonDestination := .Values.config.adxMonLogDestination -}}
{{- range $index, $resource := .Values.config.resources -}}
{{- if $index -}}{{$annotation = printf "%s," $annotation}}{{- end -}}
{{- $snapshotName := include "kube-state-logs.resourceSnapshotName" $resource -}}
{{- $annotation = printf "%sResourceType:%s:%s:%s%s" $annotation $resource $adxMonDestination "Kube" $snapshotName -}}
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