// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package config

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

// DefaultResourceList contains every built-in typed resource handler.
const DefaultResourceList = "pod,container,service,node,deployment,job,cronjob,configmap,secret,persistentvolumeclaim,ingress,horizontalpodautoscaler,serviceaccount,endpoints,persistentvolume,resourcequota,poddisruptionbudget,storageclass,networkpolicy,replicationcontroller,limitrange,lease,role,clusterrole,rolebinding,clusterrolebinding,volumeattachment,certificatesigningrequest,namespace,daemonset,statefulset,replicaset,mutatingwebhookconfiguration,validatingwebhookconfiguration,ingressclass,priorityclass,runtimeclass,validatingadmissionpolicy,validatingadmissionpolicybinding"

// AllResourceList also enables configured custom-resource handlers.
const AllResourceList = DefaultResourceList + ",crd"

// ResourceConfig holds configuration for a specific resource type
type ResourceConfig struct {
	Name                string
	Interval            time.Duration
	LabelSelector       labels.Selector
	FieldSelector       fields.Selector
	NodeLabelsToPromote []string
}

// CRDConfig holds configuration for a specific CRD
type CRDConfig struct {
	APIVersion   string   // e.g., "apps/v1"
	Resource     string   // e.g., "deployments"
	CustomFields []string // e.g., ["spec.replicas", "spec.template.spec.containers"]
}

// Config holds the configuration for kube-state-logs
type Config struct {
	LogInterval     time.Duration
	Resources       []string
	ResourceConfigs []ResourceConfig // Individual resource configurations
	CRDs            []CRDConfig      // CRD configurations
	Namespaces      []string
	Kubeconfig      string
	// ContainerEnvVars is the list of environment variable names to capture from containers.
	// If empty, no environment variables will be collected.
	ContainerEnvVars []string
	// ConfigMapIncludeValues controls whether ConfigMap data values are logged.
	ConfigMapIncludeValues bool
	// Node filters pods to only those scheduled on this specific node (by node name).
	// Used for DaemonSet deployments where each pod logs only local pods.
	// If empty, no node filtering is applied.
	Node string
	// TrackUnscheduledPods when true, collects only pods that have not yet been scheduled
	// (spec.nodeName is empty). Used by the cluster deployment in advanced mode.
	TrackUnscheduledPods bool
	// UseKubeletAPI when true, uses the kubelet API instead of the Kubernetes API
	// for pod/container collection. Only applicable when Node is set (DaemonSet mode).
	// The kubelet API is polled since it doesn't support watches.
	UseKubeletAPI bool
	// KubeletPort is the port for the kubelet API (default: 10250).
	KubeletPort int
	// NodeIP is the IP address of the node for kubelet API access.
	// Required when UseKubeletAPI is true.
	NodeIP string
	// KubeletInsecureSkipVerify disables kubelet TLS certificate verification.
	// This may be required for self-signed serving certificates and should be
	// enabled only in trusted cluster networks.
	KubeletInsecureSkipVerify bool
}

// ParseResourceList parses a comma-separated string into a slice of resource types
func ParseResourceList(resources string) []string {
	if resources == "" {
		return []string{}
	}

	resourceList := strings.Split(resources, ",")

	// Parse the list, checking for "all" and trimming whitespace
	var result []string
	for _, resource := range resourceList {
		resource = strings.TrimSpace(resource)
		if resource == "all" {
			return ParseResourceList(AllResourceList)
		}
		if resource != "" {
			result = append(result, resource)
		}
	}

	return result
}

// ParseResourceConfigs parses a comma-separated string of resource configs.
// Format: "resource:interval[:labels=...][:fields=...][:promote-node-labels=...|...]" (comma-separated list).
// Use "\\," to escape commas inside selectors.
// Example: "configmap:1m:labels=app=foo\\,env=prod,pod:30s:fields=metadata.name=my-pod"
func ParseResourceConfigs(resourceConfigs string, defaultInterval time.Duration) ([]ResourceConfig, error) {
	if resourceConfigs == "" {
		return []ResourceConfig{}, nil
	}

	var configs []ResourceConfig
	pairs, err := splitResourceConfigs(resourceConfigs)
	if err != nil {
		return nil, err
	}

	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.Split(pair, ":")
		if len(parts) < 1 {
			continue
		}

		resourceName := strings.TrimSpace(parts[0])
		if resourceName == "" {
			return nil, fmt.Errorf("resource name cannot be empty in resource-configs")
		}

		labelSelector := labels.Everything()
		fieldSelector := fields.Everything()
		interval := defaultInterval
		var nodeLabelsToPromote []string

		intervalSet := false
		for i := 1; i < len(parts); i++ {
			segment := strings.TrimSpace(parts[i])
			if segment == "" {
				continue
			}
			switch {
			case strings.HasPrefix(segment, "labels="):
				value := strings.TrimPrefix(segment, "labels=")
				if value == "" {
					return nil, fmt.Errorf("labels selector cannot be empty for resource '%s'", resourceName)
				}
				parsed, err := labels.Parse(value)
				if err != nil {
					return nil, fmt.Errorf("invalid labels selector '%s' for resource '%s': %w", value, resourceName, err)
				}
				labelSelector = parsed
			case strings.HasPrefix(segment, "fields="):
				value := strings.TrimPrefix(segment, "fields=")
				if value == "" {
					return nil, fmt.Errorf("field selector cannot be empty for resource '%s'", resourceName)
				}
				parsed, err := fields.ParseSelector(value)
				if err != nil {
					return nil, fmt.Errorf("invalid field selector '%s' for resource '%s': %w", value, resourceName, err)
				}
				fieldSelector = parsed
			case strings.HasPrefix(segment, "promote-node-labels="):
				if resourceName != "pod" && resourceName != "container" {
					return nil, fmt.Errorf("promote-node-labels is only supported for resource 'pod' or 'container', not '%s'", resourceName)
				}
				value := strings.TrimPrefix(segment, "promote-node-labels=")
				if value == "" {
					return nil, fmt.Errorf("promote-node-labels cannot be empty for resource '%s'", resourceName)
				}
				for _, labelName := range strings.Split(value, "|") {
					labelName = strings.TrimSpace(labelName)
					if labelName == "" {
						return nil, fmt.Errorf("promote-node-labels contains an empty label for resource '%s'", resourceName)
					}
					nodeLabelsToPromote = append(nodeLabelsToPromote, labelName)
				}
			default:
				if intervalSet {
					return nil, fmt.Errorf("unexpected setting '%s' for resource '%s'", segment, resourceName)
				}
				parsedInterval, err := time.ParseDuration(segment)
				if err != nil {
					return nil, fmt.Errorf("invalid interval '%s' for resource '%s': %w", segment, resourceName, err)
				}
				interval = parsedInterval
				intervalSet = true
			}
		}

		configs = append(configs, ResourceConfig{
			Name:                resourceName,
			Interval:            interval,
			LabelSelector:       labelSelector,
			FieldSelector:       fieldSelector,
			NodeLabelsToPromote: nodeLabelsToPromote,
		})
	}

	return configs, nil
}

func splitResourceConfigs(resourceConfigs string) ([]string, error) {
	var parts []string
	var current strings.Builder
	escaped := false

	for _, r := range resourceConfigs {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}

		if r == ',' {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}

		current.WriteRune(r)
	}

	if escaped {
		return nil, fmt.Errorf("unterminated escape sequence in resource-configs")
	}

	parts = append(parts, current.String())
	return parts, nil
}

// ParseCRDConfigs parses a comma-separated string of CRD configurations
// Format: "apps/v1:deployments:spec.replicas,spec.template.spec.containers,networking.k8s.io/v1:ingresses:spec.rules"
func ParseCRDConfigs(crdConfigs string) []CRDConfig {
	if crdConfigs == "" {
		return []CRDConfig{}
	}

	var configs []CRDConfig
	pairs := strings.Split(crdConfigs, ",")

	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.Split(pair, ":")
		if len(parts) >= 2 {
			apiVersion := strings.TrimSpace(parts[0])
			resource := strings.TrimSpace(parts[1])

			var customFields []string
			if len(parts) > 2 {
				fieldsStr := strings.TrimSpace(parts[2])
				if fieldsStr != "" {
					customFields = strings.Split(fieldsStr, "|")
					// Trim spaces from each field
					for i, field := range customFields {
						customFields[i] = strings.TrimSpace(field)
					}
				}
			}

			configs = append(configs, CRDConfig{
				APIVersion:   apiVersion,
				Resource:     resource,
				CustomFields: customFields,
			})
		}
	}

	return configs
}

// GetResourceInterval returns the interval for a specific resource
func (c *Config) GetResourceInterval(resourceName string) time.Duration {
	for _, config := range c.ResourceConfigs {
		if config.Name == resourceName {
			return config.Interval
		}
	}
	// Fallback to default interval
	return c.LogInterval
}

// ParseNamespaceList parses a comma-separated string into a slice of namespace names
func ParseNamespaceList(namespaces string) []string {
	if namespaces == "" {
		return []string{}
	}
	return strings.Split(namespaces, ",")
}

// ParseContainerEnvVars parses a comma-separated list of environment variable names to capture.
// Empty string returns an empty slice (disabled).
func ParseContainerEnvVars(envvars string) []string {
	if envvars == "" {
		return []string{}
	}
	parts := strings.Split(envvars, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// PromotedNodeLabelsFor returns configured node labels only when node snapshots and the target snapshots are enabled.
func (c *Config) PromotedNodeLabelsFor(resourceName string) []string {
	if resourceName != "pod" && resourceName != "container" {
		return nil
	}

	nodeConfigured := false
	resourceConfigured := false
	for _, configuredResource := range c.Resources {
		switch configuredResource {
		case "node":
			nodeConfigured = true
		case resourceName:
			resourceConfigured = true
		}
	}
	if !resourceConfigured || (!nodeConfigured && c.Node == "") {
		return nil
	}

	var nodeLabelsToPromote []string
	for _, resourceConfig := range c.ResourceConfigs {
		if resourceConfig.Name == resourceName {
			nodeLabelsToPromote = resourceConfig.NodeLabelsToPromote
		}
	}

	return nodeLabelsToPromote
}

// SetLogLevel sets the klog verbosity level
func SetLogLevel(level string) error {
	switch strings.ToLower(level) {
	case "debug":
		klog.InitFlags(nil)
		// Set to maximum verbosity for debug
		klog.V(10).Info("Debug logging enabled")
	case "info":
		klog.InitFlags(nil)
		// Default level
	case "warn":
		klog.InitFlags(nil)
		// Reduce verbosity for warnings only
	case "error":
		klog.InitFlags(nil)
		// Only show errors
	default:
		return fmt.Errorf("invalid log level: %s", level)
	}
	return nil
}

// Validate checks the configuration for potential issues and fixes them
func (c *Config) Validate() error {
	// Validate LogInterval
	if c.LogInterval <= 0 {
		klog.Warningf("Invalid LogInterval %v, setting to default 1 minute", c.LogInterval)
		c.LogInterval = time.Minute
	}

	// Validate ResourceConfig intervals
	for i := range c.ResourceConfigs {
		if c.ResourceConfigs[i].Interval <= 0 {
			klog.Warningf("Invalid interval %v for resource %s, setting to default LogInterval %v",
				c.ResourceConfigs[i].Interval, c.ResourceConfigs[i].Name, c.LogInterval)
			c.ResourceConfigs[i].Interval = c.LogInterval
		}
	}

	// Validate kubelet API configuration
	if c.UseKubeletAPI {
		if c.Node == "" {
			return fmt.Errorf("--node is required when --use-kubelet-api is enabled")
		}
		if c.NodeIP == "" {
			return fmt.Errorf("--node-ip is required when --use-kubelet-api is enabled")
		}
		if c.KubeletPort <= 0 || c.KubeletPort > 65535 {
			klog.Warningf("Invalid kubelet port %d, setting to default 10250", c.KubeletPort)
			c.KubeletPort = 10250
		}
	}

	return nil
}
