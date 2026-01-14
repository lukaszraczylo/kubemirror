package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
)

// Test helper functions - only available during testing
// These are intentionally not exported methods on DynamicControllerManager
// to avoid exposing them in production code

// getRegisteredCount returns the number of fully registered controllers (test helper)
func getRegisteredCount(d *DynamicControllerManager) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	count := 0
	for _, state := range d.registrationState {
		if state == StateFullyRegistered {
			count++
		}
	}
	return count
}

// getActiveResourceTypes returns the currently active resource types (test helper)
func getActiveResourceTypes(d *DynamicControllerManager) []schema.GroupVersionKind {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]schema.GroupVersionKind, 0, len(d.activeResourceTypes))
	for _, gvk := range d.activeResourceTypes {
		result = append(result, gvk)
	}
	return result
}

func TestDynamicControllerManager_FindActiveResourceTypes(t *testing.T) {
	// NOTE: The fake Kubernetes client has limitations with label selector filtering in LIST operations.
	// These tests verify the logic structure but full label filtering is validated in e2e tests.
	t.Skip("Skipping due to fake client label selector limitations - covered by e2e tests")

	tests := []struct {
		name                string
		availableResources  []config.ResourceType
		existingResources   []*unstructured.Unstructured
		expectedActiveTypes []string
		expectedActiveCount int
	}{
		{
			name: "no resources marked for mirroring",
			availableResources: []config.ResourceType{
				{Group: "", Version: "v1", Kind: "Secret"},
				{Group: "", Version: "v1", Kind: "ConfigMap"},
			},
			existingResources:   []*unstructured.Unstructured{},
			expectedActiveCount: 0,
			expectedActiveTypes: []string{},
		},
		{
			name: "one secret marked for mirroring",
			availableResources: []config.ResourceType{
				{Group: "", Version: "v1", Kind: "Secret"},
				{Group: "", Version: "v1", Kind: "ConfigMap"},
			},
			existingResources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "test-secret",
							"namespace": "default",
							"labels": map[string]interface{}{
								constants.LabelEnabled: "true",
							},
							"annotations": map[string]interface{}{
								constants.AnnotationSync: "true",
							},
						},
						"data": map[string]interface{}{
							"key": "dmFsdWU=", // base64 encoded "value"
						},
					},
				},
			},
			expectedActiveCount: 1,
			expectedActiveTypes: []string{"Secret.v1."},
		},
		{
			name: "both secrets and configmaps marked",
			availableResources: []config.ResourceType{
				{Group: "", Version: "v1", Kind: "Secret"},
				{Group: "", Version: "v1", Kind: "ConfigMap"},
			},
			existingResources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "test-secret",
							"namespace": "default",
							"labels": map[string]interface{}{
								constants.LabelEnabled: "true",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]interface{}{
							"name":      "test-configmap",
							"namespace": "default",
							"labels": map[string]interface{}{
								constants.LabelEnabled: "true",
							},
						},
					},
				},
			},
			expectedActiveCount: 2,
			expectedActiveTypes: []string{"Secret.v1.", "ConfigMap.v1."},
		},
		{
			name: "resources without enabled label are ignored",
			availableResources: []config.ResourceType{
				{Group: "", Version: "v1", Kind: "Secret"},
			},
			existingResources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "test-secret",
							"namespace": "default",
							// No enabled label
						},
					},
				},
			},
			expectedActiveCount: 0,
			expectedActiveTypes: []string{},
		},
		{
			name: "multiple resources of same type count as one active type",
			availableResources: []config.ResourceType{
				{Group: "", Version: "v1", Kind: "Secret"},
			},
			existingResources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "secret-1",
							"namespace": "default",
							"labels": map[string]interface{}{
								constants.LabelEnabled: "true",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "secret-2",
							"namespace": "default",
							"labels": map[string]interface{}{
								constants.LabelEnabled: "true",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "secret-3",
							"namespace": "kube-system",
							"labels": map[string]interface{}{
								constants.LabelEnabled: "true",
							},
						},
					},
				},
			},
			expectedActiveCount: 1,
			expectedActiveTypes: []string{"Secret.v1."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with scheme
			scheme := runtime.NewScheme()

			// Convert unstructured objects to client.Objects
			objects := make([]client.Object, len(tt.existingResources))
			for i, u := range tt.existingResources {
				objects[i] = u
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create dynamic manager
			mgr := &DynamicControllerManager{
				client:                 fakeClient,
				availableResourceTypes: tt.availableResources,
			}

			// Find active resource types
			ctx := context.Background()
			activeTypes, err := mgr.findActiveResourceTypes(ctx)

			// Assertions
			require.NoError(t, err)
			assert.Equal(t, tt.expectedActiveCount, len(activeTypes), "unexpected number of active types")

			// Verify expected types are present
			for _, expectedType := range tt.expectedActiveTypes {
				_, found := activeTypes[expectedType]
				assert.True(t, found, "expected type %s not found in active types", expectedType)
			}
		})
	}
}

func TestDynamicControllerManager_GetRegisteredCount(t *testing.T) {
	mgr := &DynamicControllerManager{
		registrationState: map[string]RegistrationState{
			"Secret.v1.":    StateFullyRegistered,
			"ConfigMap.v1.": StateFullyRegistered,
		},
	}

	count := getRegisteredCount(mgr)
	assert.Equal(t, 2, count)
}

func TestDynamicControllerManager_GetRegisteredCount_PartialStates(t *testing.T) {
	mgr := &DynamicControllerManager{
		registrationState: map[string]RegistrationState{
			"Secret.v1.":     StateFullyRegistered,
			"ConfigMap.v1.":  StateSourceOnly,    // Partial - shouldn't count
			"Deployment.v1.": StateNotRegistered, // Not registered - shouldn't count
		},
	}

	count := getRegisteredCount(mgr)
	assert.Equal(t, 1, count, "only fully registered controllers should be counted")
}

func TestDynamicControllerManager_GetActiveResourceTypes(t *testing.T) {
	secretGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	mgr := &DynamicControllerManager{
		activeResourceTypes: map[string]schema.GroupVersionKind{
			"Secret.v1.":    secretGVK,
			"ConfigMap.v1.": configMapGVK,
		},
	}

	activeTypes := getActiveResourceTypes(mgr)
	assert.Equal(t, 2, len(activeTypes))

	// Verify both GVKs are present
	foundSecret := false
	foundConfigMap := false
	for _, gvk := range activeTypes {
		if gvk == secretGVK {
			foundSecret = true
		}
		if gvk == configMapGVK {
			foundConfigMap = true
		}
	}

	assert.True(t, foundSecret, "Secret GVK not found")
	assert.True(t, foundConfigMap, "ConfigMap GVK not found")
}

func TestDynamicControllerManager_ScanInterval(t *testing.T) {
	tests := []struct {
		name             string
		configInterval   time.Duration
		expectedInterval time.Duration
	}{
		{
			name:             "default interval when zero",
			configInterval:   0,
			expectedInterval: 5 * time.Minute,
		},
		{
			name:             "custom interval",
			configInterval:   10 * time.Minute,
			expectedInterval: 10 * time.Minute,
		},
		{
			name:             "short interval",
			configInterval:   30 * time.Second,
			expectedInterval: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewDynamicControllerManager(DynamicManagerConfig{
				ScanInterval: tt.configInterval,
			})

			assert.Equal(t, tt.expectedInterval, mgr.scanInterval)
		})
	}
}

func TestDynamicControllerManager_RegistrationTracking(t *testing.T) {
	// Test that registration tracking works correctly
	mgr := &DynamicControllerManager{
		registrationState:   make(map[string]RegistrationState),
		activeResourceTypes: make(map[string]schema.GroupVersionKind),
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	gvkStr := "Secret.v1."

	// Initially not registered
	assert.Equal(t, StateNotRegistered, mgr.registrationState[gvkStr])
	assert.Equal(t, 0, getRegisteredCount(mgr))

	// Mark as fully registered
	mgr.registrationState[gvkStr] = StateFullyRegistered
	mgr.activeResourceTypes[gvkStr] = gvk

	assert.Equal(t, StateFullyRegistered, mgr.registrationState[gvkStr])
	assert.Equal(t, 1, getRegisteredCount(mgr))

	activeTypes := getActiveResourceTypes(mgr)
	assert.Equal(t, 1, len(activeTypes))
	assert.Equal(t, gvk, activeTypes[0])
}

func TestDynamicControllerManager_PartialRegistration(t *testing.T) {
	// Test that partial registration (source only) is tracked correctly
	mgr := &DynamicControllerManager{
		registrationState:   make(map[string]RegistrationState),
		activeResourceTypes: make(map[string]schema.GroupVersionKind),
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	gvkStr := "Secret.v1."

	// Mark as partially registered (source only)
	mgr.registrationState[gvkStr] = StateSourceOnly
	mgr.activeResourceTypes[gvkStr] = gvk

	// Should not count as registered
	assert.Equal(t, StateSourceOnly, mgr.registrationState[gvkStr])
	assert.Equal(t, 0, getRegisteredCount(mgr), "partial registration should not count as fully registered")

	// But should be in active resource types
	activeTypes := getActiveResourceTypes(mgr)
	assert.Equal(t, 1, len(activeTypes))

	// Complete the registration
	mgr.registrationState[gvkStr] = StateFullyRegistered
	assert.Equal(t, 1, getRegisteredCount(mgr), "should now count as fully registered")
}

func TestDynamicControllerManager_GetRegistrationStats(t *testing.T) {
	mgr := &DynamicControllerManager{
		registrationState: map[string]RegistrationState{
			"Secret.v1.":     StateFullyRegistered,
			"ConfigMap.v1.":  StateFullyRegistered,
			"Deployment.v1.": StateSourceOnly,
			"Service.v1.":    StateSourceOnly,
			"Ingress.v1.":    StateNotRegistered,
		},
	}

	fullyReg, sourceOnly, notReg := mgr.GetRegistrationStats()

	assert.Equal(t, 2, fullyReg, "should have 2 fully registered")
	assert.Equal(t, 2, sourceOnly, "should have 2 source-only")
	assert.Equal(t, 1, notReg, "should have 1 not registered")
}

func TestDynamicControllerManager_GetRegistrationState(t *testing.T) {
	mgr := &DynamicControllerManager{
		registrationState: map[string]RegistrationState{
			"Secret.v1.":    StateFullyRegistered,
			"ConfigMap.v1.": StateSourceOnly,
		},
	}

	assert.Equal(t, StateFullyRegistered, mgr.GetRegistrationState("Secret.v1."))
	assert.Equal(t, StateSourceOnly, mgr.GetRegistrationState("ConfigMap.v1."))
	assert.Equal(t, StateNotRegistered, mgr.GetRegistrationState("Unknown.v1."), "unknown GVK should be not registered")
}

func TestRegistrationState_String(t *testing.T) {
	tests := []struct {
		expected string
		state    RegistrationState
	}{
		{"not-registered", StateNotRegistered},
		{"source-only", StateSourceOnly},
		{"fully-registered", StateFullyRegistered},
		{"unknown", RegistrationState(99)},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

// TestDynamicControllerManager_ConcurrentAccess tests thread-safety
func TestDynamicControllerManager_ConcurrentAccess(t *testing.T) {
	mgr := &DynamicControllerManager{
		registrationState:   make(map[string]RegistrationState),
		activeResourceTypes: make(map[string]schema.GroupVersionKind),
	}

	// Simulate concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			mgr.mu.Lock()
			mgr.registrationState["test"] = StateFullyRegistered
			mgr.mu.Unlock()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Reader goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = getRegisteredCount(mgr)
				_ = getActiveResourceTypes(mgr)
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 6; i++ {
		<-done
	}

	// Should not panic and should have final state
	assert.Equal(t, StateFullyRegistered, mgr.registrationState["test"])
}

func TestDynamicControllerManager_UnstructuredResourceHandling(t *testing.T) {
	// Test handling of custom resources via unstructured
	scheme := runtime.NewScheme()

	// Create an unstructured middleware (simulating a Traefik CRD)
	// Note: Use int64 instead of int to avoid deep copy issues
	middleware := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "traefik.io/v1alpha1",
			"kind":       "Middleware",
			"metadata": map[string]interface{}{
				"name":      "test-middleware",
				"namespace": "default",
				"labels": map[string]interface{}{
					constants.LabelEnabled: "true",
				},
			},
			"spec": map[string]interface{}{
				"compress": map[string]interface{}{
					"minResponseBodyBytes": int64(1024), // Use int64 for Kubernetes compatibility
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(middleware).
		Build()

	availableResources := []config.ResourceType{
		{Group: "traefik.io", Version: "v1alpha1", Kind: "Middleware"},
	}

	mgr := &DynamicControllerManager{
		client:                 fakeClient,
		availableResourceTypes: availableResources,
	}

	ctx := context.Background()
	activeTypes, err := mgr.findActiveResourceTypes(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, len(activeTypes), "should find the middleware as active")

	_, found := activeTypes["Middleware.v1alpha1.traefik.io"]
	assert.True(t, found, "middleware type should be in active types")
}
