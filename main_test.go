// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/azure/kube-state-logs/pkg/collector"
)

func TestParseConfigCRDConfigs(t *testing.T) {
	for _, tt := range []struct {
		name      string
		crdConfig string
		wantErr   string
		wantDrop  []string
	}{
		{"drop fields", "example.com/v1:widgets:spec:**.token|**.password", "", []string{"**.token", "**.password"}},
		{"too many segments", "example.com/v1:widgets:spec:**.token:**.password", "failed to parse CRD configs", nil},
		{"empty resource", "example.com/v1:", "failed to parse CRD configs", nil},
		{"invalid drop pattern", "example.com/v1:widgets:spec:spec.**", "configuration validation failed", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _, err := parseConfig("kube-state-logs", []string{"--crd-configs=" + tt.crdConfig})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseConfig error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if len(cfg.CRDs) != 1 || !slices.Equal(cfg.CRDs[0].DropFields, tt.wantDrop) {
				t.Fatalf("CRDs = %+v, want drop fields %v", cfg.CRDs, tt.wantDrop)
			}
		})
	}
}

func TestProbeHandler(t *testing.T) {
	for _, tt := range []struct {
		name       string
		ready      bool
		canceled   bool
		method     string
		path       string
		wantStatus int
	}{
		{"pending caches", false, false, http.MethodGet, "/readyz", http.StatusServiceUnavailable},
		{"synced caches", true, false, http.MethodGet, "/readyz", http.StatusOK},
		{"shutting down", true, true, http.MethodGet, "/readyz", http.StatusServiceUnavailable},
		{"live with pending caches", false, false, http.MethodGet, "/livez", http.StatusOK},
		{"live with synced caches", true, false, http.MethodGet, "/livez", http.StatusOK},
		{"live during graceful shutdown", false, true, http.MethodGet, "/livez", http.StatusOK},
		{"unknown path", true, false, http.MethodGet, "/unknown", http.StatusNotFound},
		{"unsupported method", true, false, http.MethodPost, "/readyz", http.StatusMethodNotAllowed},
		{"unsupported liveness method", true, false, http.MethodPost, "/livez", http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
			}

			recorder := httptest.NewRecorder()
			readyChecked := false
			handler := probeHandler(ctx, func() bool {
				readyChecked = true
				return tt.ready
			})
			handler.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if tt.path == "/livez" && readyChecked {
				t.Fatal("liveness must not check cache readiness")
			}
		})
	}
}

func TestRunCollectorProbeListenFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	err = runCollector(t.Context(), &collector.Collector{}, listener.Addr().String())
	if err == nil || !strings.Contains(err.Error(), "failed to listen for health probes") {
		t.Fatalf("runCollector() error = %v, want health probe listener failure", err)
	}
}

func TestRunCollectorCanceledStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runCollector(ctx, &collector.Collector{}, "127.0.0.1:0"); err != nil {
		t.Fatalf("runCollector() error = %v, want nil for cancellation", err)
	}
}
