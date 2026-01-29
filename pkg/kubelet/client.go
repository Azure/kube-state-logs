// Package kubelet provides a client for interacting with the kubelet API.
package kubelet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
)

// Client provides methods for interacting with the kubelet API.
type Client struct {
	httpClient *http.Client
	nodeIP     string
	port       int
	token      string
}

// ClientConfig holds configuration for creating a kubelet client.
type ClientConfig struct {
	// NodeIP is the IP address of the node running the kubelet.
	NodeIP string

	// Port is the kubelet API port (default: 10250).
	Port int

	// InsecureSkipVerify disables TLS certificate verification.
	// This should only be used for testing.
	InsecureSkipVerify bool

	// Timeout is the HTTP client timeout.
	Timeout time.Duration
}

// NewClient creates a new kubelet API client.
func NewClient(config ClientConfig) (*Client, error) {
	if config.NodeIP == "" {
		return nil, fmt.Errorf("node IP is required")
	}

	if config.Port == 0 {
		config.Port = DefaultKubeletPort
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// Read service account token
	token, err := os.ReadFile(ServiceAccountTokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read service account token: %w", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.InsecureSkipVerify,
	}

	// If not skipping verification, load the CA certificate
	if !config.InsecureSkipVerify {
		caCert, err := os.ReadFile(ServiceAccountCAPath)
		if err != nil {
			// If CA cert is not available, we may need to skip verification
			// for kubelet's self-signed certificate
			tlsConfig.InsecureSkipVerify = true
		} else {
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			tlsConfig.RootCAs = caCertPool
		}
	}

	httpClient := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return &Client{
		httpClient: httpClient,
		nodeIP:     config.NodeIP,
		port:       config.Port,
		token:      string(token),
	}, nil
}

// PodList represents the response from the kubelet /pods endpoint.
// The kubelet returns pods in a slightly different format than the k8s API.
type PodList struct {
	Kind       string       `json:"kind"`
	APIVersion string       `json:"apiVersion"`
	Items      []corev1.Pod `json:"items"`
}

// GetPods retrieves all pods from the kubelet API.
func (c *Client) GetPods(ctx context.Context) ([]corev1.Pod, error) {
	url := fmt.Sprintf("https://%s:%d/pods", c.nodeIP, c.port)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header with bearer token
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to kubelet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kubelet API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var podList PodList
	if err := json.Unmarshal(body, &podList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pod list: %w", err)
	}

	return podList.Items, nil
}

// GetNodeIP returns the node IP configured for this client.
func (c *Client) GetNodeIP() string {
	return c.nodeIP
}

// GetPort returns the kubelet port configured for this client.
func (c *Client) GetPort() int {
	return c.port
}
