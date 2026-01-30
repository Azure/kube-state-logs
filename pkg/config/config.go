// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package config

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

// ResourceConfig holds configuration for a specific resource type
type ResourceConfig struct {
	Name     string
	Interval time.Duration
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
			return []string{} // Return empty to use defaults
		}
		if resource != "" {
			result = append(result, resource)
		}
	}

	return result
}

// ParseResourceConfigs parses a comma-separated string of resource:interval pairs
// Format: "deployments:5m,pods:1m,services:2m"
func ParseResourceConfigs(resourceConfigs string, defaultInterval time.Duration) []ResourceConfig {
	if resourceConfigs == "" {
		return []ResourceConfig{}
	}

	var configs []ResourceConfig
	pairs := strings.Split(resourceConfigs, ",")

	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.Split(pair, ":")
		if len(parts) == 1 {
			// Just resource name, use default interval
			configs = append(configs, ResourceConfig{
				Name:     strings.TrimSpace(parts[0]),
				Interval: defaultInterval,
			})
		} else if len(parts) == 2 {
			// Resource name and interval
			resourceName := strings.TrimSpace(parts[0])
			intervalStr := strings.TrimSpace(parts[1])

			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				klog.Warningf("Invalid interval '%s' for resource '%s', using default: %v", intervalStr, resourceName, err)
				interval = defaultInterval
			}

			configs = append(configs, ResourceConfig{
				Name:     resourceName,
				Interval: interval,
			})
		}
	}

	return configs
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

	return nil
}
