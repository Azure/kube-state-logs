// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package kubelet

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

const (
	defaultTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultTimeout   = 30 * time.Second
	maxResponseBytes = 100 * 1024 * 1024 // 100MB safety limit
	podsEndpoint     = "/pods"
	statsSummaryPath = "/stats/summary"
)

// Client communicates with the local kubelet API to retrieve pod and stats data.
type Client struct {
	baseURL    string
	tokenPath  string
	httpClient *http.Client
}

// NewClient creates a new kubelet Client.
// baseURL is the kubelet API base URL (e.g., "https://localhost:10250").
// tokenPath is the path to the service account token file; empty uses the default path.
func NewClient(baseURL, tokenPath string) *Client {
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}

	// Kubelet uses a self-signed certificate on localhost.
	// InsecureSkipVerify is standard practice for direct kubelet communication
	// (same approach as metrics-server and Prometheus).
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // #nosec G402 -- kubelet localhost with self-signed cert
		},
	}

	return &Client{
		baseURL:   baseURL,
		tokenPath: tokenPath,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   defaultTimeout,
		},
	}
}

// GetPods retrieves the list of pods running on this node from the kubelet /pods endpoint.
func (c *Client) GetPods(ctx context.Context) (*corev1.PodList, error) {
	body, err := c.doRequest(ctx, podsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("kubelet /pods request failed: %w", err)
	}

	var podList corev1.PodList
	if err := json.Unmarshal(body, &podList); err != nil {
		return nil, fmt.Errorf("failed to decode kubelet /pods response: %w", err)
	}

	return &podList, nil
}

// GetStatsSummary retrieves the stats summary from the kubelet /stats/summary endpoint.
func (c *Client) GetStatsSummary(ctx context.Context) (*statsv1alpha1.Summary, error) {
	body, err := c.doRequest(ctx, statsSummaryPath)
	if err != nil {
		return nil, fmt.Errorf("kubelet /stats/summary request failed: %w", err)
	}

	var summary statsv1alpha1.Summary
	if err := json.Unmarshal(body, &summary); err != nil {
		return nil, fmt.Errorf("failed to decode kubelet /stats/summary response: %w", err)
	}

	return &summary, nil
}

// doRequest performs an authenticated GET request to the kubelet API.
func (c *Client) doRequest(ctx context.Context, path string) ([]byte, error) {
	token, err := c.readToken()
	if err != nil {
		return nil, fmt.Errorf("failed to read service account token: %w", err)
	}

	reqURL, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("failed to build request URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, path)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// readToken reads the service account token from disk.
// The token is read fresh each time to handle automatic rotation.
func (c *Client) readToken() (string, error) {
	data, err := os.ReadFile(c.tokenPath)
	if err != nil {
		klog.V(4).Infof("Failed to read token from %s: %v", c.tokenPath, err)
		return "", err
	}
	return string(data), nil
}

// NanoCoresToMilliCores converts CPU usage from nanocores to millicores.
func NanoCoresToMilliCores(nanoCores uint64) int64 {
	return int64(nanoCores / 1_000_000)
}

// SetHTTPClient overrides the HTTP client used by the kubelet client.
// This is intended for testing only.
func (c *Client) SetHTTPClient(httpClient *http.Client) {
	c.httpClient = httpClient
}
