package resources

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

type metricsRoundTripper func(*http.Request) (*http.Response, error)

func (transport metricsRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestContainerHandlerMetricsDeadline(t *testing.T) {
	for _, test := range []struct {
		name            string
		cancelFirst     bool
		alreadyCanceled bool
		wantRequests    int
	}{
		{name: "shared deadline", wantRequests: 3},
		{name: "stop after cancellation", cancelFirst: true, wantRequests: 1},
		{name: "already canceled", alreadyCanceled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.alreadyCanceled {
				cancel()
			}
			requests := 0
			var firstDeadline time.Time
			started := time.Now()
			transport := metricsRoundTripper(func(request *http.Request) (*http.Response, error) {
				requests++
				deadline, ok := request.Context().Deadline()
				if !ok {
					t.Fatal("metrics request has no deadline")
				}
				if requests == 1 {
					firstDeadline = deadline
					if deadline.Before(started.Add(30*time.Second)) || deadline.After(time.Now().Add(30*time.Second)) {
						t.Fatalf("deadline = %v, want a 30-second collection budget", deadline)
					}
				} else if !deadline.Equal(firstDeadline) {
					t.Fatalf("deadline = %v, want shared deadline %v", deadline, firstDeadline)
				}
				if test.cancelFirst {
					cancel()
					return nil, request.Context().Err()
				}
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)),
				}, nil
			})
			metricsClient, err := metricsclientset.NewForConfigAndClient(&rest.Config{Host: "https://metrics.example"}, &http.Client{Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			handler := NewContainerHandler(fake.NewSimpleClientset(), metricsClient, nil)
			handler.SetNodeFilter("node-a")
			var pods []any
			for _, name := range []string{"first", "second", "third"} {
				pod := createTestPodWithContainers(name, "default", []corev1.Container{{Name: "app"}})
				pod.Spec.NodeName = "node-a"
				pods = append(pods, pod)
			}
			entries, err := handler.processPods(ctx, pods, nil)
			if err != nil {
				t.Fatal(err)
			}
			if requests != test.wantRequests {
				t.Fatalf("metrics requests = %d, want %d", requests, test.wantRequests)
			}
			if len(entries) != len(pods) {
				t.Fatalf("container snapshots = %d, want %d despite missing metrics", len(entries), len(pods))
			}
		})
	}
}
