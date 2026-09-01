// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package kubelet

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, token string) (*Client, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(serverURL.Host)
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}

	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatalf("write test token: %v", err)
	}
	client, err := NewClient(ClientConfig{
		NodeIP:             host,
		Port:               port,
		TokenPath:          tokenPath,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	return client, tokenPath
}

func TestClientGetsPodsAndStats(t *testing.T) {
	cpu := uint64(250_000_000)
	memory := uint64(128 * 1024 * 1024)
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/pods":
			_ = json.NewEncoder(w).Encode(PodList{Items: []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "default"},
			}}})
		case "/stats/summary":
			_ = json.NewEncoder(w).Encode(StatsSummary{Pods: []PodStats{{
				PodRef: PodReference{UID: "uid-a"},
				Containers: []ContainerStats{{
					Name:   "app",
					CPU:    &CPUStats{UsageNanoCores: &cpu},
					Memory: &MemoryStats{WorkingSetBytes: &memory},
				}},
			}}})
		default:
			http.NotFound(w, r)
		}
	}, " test-token\n")

	pods, err := client.GetPods(context.Background())
	if err != nil {
		t.Fatalf("GetPods() error: %v", err)
	}
	if len(pods) != 1 || pods[0].Name != "pod-a" {
		t.Fatalf("GetPods() = %#v", pods)
	}

	stats, err := client.GetStatsSummary(context.Background())
	if err != nil {
		t.Fatalf("GetStatsSummary() error: %v", err)
	}
	if got := *stats.Pods[0].Containers[0].CPU.UsageNanoCores; got != cpu {
		t.Fatalf("UsageNanoCores = %d, want %d", got, cpu)
	}
}

func TestClientReadsRotatedTokenForEveryRequest(t *testing.T) {
	var mu sync.Mutex
	var authorizationHeaders []string
	client, tokenPath := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizationHeaders = append(authorizationHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(PodList{})
	}, "first")

	if _, err := client.GetPods(context.Background()); err != nil {
		t.Fatalf("first GetPods() error: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("second\n"), 0o600); err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	if _, err := client.GetPods(context.Background()); err != nil {
		t.Fatalf("second GetPods() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"Bearer first", "Bearer second"}
	if len(authorizationHeaders) != len(want) {
		t.Fatalf("authorization headers = %#v", authorizationHeaders)
	}
	for i := range want {
		if authorizationHeaders[i] != want[i] {
			t.Errorf("authorizationHeaders[%d] = %q, want %q", i, authorizationHeaders[i], want[i])
		}
	}
}

func TestNewClientBuildsIPv6SafeURL(t *testing.T) {
	client, err := NewClient(ClientConfig{
		NodeIP:             "2001:db8::1",
		Port:               10250,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if client.baseURL != "https://[2001:db8::1]:10250" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
}

func TestClientRejectsInvalidConfiguration(t *testing.T) {
	tests := []ClientConfig{
		{},
		{NodeIP: "127.0.0.1", Port: -1, InsecureSkipVerify: true},
		{NodeIP: "127.0.0.1", Port: 65536, InsecureSkipVerify: true},
	}
	for _, config := range tests {
		if _, err := NewClient(config); err == nil {
			t.Errorf("NewClient(%+v) succeeded", config)
		}
	}
}

func TestNewClientFailsClosedWhenCAIsUnavailable(t *testing.T) {
	_, err := NewClient(ClientConfig{
		NodeIP:    "127.0.0.1",
		Port:      10250,
		CAPath:    filepath.Join(t.TempDir(), "missing-ca.crt"),
		TokenPath: filepath.Join(t.TempDir(), "token"),
	})
	if err == nil {
		t.Fatal("NewClient() succeeded without a CA certificate")
	}
}

func TestClientReturnsStatusError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}, "token")
	if _, err := client.GetPods(context.Background()); err == nil {
		t.Fatal("GetPods() succeeded for a forbidden response")
	}
}

func TestNanoCoresToMilliCores(t *testing.T) {
	if got := NanoCoresToMilliCores(250_500_000); got != 250 {
		t.Fatalf("NanoCoresToMilliCores() = %d, want 250", got)
	}
}
