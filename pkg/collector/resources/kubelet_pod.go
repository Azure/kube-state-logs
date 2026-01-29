package resources

import (
	"context"
	"time"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// KubeletPodHandler handles collection of pod data from the kubelet API.
// Unlike PodHandler, this doesn't use informers since the kubelet API
// doesn't support watches - it polls the kubelet API directly.
type KubeletPodHandler struct {
	client *kubelet.Client
}

// NewKubeletPodHandler creates a new KubeletPodHandler.
func NewKubeletPodHandler(client *kubelet.Client) *KubeletPodHandler {
	return &KubeletPodHandler{
		client: client,
	}
}

// Collect gathers pod data from the kubelet API.
func (h *KubeletPodHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	pods, err := h.client.GetPods(ctx)
	if err != nil {
		return nil, err
	}

	listTime := time.Now()

	for i := range pods {
		pod := &pods[i]

		if !utils.ShouldIncludeNamespace(namespaces, pod.Namespace) {
			continue
		}

		entry := CreatePodLogEntry(pod)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}
