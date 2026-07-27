// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

type nodeLabelPromoter struct {
	labelNames map[string]struct{}
	informer   cache.SharedIndexInformer
}

func newNodeLabelPromoter(labelNames []string) nodeLabelPromoter {
	filter := make(map[string]struct{}, len(labelNames))
	for _, labelName := range labelNames {
		if labelName != "" {
			filter[labelName] = struct{}{}
		}
	}

	return nodeLabelPromoter{labelNames: filter}
}

func (p *nodeLabelPromoter) setupInformer(factory informers.SharedInformerFactory) {
	if len(p.labelNames) == 0 {
		return
	}
	p.informer = factory.Core().V1().Nodes().Informer()
}

func (p *nodeLabelPromoter) labelsForNode(nodeName string) map[string]string {
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
