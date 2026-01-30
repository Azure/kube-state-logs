// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	corev1 "k8s.io/api/core/v1"
)

// ExtractResourceMap extracts a resource map as string map
func ExtractResourceMap(resourceList corev1.ResourceList) map[string]string {
	if resourceList == nil {
		return nil
	}

	result := make(map[string]string)
	for resourceName, quantity := range resourceList {
		result[string(resourceName)] = quantity.String()
	}
	return result
}

// ExtractCPUMillicores extracts CPU resource as millicores
func ExtractCPUMillicores(resourceList corev1.ResourceList) *int64 {
	if resourceList == nil {
		return nil
	}
	if quantity, exists := resourceList[corev1.ResourceCPU]; exists {
		milliValue := quantity.MilliValue()
		return &milliValue
	}
	return nil
}

// ExtractMemoryBytes extracts memory resource as bytes
func ExtractMemoryBytes(resourceList corev1.ResourceList) *int64 {
	if resourceList == nil {
		return nil
	}
	if quantity, exists := resourceList[corev1.ResourceMemory]; exists {
		byteValue := quantity.Value()
		return &byteValue
	}
	return nil
}

// ExtractPodsCount extracts pods resource as int64
func ExtractPodsCount(resourceList corev1.ResourceList) *int64 {
	if resourceList == nil {
		return nil
	}
	if quantity, exists := resourceList[corev1.ResourcePods]; exists {
		value := quantity.Value()
		return &value
	}
	return nil
}

// ExtractResourceMapExcludingCPUMemory extracts a resource map as string map, excluding CPU and memory
func ExtractResourceMapExcludingCPUMemory(resourceList corev1.ResourceList) map[string]string {
	if resourceList == nil {
		return nil
	}

	result := make(map[string]string)
	for resourceName, quantity := range resourceList {
		// Skip CPU and memory as they have their own specific fields
		if resourceName != corev1.ResourceCPU && resourceName != corev1.ResourceMemory {
			result[string(resourceName)] = quantity.String()
		}
	}

	// Return nil if map is empty to keep consistent with existing behavior
	if len(result) == 0 {
		return nil
	}
	return result
}

// ExtractResourceMapExcludingCommon extracts a resource map as string map, excluding CPU, memory, and pods
func ExtractResourceMapExcludingCommon(resourceList corev1.ResourceList) map[string]string {
	if resourceList == nil {
		return nil
	}

	result := make(map[string]string)
	for resourceName, quantity := range resourceList {
		// Skip CPU, memory, and pods as they have their own specific fields
		if resourceName != corev1.ResourceCPU && resourceName != corev1.ResourceMemory && resourceName != corev1.ResourcePods {
			result[string(resourceName)] = quantity.String()
		}
	}

	// Return nil if map is empty to keep consistent with existing behavior
	if len(result) == 0 {
		return nil
	}
	return result
}
