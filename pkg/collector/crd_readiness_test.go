// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azure/kube-state-logs/pkg/collector/resources"
	"github.com/azure/kube-state-logs/pkg/config"
	"github.com/azure/kube-state-logs/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

type snapshotCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (l *snapshotCounter) Log(entry any) error {
	var resourceType string
	switch entry := entry.(type) {
	case types.CRDData:
		resourceType = entry.ResourceType
	case types.NamespaceData:
		resourceType = entry.ResourceType
	default:
		return fmt.Errorf("unexpected snapshot type: %T", entry)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts[resourceType]++
	return nil
}

func (l *snapshotCounter) count(resourceType string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[resourceType]
}

func TestRunCRDsDoNotBlockReadinessOrCollection(t *testing.T) {
	for _, mode := range []string{"mixed", "crd-only"} {
		for _, recoverAPI := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recovery=%t", mode, recoverAPI), func(t *testing.T) {
				c, client, _, _ := newReadinessTestCollector(t)
				gadgetGVR := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "gadgets"}
				widget := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "example.com/v1",
					"kind":       "Widget",
					"metadata":   map[string]any{"name": "widget", "namespace": "default"},
				}}
				gadget := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "example.com/v1",
					"kind":       "Gadget",
					"metadata":   map[string]any{"name": "gadget", "namespace": "default"},
				}}
				dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
					runtime.NewScheme(),
					map[schema.GroupVersionResource]string{readinessTestGVR: "WidgetList", gadgetGVR: "GadgetList"},
					widget, gadget,
				)
				c.dynClient = dynamicClient
				c.dynFactory = dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
				c.config.LogInterval = 5 * time.Millisecond
				c.config.Resources = []string{"crd"}
				c.config.CRDs = []config.CRDConfig{
					{APIVersion: "example.com/v1", Resource: "widgets"},
					{APIVersion: "example.com/v1", Resource: "gadgets"},
				}
				c.registerCRDHandlers()
				if mode == "mixed" {
					c.config.Resources = append(c.config.Resources, "namespace")
					c.handlers["namespace"] = resources.NewNamespaceHandler(client)
					if _, err := client.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{Name: "default"},
					}, metav1.CreateOptions{}); err != nil {
						t.Fatal(err)
					}
				}
				logger := &snapshotCounter{counts: make(map[string]int)}
				c.logger = logger
				var available atomic.Bool
				var attempts atomic.Int32
				dynamicClient.PrependReactor("list", "widgets", func(k8stesting.Action) (bool, runtime.Object, error) {
					attempts.Add(1)
					if !available.Load() {
						return true, nil, apierrors.NewNotFound(readinessTestGVR.GroupResource(), "")
					}
					return false, nil, nil
				})
				pendingInformer := c.dynFactory.ForResource(readinessTestGVR).Informer()
				// Partial data must not be logged before this cache has synced.
				if err := pendingInformer.GetStore().Add(widget.DeepCopy()); err != nil {
					t.Fatal(err)
				}
				cancel, done, runErr := runCollectorForTest(t, c)
				if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
					return c.Ready() && attempts.Load() > 0 && logger.count("gadget") >= 2 &&
						(mode == "crd-only" || logger.count("namespace") >= 2), nil
				}); err != nil {
					t.Fatalf("missing CRD blocked readiness or healthy resource collection: %v", err)
				}
				if pendingInformer.HasSynced() || logger.count("widget") != 0 {
					t.Fatal("unsynced CRD was treated as ready for collection")
				}

				if recoverAPI {
					available.Store(true)
					if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) {
						if !c.Ready() {
							return false, fmt.Errorf("collector became unready during CRD recovery")
						}
						return logger.count("widget") > 0, nil
					}); err != nil {
						t.Fatalf("CRD collection did not start after recovery: %v", err)
					}
					if !pendingInformer.HasSynced() || attempts.Load() < 2 {
						t.Fatal("CRD did not retry and sync before collection")
					}
				}

				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("collector did not stop while waiting for CRDs")
				}
				if *runErr != nil || c.Ready() {
					t.Fatalf("shutdown error = %v, ready = %t", *runErr, c.Ready())
				}
			})
		}
	}
}
