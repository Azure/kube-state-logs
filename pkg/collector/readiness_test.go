// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azure/kube-state-logs/pkg/collector/resources"
	"github.com/azure/kube-state-logs/pkg/config"
	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/kubelet"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var readinessTestGVR = schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}

func TestReadyWithLeaderElection(t *testing.T) {
	c := &Collector{config: &config.Config{LeaderElection: true}}
	if c.Ready() {
		t.Fatal("collector is ready before leader election starts")
	}

	c.electionRunning.Store(true)
	if !c.Ready() {
		t.Fatal("follower is not ready while participating in leader election")
	}

	c.leading.Store(true)
	if c.Ready() {
		t.Fatal("leader is ready before informer caches sync")
	}

	c.ready.Store(true)
	if !c.Ready() {
		t.Fatal("leader is not ready after informer caches sync")
	}

	c.electionRunning.Store(false)
	c.ready.Store(false)
	if c.Ready() {
		t.Fatal("collector is ready after leader election stops")
	}
}

func newReadinessTestCollector(t *testing.T) (*Collector, *fake.Clientset, *fake.Clientset, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	client := fake.NewSimpleClientset()
	podClient := fake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{readinessTestGVR: "WidgetList"},
	)
	c := &Collector{
		config:      &config.Config{LogInterval: time.Hour},
		handlers:    make(map[string]interfaces.ResourceHandler),
		crdHandlers: make(map[string]*resources.CRDHandler),
		factory:     informers.NewSharedInformerFactory(client, 0),
		podFactory:  informers.NewSharedInformerFactory(podClient, 0),
		dynFactory:  dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0),
		stopCh:      make(chan struct{}),
	}
	return c, client, podClient, dynamicClient
}

func TestRunReadinessWaitsForBuiltInCaches(t *testing.T) {
	for _, blockedFactory := range []string{"main", "pod", "shared"} {
		t.Run(blockedFactory, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c, client, podClient, dynamicClient := newReadinessTestCollector(t)
			if blockedFactory == "shared" {
				c.podFactory = c.factory
			}
			c.factory.Core().V1().Namespaces().Informer()
			c.podFactory.Core().V1().Pods().Informer()
			c.crdHandlers["widgets"] = resources.NewCRDHandler(dynamicClient, readinessTestGVR, "widgets", nil)

			listStarted := make(chan struct{})
			releaseList := make(chan struct{})
			var started sync.Once
			blockList := func(k8stesting.Action) (bool, runtime.Object, error) {
				started.Do(func() { close(listStarted) })
				select {
				case <-releaseList:
					return false, nil, nil
				case <-ctx.Done():
					return true, nil, ctx.Err()
				}
			}
			switch blockedFactory {
			case "main":
				client.PrependReactor("list", "namespaces", blockList)
			case "pod":
				podClient.PrependReactor("list", "pods", blockList)
			case "shared":
				client.PrependReactor("list", "pods", blockList)
			}

			if c.Ready() {
				t.Fatal("collector is ready before Run")
			}
			done := make(chan struct{})
			var runErr error
			go func() {
				runErr = c.Run(ctx)
				close(done)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("collector did not stop")
				}
				c.factory.Shutdown()
				c.podFactory.Shutdown()
				c.dynFactory.Shutdown()
			})

			select {
			case <-listStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("informer did not start listing")
			}
			err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 200*time.Millisecond, true, func(context.Context) (bool, error) {
				return c.Ready(), nil
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("collector became ready with an unsynced %s cache: %v", blockedFactory, err)
			}

			close(releaseList)
			if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
				return c.Ready(), nil
			}); err != nil {
				t.Fatalf("collector did not become ready after all caches synced: %v", err)
			}
			cancel()
			select {
			case <-done:
				if runErr != nil {
					t.Fatalf("Run() error = %v", runErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("collector did not stop")
			}
			if c.Ready() {
				t.Fatal("collector is ready after shutdown")
			}
		})
	}
}

func TestRunReadinessKubeletOnly(t *testing.T) {
	c, _, _, _ := newReadinessTestCollector(t)
	c.config.Resources = []string{"pod"}
	c.config.UseKubeletAPI = true
	c.podFactory = nil
	c.kubeletClient = &kubelet.Client{}
	c.kubeletHandlers = map[string]interfaces.KubeletHandler{"pod": pendingPodHandler{}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = c.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("collector did not stop")
		}
	})
	if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return c.Ready(), nil
	}); err != nil {
		t.Fatalf("kubelet-only collector did not become ready: %v", err)
	}
	cancel()
	select {
	case <-done:
		if runErr != nil {
			t.Fatalf("Run() error = %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not stop")
	}
	if c.Ready() {
		t.Fatal("kubelet-only collector is ready after shutdown")
	}
}

type failedSyncFactory struct {
	informers.SharedInformerFactory
}

func (failedSyncFactory) WaitForCacheSync(<-chan struct{}) map[reflect.Type]bool {
	return map[reflect.Type]bool{reflect.TypeFor[corev1.Pod](): false}
}

func TestRunReadinessSyncFailure(t *testing.T) {
	for _, tt := range []struct {
		factory string
		wantErr string
	}{
		{"main", "failed to sync informer"},
		{"pod", "failed to sync pod informer"},
	} {
		t.Run(tt.factory, func(t *testing.T) {
			c, _, _, _ := newReadinessTestCollector(t)
			switch tt.factory {
			case "main":
				c.factory = failedSyncFactory{c.factory}
			case "pod":
				c.podFactory = failedSyncFactory{c.podFactory}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := c.Run(ctx)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run() error = %v, want %q", err, tt.wantErr)
			}
			if c.Ready() {
				t.Fatal("collector is ready after cache-sync failure")
			}
			select {
			case <-c.stopCh:
			case <-time.After(5 * time.Second):
				t.Fatal("informers were not stopped after cache-sync failure")
			}
			c.factory.Shutdown()
			c.podFactory.Shutdown()
			c.dynFactory.Shutdown()
		})
	}
}

type failedSetupHandler struct {
	pendingPodHandler
	err error
}

func (h failedSetupHandler) SetupInformer(informers.SharedInformerFactory, interfaces.Logger, time.Duration) error {
	return h.err
}

func TestRunReadinessSetupFailure(t *testing.T) {
	c, _, _, _ := newReadinessTestCollector(t)
	setupErr := errors.New("cannot set up informer")
	c.config.Resources = []string{"pod"}
	c.handlers["pod"] = &failedSetupHandler{err: setupErr}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx); !errors.Is(err, setupErr) {
		t.Fatalf("Run() error = %v, want %v", err, setupErr)
	}
	if c.Ready() {
		t.Fatal("collector is ready after informer setup failure")
	}
}
