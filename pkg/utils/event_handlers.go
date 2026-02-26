// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/azure/kube-state-logs/pkg/interfaces"
)

// EventHandlerRegistrar is satisfied by cache.SharedIndexInformer and allows
// registering informer event handlers. Using a narrow interface makes
// unit-testing straightforward without implementing the full informer contract.
type EventHandlerRegistrar interface {
	AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error)
}

// SetupEventHandlers registers informer event handlers that emit a log entry
// on resource creation and deletion. Events during initial cache sync are
// suppressed via the hasSynced flag.
//
// Parameters:
//   - informer: an object supporting AddEventHandler (e.g. cache.SharedIndexInformer)
//   - createEntry: callback that builds a log entry from the typed object; the
//     eventType parameter is "created" or "deleted"
//   - shouldInclude: predicate controlling whether the object is in scope (e.g.
//     namespace filtering); return true to include
//   - logger: destination for the log entry
//   - hasSynced: atomic flag; events are silently dropped while false
func SetupEventHandlers[K metav1.Object](
	informer EventHandlerRegistrar,
	createEntry func(obj K, eventType string) any,
	shouldInclude func(obj K) bool,
	logger interfaces.Logger,
	hasSynced *atomic.Bool,
) {
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if !hasSynced.Load() {
				return
			}
			typedObj, ok := obj.(K)
			if !ok {
				return
			}
			if !shouldInclude(typedObj) {
				return
			}
			entry := createEntry(typedObj, "created")
			if err := logger.Log(entry); err != nil {
				klog.Errorf("Failed to log create event for %s: %v", typedObj.GetName(), err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// Handle tombstones (cache.DeletedFinalStateUnknown)
			if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = d.Obj
			}
			typedObj, ok := obj.(K)
			if !ok {
				return
			}
			if !shouldInclude(typedObj) {
				return
			}
			entry := createEntry(typedObj, "deleted")
			if err := logger.Log(entry); err != nil {
				klog.Errorf("Failed to log delete event for %s: %v", typedObj.GetName(), err)
			}
		},
	})
}
