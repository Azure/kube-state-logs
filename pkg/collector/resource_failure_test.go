// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azure/kube-state-logs/pkg/collector/resources"
	"github.com/azure/kube-state-logs/pkg/config"
	"github.com/azure/kube-state-logs/pkg/interfaces"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"
)

func runCollectorForTest(t *testing.T, c *Collector) (context.CancelFunc, <-chan struct{}, *error) {
	t.Helper()
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
			return
		}
		c.factory.Shutdown()
		if c.podFactory != nil {
			c.podFactory.Shutdown()
		}
		c.dynFactory.Shutdown()
	})
	return cancel, done, &runErr
}

func TestRunRejectsUnknownBuiltInResource(t *testing.T) {
	for _, source := range []string{"resources", "resource-configs"} {
		t.Run(source, func(t *testing.T) {
			c, _, _, _ := newReadinessTestCollector(t)
			switch source {
			case "resources":
				c.config.Resources = []string{"unknown"}
			case "resource-configs":
				c.config.ResourceConfigs = []config.ResourceConfig{{Name: "unknown", Interval: time.Hour}}
			}
			_, done, runErr := runCollectorForTest(t, c)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("collector did not reject unknown resource")
			}
			if *runErr == nil || !strings.Contains((*runErr).Error(), "unknown resource type: unknown") {
				t.Fatalf("Run() error = %v, want unknown resource error", *runErr)
			}
			if c.Ready() {
				t.Fatal("collector is ready with an unknown resource")
			}
		})
	}
}

func TestRunBuiltInPermanentAPIErrors(t *testing.T) {
	for _, verb := range []string{"list", "watch"} {
		for _, status := range []struct {
			name string
			err  error
		}{
			{"not found", apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "")},
			{"unauthorized", apierrors.NewUnauthorized("authentication required")},
			{"forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("access denied"))},
		} {
			for _, shared := range []bool{false, true} {
				name := "separate pod factory"
				if shared {
					name = "shared factory"
				}
				t.Run(verb+"/"+status.name+"/"+name, func(t *testing.T) {
					c, client, podClient, _ := newReadinessTestCollector(t)
					if shared {
						c.podFactory = c.factory
						podClient = client
					}
					c.config.Resources = []string{"pod"}
					c.handlers["pod"] = resources.NewPodHandler(podClient)
					if verb == "list" {
						podClient.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
							return true, nil, status.err
						})
					} else {
						podClient.PrependWatchReactor("pods", func(k8stesting.Action) (bool, watch.Interface, error) {
							return true, nil, status.err
						})
					}

					_, done, runErr := runCollectorForTest(t, c)
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Fatal("collector kept retrying a permanent built-in API error")
					}
					if !errors.Is(*runErr, status.err) {
						t.Fatalf("Run() error = %v, want wrapped API error %v", *runErr, status.err)
					}
					if c.Ready() {
						t.Fatal("collector is ready after a permanent API error")
					}
				})
			}
		}
	}
}

func TestRunBuiltInDependencyFailure(t *testing.T) {
	for _, resource := range []string{"service", "pod", "container"} {
		t.Run(resource, func(t *testing.T) {
			c, client, _, _ := newReadinessTestCollector(t)
			c.podFactory = c.factory
			c.config.Resources = []string{resource}
			c.handlers = map[string]interfaces.ResourceHandler{
				"service":   resources.NewServiceHandler(client),
				"pod":       resources.NewPodHandler(client, "topology.kubernetes.io/zone"),
				"container": resources.NewContainerHandler(client, nil, nil, "topology.kubernetes.io/zone"),
			}
			dependency := "nodes"
			if resource == "service" {
				dependency = "endpoints"
			}
			apiErr := apierrors.NewForbidden(schema.GroupResource{Resource: dependency}, "", errors.New("access denied"))
			client.PrependReactor("list", dependency, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apiErr
			})
			_, done, runErr := runCollectorForTest(t, c)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("collector ignored failure of %s dependency", dependency)
			}
			if !errors.Is(*runErr, apiErr) {
				t.Fatalf("Run() error = %v, want dependency API error %v", *runErr, apiErr)
			}
			if c.Ready() {
				t.Fatal("collector is ready with a failed dependency cache")
			}
		})
	}
}

func TestRunBuiltInWatchFailureAfterSync(t *testing.T) {
	c, client, _, _ := newReadinessTestCollector(t)
	c.podFactory = c.factory
	c.config.Resources = []string{"pod"}
	c.handlers["pod"] = resources.NewPodHandler(client)
	stream := watch.NewRaceFreeFake()
	defer stream.Stop()
	var watchAttempts atomic.Int32
	apiErr := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("access revoked"))
	client.PrependWatchReactor("pods", func(k8stesting.Action) (bool, watch.Interface, error) {
		if watchAttempts.Add(1) == 1 {
			return true, stream, nil
		}
		return true, nil, apiErr
	})
	_, done, runErr := runCollectorForTest(t, c)
	if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return c.Ready() && watchAttempts.Load() > 0, nil
	}); err != nil {
		t.Fatalf("collector did not become ready: %v", err)
	}

	stream.Stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not exit after watch access was revoked")
	}
	if !errors.Is(*runErr, apiErr) {
		t.Fatalf("Run() error = %v, want watch API error %v", *runErr, apiErr)
	}
	if c.Ready() {
		t.Fatal("collector stayed ready after watch access was revoked")
	}
}

func TestRunRetriesRecoverableAPIErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		crd  bool
		err  error
	}{
		{"missing CRD", true, apierrors.NewNotFound(readinessTestGVR.GroupResource(), "")},
		{"CRD authentication", true, apierrors.NewUnauthorized("authentication required")},
		{"CRD RBAC", true, apierrors.NewForbidden(readinessTestGVR.GroupResource(), "", errors.New("access denied"))},
		{"built-in transient failure", false, apierrors.NewServiceUnavailable("API server unavailable")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, client, _, dynamicClient := newReadinessTestCollector(t)
			var available atomic.Bool
			var attempts atomic.Int32
			firstAttempt := make(chan struct{})
			var started sync.Once
			failUntilAvailable := func(k8stesting.Action) (bool, runtime.Object, error) {
				attempts.Add(1)
				started.Do(func() { close(firstAttempt) })
				if !available.Load() {
					return true, nil, tt.err
				}
				return false, nil, nil
			}
			if tt.crd {
				c.config.Resources = []string{"crd"}
				c.config.CRDs = []config.CRDConfig{{APIVersion: "example.com/v1", Resource: "widgets"}}
				c.dynClient = dynamicClient
				c.registerCRDHandlers()
				if len(c.crdHandlers) != 1 {
					t.Fatal("missing CRD was not registered for retries")
				}
				dynamicClient.PrependReactor("list", "widgets", failUntilAvailable)
			} else {
				c.config.Resources = []string{"namespace"}
				c.handlers["namespace"] = resources.NewNamespaceHandler(client)
				client.PrependReactor("list", "namespaces", failUntilAvailable)
			}
			_, done, runErr := runCollectorForTest(t, c)
			select {
			case <-firstAttempt:
			case <-time.After(5 * time.Second):
				t.Fatal("informer did not attempt its first list")
			}
			select {
			case <-done:
				t.Fatalf("collector exited instead of retrying: %v", *runErr)
			case <-time.After(200 * time.Millisecond):
			}
			if tt.crd {
				if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
					return c.Ready(), nil
				}); err != nil {
					t.Fatalf("unsynced CRD blocked readiness: %v", err)
				}
				if c.crdHandlers["widgets.example.com"].HasSynced() {
					t.Fatal("CRD synced while its API was unavailable")
				}
			} else if c.Ready() {
				t.Fatal("collector is ready before the failing built-in cache syncs")
			}

			available.Store(true)
			if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) {
				select {
				case <-done:
					return false, errors.New("collector exited instead of recovering")
				default:
					if tt.crd {
						return c.Ready() && c.crdHandlers["widgets.example.com"].HasSynced(), nil
					}
					return c.Ready(), nil
				}
			}); err != nil {
				t.Fatalf("collector did not sync the cache after API recovery: %v", err)
			}
			if attempts.Load() < 2 {
				t.Fatal("informer did not retry the failed list")
			}
		})
	}
}
