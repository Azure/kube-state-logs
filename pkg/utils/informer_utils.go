package utils

import (
	"k8s.io/client-go/tools/cache"
)

// SafeGetStoreList safely gets the list from an informer store with nil checks
func SafeGetStoreList(informer cache.SharedIndexInformer) []any {
	if informer == nil {
		return []any{}
	}

	store := informer.GetStore()
	if store == nil {
		return []any{}
	}

	return store.List()
}
