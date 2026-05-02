package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/lukaszraczylo/kubemirror/pkg/circuitbreaker"
	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/filter"
)

func TestNamespaceReconciler_CleanupWhenNamespaceNoLongerTarget(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name              string
		namespace         *corev1.Namespace
		sourceResources   []*unstructured.Unstructured
		existingMirrors   []*unstructured.Unstructured
		expectedDeleted   []string // mirror names that should be deleted
		expectedRemaining []string // mirror names that should remain
	}{
		{
			name: "namespace label changes to allow-mirrors=false, mirror should be deleted",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Labels: map[string]string{
						constants.LabelAllowMirrors: "false", // Changed to false
					},
				},
			},
			sourceResources: []*unstructured.Unstructured{
				makeUnstructuredSecret("test-secret", "default", map[string]string{
					constants.LabelEnabled: "true",
				}, map[string]string{
					constants.AnnotationSync:             "true",
					constants.AnnotationTargetNamespaces: "all",
				}),
			},
			existingMirrors: []*unstructured.Unstructured{
				makeUnstructuredMirror("test-secret", "target-ns", "default", "test-secret"),
			},
			expectedDeleted:   []string{"test-secret"},
			expectedRemaining: []string{},
		},
		{
			name: "namespace no longer matches pattern, mirror should be deleted",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "staging-1",
				},
			},
			sourceResources: []*unstructured.Unstructured{
				makeUnstructuredSecret("test-secret", "default", map[string]string{
					constants.LabelEnabled: "true",
				}, map[string]string{
					constants.AnnotationSync:             "true",
					constants.AnnotationTargetNamespaces: "prod-*", // Pattern changed, no longer matches staging-*
				}),
			},
			existingMirrors: []*unstructured.Unstructured{
				makeUnstructuredMirror("test-secret", "staging-1", "default", "test-secret"),
			},
			expectedDeleted:   []string{"test-secret"},
			expectedRemaining: []string{},
		},
		{
			name: "namespace becomes valid target, no existing mirror, should be created",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "prod-1",
				},
			},
			sourceResources: []*unstructured.Unstructured{
				makeUnstructuredSecret("test-secret", "default", map[string]string{
					constants.LabelEnabled: "true",
				}, map[string]string{
					constants.AnnotationSync:             "true",
					constants.AnnotationTargetNamespaces: "prod-*",
				}),
			},
			existingMirrors:   []*unstructured.Unstructured{},
			expectedDeleted:   []string{},
			expectedRemaining: []string{"test-secret"}, // Should be created
		},
		{
			name: "namespace still valid, mirror remains",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "prod-1",
				},
			},
			sourceResources: []*unstructured.Unstructured{
				makeUnstructuredSecret("test-secret", "default", map[string]string{
					constants.LabelEnabled: "true",
				}, map[string]string{
					constants.AnnotationSync:             "true",
					constants.AnnotationTargetNamespaces: "prod-*",
				}),
			},
			existingMirrors: []*unstructured.Unstructured{
				makeUnstructuredMirror("test-secret", "prod-1", "default", "test-secret"),
			},
			expectedDeleted:   []string{},
			expectedRemaining: []string{"test-secret"},
		},
		{
			name: "multiple sources, only non-matching mirrors deleted",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "app-1",
				},
			},
			sourceResources: []*unstructured.Unstructured{
				makeUnstructuredSecret("secret-1", "default", map[string]string{
					constants.LabelEnabled: "true",
				}, map[string]string{
					constants.AnnotationSync:             "true",
					constants.AnnotationTargetNamespaces: "app-*", // Matches
				}),
				makeUnstructuredSecret("secret-2", "default", map[string]string{
					constants.LabelEnabled: "true",
				}, map[string]string{
					constants.AnnotationSync:             "true",
					constants.AnnotationTargetNamespaces: "prod-*", // Doesn't match
				}),
			},
			existingMirrors: []*unstructured.Unstructured{
				makeUnstructuredMirror("secret-1", "app-1", "default", "secret-1"),
				makeUnstructuredMirror("secret-2", "app-1", "default", "secret-2"),
			},
			expectedDeleted:   []string{"secret-2"},
			expectedRemaining: []string{"secret-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with namespace, sources, and existing mirrors
			objects := []client.Object{tt.namespace}
			for _, src := range tt.sourceResources {
				objects = append(objects, src)
			}
			for _, mirror := range tt.existingMirrors {
				objects = append(objects, mirror)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create namespace lister mock
			mockLister := &mockNamespaceLister{
				namespaces: []string{tt.namespace.Name},
				allowMirrors: func() map[string]bool {
					result := make(map[string]bool)
					if tt.namespace.Labels[constants.LabelAllowMirrors] == "true" {
						result[tt.namespace.Name] = true
					}
					return result
				}(),
				optOut: func() map[string]bool {
					result := make(map[string]bool)
					if tt.namespace.Labels[constants.LabelAllowMirrors] == "false" {
						result[tt.namespace.Name] = true
					}
					return result
				}(),
			}

			// Create reconciler
			reconciler := &NamespaceReconciler{
				Client:          fakeClient,
				Scheme:          scheme,
				Config:          &config.Config{MaxTargetsPerResource: 100},
				Filter:          filter.NewNamespaceFilter([]string{"kube-system"}, []string{}),
				NamespaceLister: mockLister,
				ResourceTypes: []config.ResourceType{
					{Group: "", Version: "v1", Kind: "Secret"},
				},
			}

			// Reconcile the namespace
			ctx := context.Background()
			req := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name: tt.namespace.Name,
				},
			}
			result, err := reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			// Regression: never schedule an unconditional re-reconcile. The
			// previous implementation returned RequeueAfter=3s for every
			// namespace event, which scaled to one re-reconcile per namespace
			// every 3 seconds forever.
			assert.Zero(t, result.RequeueAfter, "happy-path Reconcile must not schedule a periodic requeue")

			// Verify mirrors were deleted as expected
			for _, mirrorName := range tt.expectedDeleted {
				mirror := &unstructured.Unstructured{}
				mirror.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Secret"})
				err := fakeClient.Get(ctx, client.ObjectKey{
					Namespace: tt.namespace.Name,
					Name:      mirrorName,
				}, mirror)
				assert.True(t, errors.IsNotFound(err),
					"mirror %s should be deleted in namespace %s", mirrorName, tt.namespace.Name)
			}

			// Verify mirrors remain as expected
			for _, mirrorName := range tt.expectedRemaining {
				mirror := &unstructured.Unstructured{}
				mirror.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Secret"})
				err := fakeClient.Get(ctx, client.ObjectKey{
					Namespace: tt.namespace.Name,
					Name:      mirrorName,
				}, mirror)
				assert.NoError(t, err,
					"mirror %s should exist in namespace %s", mirrorName, tt.namespace.Name)
			}
		})
	}
}
func TestNamespaceReconciler_newSourceReconciler_forwardsAPIReaderAndCircuitBreaker(t *testing.T) {
	// Regression test (H4): the SourceReconciler that NamespaceReconciler builds
	// for delegated mirror reconciliation must carry the APIReader and the
	// CircuitBreaker, otherwise namespace-driven mirror updates silently bypass
	// --verify-source-freshness and the per-resource failure throttling.
	apiReader := &stubAPIReader{}
	cb := circuitbreaker.NewWithDefaults()

	r := &NamespaceReconciler{
		APIReader:      apiReader,
		CircuitBreaker: cb,
		Config:         &config.Config{},
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	sr := r.newSourceReconciler(gvk)

	require.NotNil(t, sr)
	assert.Same(t, apiReader, sr.APIReader, "APIReader must be forwarded")
	assert.Same(t, cb, sr.CircuitBreaker, "CircuitBreaker must be forwarded")
	assert.Equal(t, gvk, sr.GVK)
}

// stubAPIReader is a minimal client.Reader for identity-comparison tests; it
// is never invoked, so the methods only need to satisfy the interface.
type stubAPIReader struct{}

func (s *stubAPIReader) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return nil
}

func (s *stubAPIReader) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return nil
}

// Helper functions

func makeUnstructuredSecret(name, namespace string, labels, annotations map[string]string) *unstructured.Unstructured {
	secret := &unstructured.Unstructured{}
	secret.SetGroupVersionKind(schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Secret",
	})
	secret.SetName(name)
	secret.SetNamespace(namespace)
	secret.SetLabels(labels)
	secret.SetAnnotations(annotations)

	// Set some data
	_ = unstructured.SetNestedMap(secret.Object, map[string]interface{}{
		"key": "dmFsdWU=", // base64("value")
	}, "data")

	return secret
}

func makeUnstructuredMirror(name, namespace, sourceNs, sourceName string) *unstructured.Unstructured {
	mirror := &unstructured.Unstructured{}
	mirror.SetGroupVersionKind(schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Secret",
	})
	mirror.SetName(name)
	mirror.SetNamespace(namespace)
	mirror.SetLabels(map[string]string{
		constants.LabelManagedBy: "kubemirror",
		constants.LabelMirror:    "true",
	})
	mirror.SetAnnotations(map[string]string{
		constants.AnnotationSourceNamespace: sourceNs,
		constants.AnnotationSourceName:      sourceName,
		constants.AnnotationSourceUID:       "test-uid",
	})

	// Set some data
	_ = unstructured.SetNestedMap(mirror.Object, map[string]interface{}{
		"key": "dmFsdWU=",
	}, "data")

	return mirror
}

// Mock namespace lister for testing
type mockNamespaceLister struct {
	allowMirrors map[string]bool
	optOut       map[string]bool
	namespaces   []string
}

func (m *mockNamespaceLister) ListNamespaces(ctx context.Context) ([]string, error) {
	return m.namespaces, nil
}

func (m *mockNamespaceLister) ListAllowMirrorsNamespaces(ctx context.Context) ([]string, error) {
	var result []string
	for ns, allowed := range m.allowMirrors {
		if allowed {
			result = append(result, ns)
		}
	}
	return result, nil
}

func (m *mockNamespaceLister) ListOptOutNamespaces(ctx context.Context) ([]string, error) {
	var result []string
	for ns, optedOut := range m.optOut {
		if optedOut {
			result = append(result, ns)
		}
	}
	return result, nil
}

func (m *mockNamespaceLister) ListNamespacesWithLabels(ctx context.Context) (*NamespaceInfo, error) {
	info := &NamespaceInfo{
		All:          m.namespaces,
		AllowMirrors: make([]string, 0),
		OptOut:       make([]string, 0),
	}

	for ns, allowed := range m.allowMirrors {
		if allowed {
			info.AllowMirrors = append(info.AllowMirrors, ns)
		}
	}

	for ns, optedOut := range m.optOut {
		if optedOut {
			info.OptOut = append(info.OptOut, ns)
		}
	}

	return info, nil
}
