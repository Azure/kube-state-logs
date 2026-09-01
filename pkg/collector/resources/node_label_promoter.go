// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type nodeLabelPromoter struct {
	labelNames map[string]struct{}
	informer   cache.SharedIndexInformer
	client     kubernetes.Interface
	nodeName   string

	mu           *sync.Mutex
	cachedLabels map[string]string
	cacheUntil   time.Time
}

func newNodeLabelPromoter(labelNames []string) nodeLabelPromoter {
	filter := make(map[string]struct{}, len(labelNames))
	for _, labelName := range labelNames {
		if labelName != "" {
			filter[labelName] = struct{}{}
		}
	}

	return nodeLabelPromoter{labelNames: filter, mu: &sync.Mutex{}}
}

func (p *nodeLabelPromoter) setupInformer(factory informers.SharedInformerFactory) {
	if len(p.labelNames) == 0 || p.client != nil {
		return
	}
	p.informer = factory.Core().V1().Nodes().Informer()
}

func (p *nodeLabelPromoter) useDirectLookup(client kubernetes.Interface, nodeName string) {
	if len(p.labelNames) == 0 || client == nil || nodeName == "" {
		return
	}
	p.client = client
	p.nodeName = nodeName
}

func (p *nodeLabelPromoter) labelsForNode(nodeName string) map[string]string {
	if p.client != nil {
		return p.labelsFromAPI(nodeName)
	}
	if p.informer == nil || nodeName == "" {
		return nil
	}

	obj, exists, err := p.informer.GetStore().GetByKey(nodeName)
	if err != nil || !exists {
		return nil
	}

	node, ok := obj.(*corev1.Node)
	if !ok {
		return nil
	}

	var labels map[string]string
	for labelName := range p.labelNames {
		if value, exists := node.Labels[labelName]; exists {
			if labels == nil {
				labels = make(map[string]string, len(p.labelNames))
			}
			labels[labelName] = value
		}
	}

	return labels
}

func (p *nodeLabelPromoter) labelsFromAPI(nodeName string) map[string]string {
	if nodeName == "" || nodeName != p.nodeName {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Now().Before(p.cacheUntil) {
		return cloneStringMap(p.cachedLabels)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	node, err := p.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		klog.Warningf("Failed to retrieve labels for node %s: %v", nodeName, err)
		p.cacheUntil = time.Now().Add(time.Minute)
		return cloneStringMap(p.cachedLabels)
	}

	p.cachedLabels = filterNodeLabels(node.Labels, p.labelNames)
	p.cacheUntil = time.Now().Add(5 * time.Minute)
	return cloneStringMap(p.cachedLabels)
}

func filterNodeLabels(nodeLabels map[string]string, labelNames map[string]struct{}) map[string]string {
	var labels map[string]string
	for labelName := range labelNames {
		if value, exists := nodeLabels[labelName]; exists {
			if labels == nil {
				labels = make(map[string]string, len(labelNames))
			}
			labels[labelName] = value
		}
	}
	return labels
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
