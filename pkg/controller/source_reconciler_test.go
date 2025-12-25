package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/filter"
)

// MockClient is a mock implementation of client.Client for testing.
type MockClient struct {
	mock.Mock
}

func (m *MockClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj)
	if args.Error(0) != nil {
		return args.Error(0)
	}
	// Copy the mock object into obj
	if mockObj := args.Get(1); mockObj != nil {
		switch v := mockObj.(type) {
		case *corev1.Secret:
			*obj.(*corev1.Secret) = *v
		case *corev1.ConfigMap:
			*obj.(*corev1.ConfigMap) = *v
		case *unstructured.Unstructured:
			// Copy the unstructured object
			*obj.(*unstructured.Unstructured) = *v
		}
	}
	return nil
}

func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

func (m *MockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Status() client.StatusWriter {
	args := m.Called()
	return args.Get(0).(client.StatusWriter)
}

func (m *MockClient) Scheme() *runtime.Scheme {
	args := m.Called()
	return args.Get(0).(*runtime.Scheme)
}

func (m *MockClient) RESTMapper() meta.RESTMapper {
	args := m.Called()
	return args.Get(0).(meta.RESTMapper)
}

func (m *MockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	args := m.Called(obj)
	return args.Get(0).(schema.GroupVersionKind), args.Error(1)
}

func (m *MockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	args := m.Called(obj)
	return args.Bool(0), args.Error(1)
}

func (m *MockClient) SubResource(subResource string) client.SubResourceClient {
	args := m.Called(subResource)
	return args.Get(0).(client.SubResourceClient)
}

// MockNamespaceLister is a mock implementation of NamespaceLister for testing.
type MockNamespaceLister struct {
	mock.Mock
}

func (m *MockNamespaceLister) ListNamespaces(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockNamespaceLister) ListAllowMirrorsNamespaces(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func TestIsEnabledForMirroring(t *testing.T) {
	tests := []struct {
		obj  metav1.Object
		name string
		want bool
	}{
		{
			name: "enabled with both label and annotation",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelEnabled: "true",
					},
					Annotations: map[string]string{
						constants.AnnotationSync: "true",
					},
				},
			},
			want: true,
		},
		{
			name: "missing label",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationSync: "true",
					},
				},
			},
			want: false,
		},
		{
			name: "missing annotation",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelEnabled: "true",
					},
				},
			},
			want: false,
		},
		{
			name: "label set to false",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelEnabled: "false",
					},
					Annotations: map[string]string{
						constants.AnnotationSync: "true",
					},
				},
			},
			want: false,
		},
		{
			name: "no labels or annotations",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEnabledForMirroring(tt.obj)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSourceReconciler_resolveTargetNamespaces(t *testing.T) {
	tests := []struct {
		name                   string
		sourceAnnotations      map[string]string
		allNamespaces          []string
		allowMirrorsNamespaces []string
		sourceNamespace        string
		wantContains           []string
		wantNotContains        []string
		wantError              bool
		expectListCalls        bool
	}{
		{
			name: "no target annotation",
			sourceAnnotations: map[string]string{
				constants.AnnotationSync: "true",
			},
			allNamespaces:   []string{"app1", "app2"},
			sourceNamespace: "default",
			wantContains:    nil,
			expectListCalls: false,
		},
		{
			name: "single target namespace",
			sourceAnnotations: map[string]string{
				constants.AnnotationTargetNamespaces: "app1",
			},
			allNamespaces:   []string{"app1", "app2", "default"},
			sourceNamespace: "default",
			wantContains:    []string{"app1"},
			wantNotContains: []string{"app2", "default"},
			expectListCalls: true,
		},
		{
			name: "multiple target namespaces",
			sourceAnnotations: map[string]string{
				constants.AnnotationTargetNamespaces: "app1,app2",
			},
			allNamespaces:   []string{"app1", "app2", "app3", "default"},
			sourceNamespace: "default",
			wantContains:    []string{"app1", "app2"},
			wantNotContains: []string{"app3", "default"},
			expectListCalls: true,
		},
		{
			name: "all keyword",
			sourceAnnotations: map[string]string{
				constants.AnnotationTargetNamespaces: "all",
			},
			allNamespaces:   []string{"app1", "app2", "default"},
			sourceNamespace: "default",
			wantContains:    []string{"app1", "app2"},
			wantNotContains: []string{"default"}, // source excluded
			expectListCalls: true,
		},
		{
			name: "pattern matching",
			sourceAnnotations: map[string]string{
				constants.AnnotationTargetNamespaces: "app-*",
			},
			allNamespaces:   []string{"app-frontend", "app-backend", "prod-api", "default"},
			sourceNamespace: "default",
			wantContains:    []string{"app-frontend", "app-backend"},
			wantNotContains: []string{"prod-api", "default"},
			expectListCalls: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLister := new(MockNamespaceLister)

			if tt.expectListCalls {
				mockLister.On("ListNamespaces", mock.Anything).Return(tt.allNamespaces, nil)
				mockLister.On("ListAllowMirrorsNamespaces", mock.Anything).Return(tt.allowMirrorsNamespaces, nil)
			}

			r := &SourceReconciler{
				Config:          &config.Config{},
				Filter:          filter.NewNamespaceFilter([]string{}, []string{}),
				NamespaceLister: mockLister,
			}

			sourceObj := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-secret",
					Namespace:   tt.sourceNamespace,
					Annotations: tt.sourceAnnotations,
				},
			}

			got, err := r.resolveTargetNamespaces(context.Background(), sourceObj)

			if tt.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.wantContains != nil {
				for _, ns := range tt.wantContains {
					assert.Contains(t, got, ns)
				}
			}

			if tt.wantNotContains != nil {
				for _, ns := range tt.wantNotContains {
					assert.NotContains(t, got, ns)
				}
			}

			if tt.expectListCalls {
				mockLister.AssertExpectations(t)
			}
		})
	}
}

func TestSourceReconciler_Reconcile_MirrorResource(t *testing.T) {
	// Test that mirrors are not reconciled as sources
	mockClient := new(MockClient)
	mockLister := new(MockNamespaceLister)

	r := &SourceReconciler{
		Client:          mockClient,
		Scheme:          runtime.NewScheme(),
		Config:          &config.Config{},
		Filter:          filter.NewNamespaceFilter([]string{}, []string{}),
		NamespaceLister: mockLister,
		GVK: schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Secret",
		},
	}

	// Create a mirror resource (has the mirror label) as unstructured
	mirrorSecret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "test-secret",
				"namespace": "app1",
				"labels": map[string]interface{}{
					constants.LabelManagedBy: constants.ControllerName,
					constants.LabelMirror:    "true",
				},
			},
		},
	}

	mockClient.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(nil, mirrorSecret)

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "app1",
			Name:      "test-secret",
		},
	}

	result, err := r.Reconcile(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	mockClient.AssertExpectations(t)
}

func TestSourceReconciler_Reconcile_NotFound(t *testing.T) {
	// Test that deleted resources are handled gracefully
	mockClient := new(MockClient)
	mockLister := new(MockNamespaceLister)

	r := &SourceReconciler{
		Client:          mockClient,
		Scheme:          runtime.NewScheme(),
		Config:          &config.Config{},
		Filter:          filter.NewNamespaceFilter([]string{}, []string{}),
		NamespaceLister: mockLister,
		GVK: schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Secret",
		},
	}

	notFoundErr := errors.NewNotFound(schema.GroupResource{
		Group:    "",
		Resource: "secrets",
	}, "test-secret")

	mockClient.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(notFoundErr, (*unstructured.Unstructured)(nil))

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-secret",
		},
	}

	result, err := r.Reconcile(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	mockClient.AssertExpectations(t)
}

// Benchmark tests for performance-critical paths

func BenchmarkIsEnabledForMirroring(b *testing.B) {
	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				constants.LabelEnabled: "true",
			},
			Annotations: map[string]string{
				constants.AnnotationSync: "true",
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = isEnabledForMirroring(obj)
	}
}

func BenchmarkResolveTargetNamespaces(b *testing.B) {
	mockLister := new(MockNamespaceLister)
	allNamespaces := make([]string, 100)
	for i := 0; i < 100; i++ {
		allNamespaces[i] = fmt.Sprintf("namespace-%d", i)
	}
	mockLister.On("ListNamespaces", mock.Anything).Return(allNamespaces, nil)
	mockLister.On("ListAllowMirrorsNamespaces", mock.Anything).Return(allNamespaces[:50], nil)

	r := &SourceReconciler{
		Config:          &config.Config{},
		Filter:          filter.NewNamespaceFilter([]string{}, []string{}),
		NamespaceLister: mockLister,
	}

	sourceObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
			Annotations: map[string]string{
				constants.AnnotationTargetNamespaces: "all",
			},
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = r.resolveTargetNamespaces(ctx, sourceObj)
	}
}
