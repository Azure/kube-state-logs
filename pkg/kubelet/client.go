// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

// Package kubelet provides a client for interacting with the kubelet API.
package kubelet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	// DefaultKubeletPort is the default port for the kubelet API.
	DefaultKubeletPort = 10250

	// ServiceAccountTokenPath is the path to the service account token.
	ServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// ServiceAccountCAPath is the path to the service account CA certificate.
	ServiceAccountCAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	maxResponseBytes = 100 * 1024 * 1024
)

// Interface is the kubelet API surface used by the collectors.
type Interface interface {
	GetPods(ctx context.Context) ([]corev1.Pod, error)
	GetStatsSummary(ctx context.Context) (*StatsSummary, error)
}

// Client provides methods for interacting with the kubelet API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	nodeIP     string
	port       int
	tokenPath  string
}

// ClientConfig holds configuration for creating a kubelet client.
type ClientConfig struct {
	// NodeIP is the IP address of the node running the kubelet.
	NodeIP string

	// Port is the kubelet API port (default: 10250).
	Port int

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Timeout is the HTTP client timeout.
	Timeout time.Duration

	// TokenPath is the projected service account token path.
	TokenPath string

	// CAPath is the CA bundle used to verify the kubelet certificate.
	CAPath string
}

// PodList represents the response from the kubelet /pods endpoint.
type PodList struct {
	Kind       string       `json:"kind"`
	APIVersion string       `json:"apiVersion"`
	Items      []corev1.Pod `json:"items"`
}

// StatsSummary is the subset of the kubelet /stats/summary response used by
// kube-state-logs. Keeping the wire type local avoids importing the large
// k8s.io/kubelet module solely for API structs.
type StatsSummary struct {
	Pods []PodStats `json:"pods"`
}

// PodStats contains per-container usage for one pod.
type PodStats struct {
	PodRef     PodReference     `json:"podRef"`
	Containers []ContainerStats `json:"containers"`
}

// PodReference identifies a pod in a stats summary.
type PodReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
}

// ContainerStats contains the usage values needed for a container snapshot.
type ContainerStats struct {
	Name   string       `json:"name"`
	CPU    *CPUStats    `json:"cpu,omitempty"`
	Memory *MemoryStats `json:"memory,omitempty"`
}

// CPUStats contains current CPU usage.
type CPUStats struct {
	UsageNanoCores *uint64 `json:"usageNanoCores,omitempty"`
}

// MemoryStats contains current memory usage.
type MemoryStats struct {
	WorkingSetBytes *uint64 `json:"workingSetBytes,omitempty"`
}

// NewClient creates a new kubelet API client.
func NewClient(config ClientConfig) (*Client, error) {
	if config.NodeIP == "" {
		return nil, fmt.Errorf("node IP is required")
	}

	if config.Port == 0 {
		config.Port = DefaultKubeletPort
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("invalid kubelet port %d", config.Port)
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.TokenPath == "" {
		config.TokenPath = ServiceAccountTokenPath
	}
	if config.CAPath == "" {
		config.CAPath = ServiceAccountCAPath
	}

	tlsConfig := &tls.Config{ // #nosec G402 -- explicitly controlled by operator configuration
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: config.InsecureSkipVerify,
	}
	if !config.InsecureSkipVerify {
		caCert, err := os.ReadFile(config.CAPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read kubelet CA certificate: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
			return nil, fmt.Errorf("failed to parse kubelet CA certificate from %s", config.CAPath)
		}
		tlsConfig.RootCAs = caCertPool
	}

	hostPort := net.JoinHostPort(config.NodeIP, strconv.Itoa(config.Port))
	baseURL := (&url.URL{Scheme: "https", Host: hostPort}).String()

	return &Client{
		httpClient: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
		baseURL:   baseURL,
		nodeIP:    config.NodeIP,
		port:      config.Port,
		tokenPath: config.TokenPath,
	}, nil
}

// GetPods retrieves all pods from the kubelet API.
func (c *Client) GetPods(ctx context.Context) ([]corev1.Pod, error) {
	var podList PodList
	if err := c.get(ctx, "/pods", &podList); err != nil {
		return nil, fmt.Errorf("kubelet /pods request failed: %w", err)
	}
	return podList.Items, nil
}

// GetStatsSummary retrieves local pod and container usage from the kubelet.
func (c *Client) GetStatsSummary(ctx context.Context) (*StatsSummary, error) {
	var summary StatsSummary
	if err := c.get(ctx, "/stats/summary", &summary); err != nil {
		return nil, fmt.Errorf("kubelet /stats/summary request failed: %w", err)
	}
	return &summary, nil
}

func (c *Client) get(ctx context.Context, requestPath string, destination any) error {
	requestURL, err := url.JoinPath(c.baseURL, requestPath)
	if err != nil {
		return fmt.Errorf("failed to build request URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	token, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return fmt.Errorf("failed to read service account token: %w", err)
	}
	trimmedToken := strings.TrimSpace(string(token))
	if trimmedToken == "" {
		return fmt.Errorf("service account token is empty")
	}
	req.Header.Set("Authorization", "Bearer "+trimmedToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request to kubelet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
		return fmt.Errorf("kubelet API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("kubelet response exceeded %d bytes", maxResponseBytes)
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return fmt.Errorf("failed to decode kubelet response: %w", err)
	}

	return nil
}

// GetNodeIP returns the node IP configured for this client.
func (c *Client) GetNodeIP() string {
	return c.nodeIP
}

// GetPort returns the kubelet port configured for this client.
func (c *Client) GetPort() int {
	return c.port
}

// NanoCoresToMilliCores converts CPU usage from nanocores to millicores.
func NanoCoresToMilliCores(nanoCores uint64) int64 {
	return int64(nanoCores / 1_000_000)
}
