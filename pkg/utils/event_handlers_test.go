// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"sync"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// testLogger collects log entries for verification.
type testLogger struct {
	mu      sync.Mutex
	entries []any
}

func (l *testLogger) Log(entry any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
	return nil
}

func (l *testLogger) getEntries() []any {
	l.mu.Lock()
	defer l.mu.Unlock()
	dst := make([]any, len(l.entries))
	copy(dst, l.entries)
	return dst
}

// testObj implements metav1.Object for use in tests.
type testObj struct {
	metav1.ObjectMeta
}

type logEntry struct {
	Name      string
	EventType string
}

func TestSetupEventHandlers_AddFunc(t *testing.T) {
	fakeInformer := &fakeInformer{}

	logger := &testLogger{}
	var hasSynced atomic.Bool
	hasSynced.Store(true) // Simulate post-sync state

	var capturedEntry logEntry

	SetupEventHandlers(fakeInformer, func(obj *testObj, eventType string) any {
		capturedEntry = logEntry{Name: obj.Name, EventType: eventType}
		return capturedEntry
	}, func(obj *testObj) bool {
		return true
	}, logger, &hasSynced)

	// Trigger AddFunc
	obj := &testObj{ObjectMeta: metav1.ObjectMeta{Name: "test-resource"}}
	fakeInformer.triggerAdd(obj)

	if capturedEntry.Name != "test-resource" {
		t.Errorf("Expected name 'test-resource', got '%s'", capturedEntry.Name)
	}
	if capturedEntry.EventType != "created" {
		t.Errorf("Expected event type 'created', got '%s'", capturedEntry.EventType)
	}
	entries := logger.getEntries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(entries))
	}
}

func TestSetupEventHandlers_DeleteFunc(t *testing.T) {
	fakeInformer := &fakeInformer{}

	logger := &testLogger{}
	var hasSynced atomic.Bool
	hasSynced.Store(true)

	var capturedEntry logEntry

	SetupEventHandlers(fakeInformer, func(obj *testObj, eventType string) any {
		capturedEntry = logEntry{Name: obj.Name, EventType: eventType}
		return capturedEntry
	}, func(obj *testObj) bool {
		return true
	}, logger, &hasSynced)

	// Trigger DeleteFunc
	now := metav1.Now()
	obj := &testObj{ObjectMeta: metav1.ObjectMeta{
		Name:              "deleted-resource",
		DeletionTimestamp: &now,
	}}
	fakeInformer.triggerDelete(obj)

	if capturedEntry.Name != "deleted-resource" {
		t.Errorf("Expected name 'deleted-resource', got '%s'", capturedEntry.Name)
	}
	if capturedEntry.EventType != "deleted" {
		t.Errorf("Expected event type 'deleted', got '%s'", capturedEntry.EventType)
	}
}

func TestSetupEventHandlers_DeleteFunc_Tombstone(t *testing.T) {
	fakeInformer := &fakeInformer{}

	logger := &testLogger{}
	var hasSynced atomic.Bool
	hasSynced.Store(true)

	var capturedEntry logEntry

	SetupEventHandlers(fakeInformer, func(obj *testObj, eventType string) any {
		capturedEntry = logEntry{Name: obj.Name, EventType: eventType}
		return capturedEntry
	}, func(obj *testObj) bool {
		return true
	}, logger, &hasSynced)

	// Trigger DeleteFunc with tombstone
	obj := &testObj{ObjectMeta: metav1.ObjectMeta{Name: "tombstone-resource"}}
	tombstone := cache.DeletedFinalStateUnknown{Key: "tombstone-resource", Obj: obj}
	fakeInformer.triggerDelete(tombstone)

	if capturedEntry.Name != "tombstone-resource" {
		t.Errorf("Expected name 'tombstone-resource', got '%s'", capturedEntry.Name)
	}
	if capturedEntry.EventType != "deleted" {
		t.Errorf("Expected event type 'deleted', got '%s'", capturedEntry.EventType)
	}
}

func TestSetupEventHandlers_SuppressedDuringSync(t *testing.T) {
	fakeInformer := &fakeInformer{}

	logger := &testLogger{}
	var hasSynced atomic.Bool
	// hasSynced is false (default) — simulating initial sync

	SetupEventHandlers(fakeInformer, func(obj *testObj, eventType string) any {
		return logEntry{Name: obj.Name, EventType: eventType}
	}, func(obj *testObj) bool {
		return true
	}, logger, &hasSynced)

	// Trigger AddFunc during sync — should be suppressed
	obj := &testObj{ObjectMeta: metav1.ObjectMeta{Name: "syncing-resource"}}
	fakeInformer.triggerAdd(obj)

	entries := logger.getEntries()
	if len(entries) != 0 {
		t.Errorf("Expected 0 log entries during sync, got %d", len(entries))
	}

	// Now enable sync
	hasSynced.Store(true)
	fakeInformer.triggerAdd(obj)

	entries = logger.getEntries()
	if len(entries) != 1 {
		t.Errorf("Expected 1 log entry after sync, got %d", len(entries))
	}
}

func TestSetupEventHandlers_NamespaceFiltering(t *testing.T) {
	fakeInformer := &fakeInformer{}

	logger := &testLogger{}
	var hasSynced atomic.Bool
	hasSynced.Store(true)

	SetupEventHandlers(fakeInformer, func(obj *testObj, eventType string) any {
		return logEntry{Name: obj.Name, EventType: eventType}
	}, func(obj *testObj) bool {
		// Only include objects in "allowed-ns"
		return obj.Namespace == "allowed-ns"
	}, logger, &hasSynced)

	// Add object in allowed namespace
	obj1 := &testObj{ObjectMeta: metav1.ObjectMeta{Name: "allowed", Namespace: "allowed-ns"}}
	fakeInformer.triggerAdd(obj1)

	// Add object in different namespace — should be filtered
	obj2 := &testObj{ObjectMeta: metav1.ObjectMeta{Name: "filtered", Namespace: "other-ns"}}
	fakeInformer.triggerAdd(obj2)

	entries := logger.getEntries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 log entry (filtered), got %d", len(entries))
	}
}

// fakeInformer implements EventHandlerRegistrar for testing.
type fakeInformer struct {
	handlers []cache.ResourceEventHandler
}

func (f *fakeInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	f.handlers = append(f.handlers, handler)
	return nil, nil
}

func (f *fakeInformer) triggerAdd(obj interface{}) {
	for _, h := range f.handlers {
		h.OnAdd(obj, false)
	}
}

func (f *fakeInformer) triggerDelete(obj interface{}) {
	for _, h := range f.handlers {
		h.OnDelete(obj)
	}
}
