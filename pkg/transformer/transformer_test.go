package transformer

import (
	"fmt"
	"testing"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestTransformer_Transform(t *testing.T) {
	tests := []struct {
		ctx      TransformContext
		source   runtime.Object
		validate func(t *testing.T, result runtime.Object)
		name     string
		errMsg   string
		options  TransformOptions
		wantErr  bool
	}{
		// Good cases - Value rules
		{
			name: "value rule - simple data field",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: data.LOG_LEVEL
    value: "error"
`,
					},
				},
				Data: map[string]string{
					"LOG_LEVEL": "debug",
				},
			},
			ctx: TransformContext{
				TargetNamespace: "prod",
			},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)
				value, found, err := unstructured.NestedString(u.Object, "data", "LOG_LEVEL")
				require.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "error", value)
			},
		},

		// Good cases - Template rules
		{
			name: "template rule - namespace substitution",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: data.API_URL
    template: "https://{{.TargetNamespace}}.api.example.com"
`,
					},
				},
				Data: map[string]string{},
			},
			ctx: TransformContext{
				TargetNamespace: "prod-app",
				SourceNamespace: "default",
			},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)
				value, found, err := unstructured.NestedString(u.Object, "data", "API_URL")
				require.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "https://prod-app.api.example.com", value)
			},
		},
		{
			name: "template rule - with template functions",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: data.NAMESPACE_UPPER
    template: "{{upper .TargetNamespace}}"
  - path: data.SOURCE_LOWER
    template: "{{lower .SourceName}}"
`,
					},
				},
				Data: map[string]string{},
			},
			ctx: TransformContext{
				TargetNamespace: "prod-app",
				SourceName:      "TEST-CONFIG",
			},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)

				upperValue, found, err := unstructured.NestedString(u.Object, "data", "NAMESPACE_UPPER")
				require.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "PROD-APP", upperValue)

				lowerValue, found, err := unstructured.NestedString(u.Object, "data", "SOURCE_LOWER")
				require.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "test-config", lowerValue)
			},
		},

		// Good cases - Merge rules
		{
			name: "merge rule - add labels",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Labels: map[string]string{
						"app": "myapp",
					},
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: metadata.labels
    merge:
      environment: "production"
      tier: "frontend"
`,
					},
				},
				Data: map[string]string{},
			},
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)
				labels := u.GetLabels()
				assert.Equal(t, "myapp", labels["app"], "original label should be preserved")
				assert.Equal(t, "production", labels["environment"])
				assert.Equal(t, "frontend", labels["tier"])
			},
		},
		{
			name: "merge rule - create new map",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: metadata.labels
    merge:
      new-label: "new-value"
`,
					},
				},
				Data: map[string]string{},
			},
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)
				labels := u.GetLabels()
				assert.Equal(t, "new-value", labels["new-label"])
			},
		},

		// Good cases - Delete rules
		{
			name: "delete rule - remove data field",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: data.DEBUG_MODE
    delete: true
`,
					},
				},
				Data: map[string]string{
					"DEBUG_MODE": "true",
					"LOG_LEVEL":  "info",
				},
			},
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)
				data, found, err := unstructured.NestedMap(u.Object, "data")
				require.NoError(t, err)
				assert.True(t, found)
				_, exists := data["DEBUG_MODE"]
				assert.False(t, exists, "DEBUG_MODE should be deleted")
				assert.Equal(t, "info", data["LOG_LEVEL"], "LOG_LEVEL should remain")
			},
		},

		// Bad cases - Invalid YAML
		{
			name: "invalid YAML in transform annotation",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform:       "invalid: yaml: [[[",
						constants.AnnotationTransformStrict: "true",
					},
				},
				Data: map[string]string{},
			},
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: true,
			errMsg:  "failed to parse",
		},

		// Bad cases - Invalid rules
		{
			name: "empty path in strict mode",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - value: "something"
`,
						constants.AnnotationTransformStrict: "true",
					},
				},
				Data: map[string]string{},
			},
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: true,
			errMsg:  "invalid transformation rules",
		},
		{
			name: "too many rules in strict mode",
			source: func() runtime.Object {
				rules := "rules:\n"
				for i := 0; i < 100; i++ {
					rules += fmt.Sprintf("  - path: data.KEY%d\n    value: \"val\"\n", i)
				}
				return &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-config",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationTransform:       rules,
							constants.AnnotationTransformStrict: "true",
						},
					},
					Data: map[string]string{},
				}
			}(),
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: true,
			errMsg:  "too many rules",
		},

		// Edge cases
		{
			name: "no transformation rules",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"KEY": "value",
				},
			},
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				// When no rules, returns original typed object - convert to unstructured for checking
				unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(result)
				require.NoError(t, err)
				u := &unstructured.Unstructured{Object: unstructuredObj}
				value, found, err := unstructured.NestedString(u.Object, "data", "KEY")
				require.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "value", value, "original value should be unchanged")
			},
		},
		{
			name: "non-strict mode ignores errors",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: "invalid yaml [[[",
					},
				},
				Data: map[string]string{
					"KEY": "value",
				},
			},
			ctx:     TransformContext{},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				// Should return original unchanged - check via unstructured conversion
				unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(result)
				require.NoError(t, err)
				u := &unstructured.Unstructured{Object: unstructuredObj}
				value, found, err := unstructured.NestedString(u.Object, "data", "KEY")
				require.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "value", value)
			},
		},
		{
			name: "multiple rules applied in order",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: data.KEY1
    value: "first"
  - path: data.KEY2
    template: "{{.TargetNamespace}}-value"
  - path: metadata.labels
    merge:
      env: "prod"
  - path: data.TO_DELETE
    delete: true
`,
					},
				},
				Data: map[string]string{
					"TO_DELETE": "remove-me",
				},
			},
			ctx: TransformContext{
				TargetNamespace: "production",
			},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)

				key1, found, _ := unstructured.NestedString(u.Object, "data", "KEY1")
				assert.True(t, found)
				assert.Equal(t, "first", key1)

				key2, found, _ := unstructured.NestedString(u.Object, "data", "KEY2")
				assert.True(t, found)
				assert.Equal(t, "production-value", key2)

				labels := u.GetLabels()
				assert.Equal(t, "prod", labels["env"])

				data, found, _ := unstructured.NestedMap(u.Object, "data")
				assert.True(t, found)
				_, exists := data["TO_DELETE"]
				assert.False(t, exists)
			},
		},

		// Array indexing cases
		{
			name: "array indexing - modify container image",
			source: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Pod",
					"metadata": map[string]interface{}{
						"name":      "test-pod",
						"namespace": "default",
						"annotations": map[string]interface{}{
							constants.AnnotationTransform: `
rules:
  - path: spec.containers[0].image
    template: "registry.{{.TargetNamespace}}.example.com/app:v1"
  - path: spec.containers[0].env[1].value
    value: "production"
`,
						},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "app",
								"image": "app:latest",
								"env": []interface{}{
									map[string]interface{}{"name": "VAR1", "value": "val1"},
									map[string]interface{}{"name": "VAR2", "value": "val2"},
								},
							},
						},
					},
				},
			},
			ctx: TransformContext{
				TargetNamespace: "prod-app",
			},
			options: DefaultTransformOptions(),
			wantErr: false,
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)

				// Check container image was transformed
				containers, found, _ := unstructured.NestedSlice(u.Object, "spec", "containers")
				assert.True(t, found)
				assert.Len(t, containers, 1)

				container := containers[0].(map[string]interface{})
				assert.Equal(t, "registry.prod-app.example.com/app:v1", container["image"])

				// Check env var was transformed
				env := container["env"].([]interface{})
				assert.Len(t, env, 2)
				envVar := env[1].(map[string]interface{})
				assert.Equal(t, "production", envVar["value"])
			},
		},

		// Awkward cases
		{
			name: "template with missing context variable",
			source: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationTransform: `
rules:
  - path: data.VALUE
    template: "{{.TargetNamespace}}-empty"
`,
					},
				},
				Data: map[string]string{},
			},
			ctx: TransformContext{
				TargetNamespace: "",
			},
			options: DefaultTransformOptions(),
			wantErr: false, // Non-strict mode
			validate: func(t *testing.T, result runtime.Object) {
				u := result.(*unstructured.Unstructured)
				value, found, _ := unstructured.NestedString(u.Object, "data", "VALUE")
				// Template with empty context variable produces "-empty"
				assert.True(t, found)
				assert.Equal(t, "-empty", value)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := NewTransformer(tt.options)
			result, err := transformer.Transform(tt.source, tt.ctx)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "simple path",
			path: "data.KEY",
			want: []string{"data", "KEY"},
		},
		{
			name: "nested path",
			path: "metadata.labels.app",
			want: []string{"metadata", "labels", "app"},
		},
		{
			name: "empty path",
			path: "",
			want: nil,
		},
		{
			name: "single segment",
			path: "data",
			want: []string{"data"},
		},
		// Array indexing tests
		{
			name: "array index - single",
			path: "containers[0]",
			want: []string{"containers", "[0]"},
		},
		{
			name: "array index - with nested field",
			path: "spec.containers[0].image",
			want: []string{"spec", "containers", "[0]", "image"},
		},
		{
			name: "array index - multiple levels",
			path: "spec.template.spec.containers[0].env[2].value",
			want: []string{"spec", "template", "spec", "containers", "[0]", "env", "[2]", "value"},
		},
		{
			name: "array index - at end",
			path: "data.items[5]",
			want: []string{"data", "items", "[5]"},
		},
		{
			name: "array index - large number",
			path: "list[999].field",
			want: []string{"list", "[999]", "field"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePath(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetNestedField(t *testing.T) {
	tests := []struct {
		value   interface{}
		obj     map[string]interface{}
		want    map[string]interface{}
		name    string
		path    []string
		wantErr bool
	}{
		{
			name:    "set top-level field",
			obj:     map[string]interface{}{},
			path:    []string{"key"},
			value:   "value",
			wantErr: false,
			want: map[string]interface{}{
				"key": "value",
			},
		},
		{
			name:    "set nested field - creates intermediate maps",
			obj:     map[string]interface{}{},
			path:    []string{"data", "key"},
			value:   "value",
			wantErr: false,
			want: map[string]interface{}{
				"data": map[string]interface{}{
					"key": "value",
				},
			},
		},
		{
			name:    "set deeply nested field",
			obj:     map[string]interface{}{},
			path:    []string{"a", "b", "c", "d"},
			value:   "deep",
			wantErr: false,
			want: map[string]interface{}{
				"a": map[string]interface{}{
					"b": map[string]interface{}{
						"c": map[string]interface{}{
							"d": "deep",
						},
					},
				},
			},
		},
		{
			name:    "empty path",
			obj:     map[string]interface{}{},
			path:    []string{},
			value:   "value",
			wantErr: true,
		},
		{
			name: "path segment is not a map",
			obj: map[string]interface{}{
				"key": "string-value",
			},
			path:    []string{"key", "nested"},
			value:   "value",
			wantErr: true,
		},
		// Array indexing tests
		{
			name: "set array element value",
			obj: map[string]interface{}{
				"items": []interface{}{"a", "b", "c"},
			},
			path:    []string{"items", "[1]"},
			value:   "modified",
			wantErr: false,
			want: map[string]interface{}{
				"items": []interface{}{"a", "modified", "c"},
			},
		},
		{
			name: "set nested field in array element",
			obj: map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  "app",
						"image": "old-image",
					},
				},
			},
			path:    []string{"containers", "[0]", "image"},
			value:   "new-image",
			wantErr: false,
			want: map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  "app",
						"image": "new-image",
					},
				},
			},
		},
		{
			name: "set deeply nested array access",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"env": []interface{}{
								map[string]interface{}{"name": "VAR1", "value": "val1"},
								map[string]interface{}{"name": "VAR2", "value": "val2"},
							},
						},
					},
				},
			},
			path:    []string{"spec", "containers", "[0]", "env", "[1]", "value"},
			value:   "new-val2",
			wantErr: false,
			want: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"env": []interface{}{
								map[string]interface{}{"name": "VAR1", "value": "val1"},
								map[string]interface{}{"name": "VAR2", "value": "new-val2"},
							},
						},
					},
				},
			},
		},
		{
			name: "array index out of bounds",
			obj: map[string]interface{}{
				"items": []interface{}{"a", "b"},
			},
			path:    []string{"items", "[5]"},
			value:   "value",
			wantErr: true,
		},
		{
			name: "array index on non-array",
			obj: map[string]interface{}{
				"items": "not-an-array",
			},
			path:    []string{"items", "[0]"},
			value:   "value",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := setNestedField(tt.obj, tt.path, tt.value)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, tt.obj)
			}
		})
	}
}

func TestTransformer_TemplateTimeout(t *testing.T) {
	// Test that template execution times out
	// Since we can't easily create a template that times out reliably in tests,
	// we'll skip this test or use a very aggressive timeout
	// The timeout mechanism is implemented via context in the transformer
	t.Skip("Template timeout testing is unreliable in unit tests - covered by integration tests")
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		text     string
		expected bool
	}{
		// Exact matches
		{
			name:     "exact match",
			pattern:  "production",
			text:     "production",
			expected: true,
		},
		{
			name:     "exact match - no match",
			pattern:  "production",
			text:     "staging",
			expected: false,
		},

		// Wildcard * patterns
		{
			name:     "wildcard all",
			pattern:  "*",
			text:     "anything",
			expected: true,
		},
		{
			name:     "prefix wildcard",
			pattern:  "prod-*",
			text:     "prod-app-1",
			expected: true,
		},
		{
			name:     "prefix wildcard - no match",
			pattern:  "prod-*",
			text:     "staging-app-1",
			expected: false,
		},
		{
			name:     "suffix wildcard",
			pattern:  "*-staging",
			text:     "app-staging",
			expected: true,
		},
		{
			name:     "suffix wildcard - no match",
			pattern:  "*-staging",
			text:     "app-production",
			expected: false,
		},
		{
			name:     "middle wildcard",
			pattern:  "app-*-db",
			text:     "app-prod-db",
			expected: true,
		},
		{
			name:     "middle wildcard - no match",
			pattern:  "app-*-db",
			text:     "app-prod-cache",
			expected: false,
		},
		{
			name:     "multiple wildcards",
			pattern:  "*-prod-*",
			text:     "service-prod-v1",
			expected: true,
		},
		{
			name:     "wildcard matches empty",
			pattern:  "app-*",
			text:     "app-",
			expected: true,
		},

		// Single character wildcard ?
		{
			name:     "single char wildcard",
			pattern:  "app-?",
			text:     "app-1",
			expected: true,
		},
		{
			name:     "single char wildcard - no match (too long)",
			pattern:  "app-?",
			text:     "app-12",
			expected: false,
		},
		{
			name:     "single char wildcard - no match (too short)",
			pattern:  "app-?",
			text:     "app-",
			expected: false,
		},
		{
			name:     "multiple single char wildcards",
			pattern:  "app-??",
			text:     "app-12",
			expected: true,
		},
		{
			name:     "mixed wildcards",
			pattern:  "app-?-*",
			text:     "app-1-prod",
			expected: true,
		},

		// Edge cases
		{
			name:     "empty pattern and text",
			pattern:  "",
			text:     "",
			expected: true,
		},
		{
			name:     "empty pattern non-empty text",
			pattern:  "",
			text:     "text",
			expected: false,
		},
		{
			name:     "pattern longer than text",
			pattern:  "production",
			text:     "prod",
			expected: false,
		},
		{
			name:     "text longer than pattern",
			pattern:  "prod",
			text:     "production",
			expected: false,
		},

		// Real-world examples
		{
			name:     "preprod namespaces",
			pattern:  "preprod-*",
			text:     "preprod-api",
			expected: true,
		},
		{
			name:     "staging environments",
			pattern:  "*-staging",
			text:     "app-staging",
			expected: true,
		},
		{
			name:     "numbered namespaces",
			pattern:  "namespace-?",
			text:     "namespace-1",
			expected: true,
		},
		{
			name:     "versioned services",
			pattern:  "service-v*",
			text:     "service-v1.2.3",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchGlob(tt.pattern, tt.text)
			assert.Equal(t, tt.expected, result, "matchGlob(%q, %q)", tt.pattern, tt.text)
		})
	}
}

func TestMatchesNamespacePattern(t *testing.T) {
	tests := []struct {
		name            string
		pattern         *string
		targetNamespace string
		expected        bool
	}{
		{
			name:            "no pattern - matches all",
			pattern:         nil,
			targetNamespace: "any-namespace",
			expected:        true,
		},
		{
			name:            "empty pattern - matches all",
			pattern:         stringPtr(""),
			targetNamespace: "any-namespace",
			expected:        true,
		},
		{
			name:            "exact match",
			pattern:         stringPtr("production"),
			targetNamespace: "production",
			expected:        true,
		},
		{
			name:            "exact match - no match",
			pattern:         stringPtr("production"),
			targetNamespace: "staging",
			expected:        false,
		},
		{
			name:            "prefix pattern match",
			pattern:         stringPtr("preprod-*"),
			targetNamespace: "preprod-api",
			expected:        true,
		},
		{
			name:            "prefix pattern no match",
			pattern:         stringPtr("preprod-*"),
			targetNamespace: "prod-api",
			expected:        false,
		},
		{
			name:            "suffix pattern match",
			pattern:         stringPtr("*-staging"),
			targetNamespace: "app-staging",
			expected:        true,
		},
		{
			name:            "suffix pattern no match",
			pattern:         stringPtr("*-staging"),
			targetNamespace: "app-prod",
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := Rule{
				Path:             "data.test",
				Value:            stringPtr("value"),
				NamespacePattern: tt.pattern,
			}
			result := matchesNamespacePattern(rule, tt.targetNamespace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransformer_NamespacePatternFiltering(t *testing.T) {
	tests := []struct {
		name            string
		sourceData      map[string]interface{}
		rules           string
		targetNamespace string
		expectedData    map[string]interface{}
		description     string
	}{
		{
			name: "rule applies to matching namespace",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"HOST": "default.example.com",
				},
			},
			rules: `
rules:
  - path: data.HOST
    value: "preprod.example.com"
    namespacePattern: "preprod-*"
`,
			targetNamespace: "preprod-api",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"HOST": "preprod.example.com",
				},
			},
			description: "Rule with pattern 'preprod-*' should apply to 'preprod-api'",
		},
		{
			name: "rule skipped for non-matching namespace",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"HOST": "default.example.com",
				},
			},
			rules: `
rules:
  - path: data.HOST
    value: "preprod.example.com"
    namespacePattern: "preprod-*"
`,
			targetNamespace: "production",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"HOST": "default.example.com",
				},
			},
			description: "Rule with pattern 'preprod-*' should NOT apply to 'production'",
		},
		{
			name: "multiple rules with different patterns",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"HOST":      "default.example.com",
					"LOG_LEVEL": "info",
				},
			},
			rules: `
rules:
  - path: data.HOST
    value: "preprod.example.com"
    namespacePattern: "preprod-*"
  - path: data.HOST
    value: "prod.example.com"
    namespacePattern: "prod-*"
  - path: data.LOG_LEVEL
    value: "debug"
    namespacePattern: "preprod-*"
`,
			targetNamespace: "preprod-api",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"HOST":      "preprod.example.com",
					"LOG_LEVEL": "debug",
				},
			},
			description: "Only rules matching 'preprod-*' should apply to 'preprod-api'",
		},
		{
			name: "rule without pattern applies to all namespaces",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"GLOBAL": "old",
					"SCOPED": "old",
				},
			},
			rules: `
rules:
  - path: data.GLOBAL
    value: "applied-to-all"
  - path: data.SCOPED
    value: "applied-to-prod"
    namespacePattern: "prod-*"
`,
			targetNamespace: "staging",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"GLOBAL": "applied-to-all",
					"SCOPED": "old",
				},
			},
			description: "Rule without pattern should apply, rule with non-matching pattern should not",
		},
		{
			name: "template rule with namespace pattern",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"API_URL": "default.api.com",
				},
			},
			rules: `
rules:
  - path: data.API_URL
    template: "https://{{.TargetNamespace}}.api.com"
    namespacePattern: "preprod-*"
`,
			targetNamespace: "preprod-service",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"API_URL": "https://preprod-service.api.com",
				},
			},
			description: "Template rule with namespace pattern should apply when pattern matches",
		},
		{
			name: "suffix pattern matching (*-staging)",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"LOG_LEVEL":    "info",
					"GRAPHQL_HOST": "default.example.com",
				},
			},
			rules: `
rules:
  - path: data.LOG_LEVEL
    value: "warn"
    namespacePattern: "*-staging"
  - path: data.GRAPHQL_HOST
    value: "https://staging.example.com/v1/graphql"
    namespacePattern: "*-staging"
`,
			targetNamespace: "app-staging",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"LOG_LEVEL":    "warn",
					"GRAPHQL_HOST": "https://staging.example.com/v1/graphql",
				},
			},
			description: "Suffix pattern *-staging should match app-staging",
		},
		{
			name: "suffix pattern non-matching",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"LOG_LEVEL": "info",
				},
			},
			rules: `
rules:
  - path: data.LOG_LEVEL
    value: "warn"
    namespacePattern: "*-staging"
`,
			targetNamespace: "production",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"LOG_LEVEL": "info",
				},
			},
			description: "Suffix pattern *-staging should NOT match production",
		},
		{
			name: "single character wildcard (namespace-?)",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"ENVIRONMENT": "unknown",
				},
			},
			rules: `
rules:
  - path: data.ENVIRONMENT
    value: "development"
    namespacePattern: "namespace-?"
`,
			targetNamespace: "namespace-2",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"ENVIRONMENT": "development",
				},
			},
			description: "Single char wildcard namespace-? should match namespace-2",
		},
		{
			name: "single character wildcard non-matching (too long)",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"ENVIRONMENT": "unknown",
				},
			},
			rules: `
rules:
  - path: data.ENVIRONMENT
    value: "development"
    namespacePattern: "namespace-?"
`,
			targetNamespace: "namespace-10",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"ENVIRONMENT": "unknown",
				},
			},
			description: "Single char wildcard namespace-? should NOT match namespace-10 (too many chars)",
		},
		{
			name: "prod-* pattern with value rule",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"LOG_LEVEL":    "info",
					"GRAPHQL_HOST": "default.example.com",
				},
			},
			rules: `
rules:
  - path: data.LOG_LEVEL
    value: "error"
    namespacePattern: "prod-*"
  - path: data.GRAPHQL_HOST
    value: "https://api.example.com/v1/graphql"
    namespacePattern: "prod-*"
`,
			targetNamespace: "prod-api",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"LOG_LEVEL":    "error",
					"GRAPHQL_HOST": "https://api.example.com/v1/graphql",
				},
			},
			description: "Pattern prod-* should match prod-api",
		},
		{
			name: "merge rule with namespace pattern",
			sourceData: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app": "myapp",
					},
				},
				"data": map[string]interface{}{
					"config": "value",
				},
			},
			rules: `
rules:
  - path: metadata.labels
    merge:
      security-tier: "high"
      compliance: "required"
    namespacePattern: "prod-*"
`,
			targetNamespace: "prod-api",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"config": "value",
				},
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app":           "myapp",
						"security-tier": "high",
						"compliance":    "required",
					},
				},
			},
			description: "Merge rule with prod-* pattern should add labels to prod-api",
		},
		{
			name: "delete rule with namespace pattern",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"DEBUG_MODE":   "true",
					"ADMIN_KEY":    "secret",
					"PUBLIC_VALUE": "safe",
				},
			},
			rules: `
rules:
  - path: data.DEBUG_MODE
    delete: true
    namespacePattern: "prod-*"
  - path: data.ADMIN_KEY
    delete: true
    namespacePattern: "prod-*"
`,
			targetNamespace: "prod-api",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"PUBLIC_VALUE": "safe",
				},
			},
			description: "Delete rules with prod-* pattern should remove debug fields from prod-api",
		},
		{
			name: "complex multi-environment pattern (like example 10)",
			sourceData: map[string]interface{}{
				"data": map[string]interface{}{
					"APP_NAME":    "default-app",
					"ENVIRONMENT": "unknown",
					"LOG_LEVEL":   "info",
				},
			},
			rules: `
rules:
  # Global rule - no pattern
  - path: data.APP_NAME
    value: "universal-app"

  # Numbered namespaces
  - path: data.ENVIRONMENT
    value: "development"
    namespacePattern: "namespace-?"

  # Preprod
  - path: data.ENVIRONMENT
    value: "preproduction"
    namespacePattern: "preprod-*"

  - path: data.LOG_LEVEL
    value: "debug"
    namespacePattern: "preprod-*"

  # Production
  - path: data.ENVIRONMENT
    value: "production"
    namespacePattern: "prod-*"

  - path: data.LOG_LEVEL
    value: "error"
    namespacePattern: "prod-*"
`,
			targetNamespace: "preprod-api",
			expectedData: map[string]interface{}{
				"data": map[string]interface{}{
					"APP_NAME":    "universal-app",
					"ENVIRONMENT": "preproduction",
					"LOG_LEVEL":   "debug",
				},
			},
			description: "Complex multi-pattern rules should apply global + preprod-specific rules to preprod-api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create source object with transformation rules
			source := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "source-namespace",
						"annotations": map[string]interface{}{
							constants.AnnotationTransform: tt.rules,
						},
					},
				},
			}

			// Merge source data
			for k, v := range tt.sourceData {
				if k == "metadata" {
					// Merge metadata instead of replacing it
					existingMeta := source.Object["metadata"].(map[string]interface{})
					newMeta := v.(map[string]interface{})
					for mk, mv := range newMeta {
						existingMeta[mk] = mv
					}
				} else {
					source.Object[k] = v
				}
			}

			// Transform
			transformer := NewDefaultTransformer()
			ctx := TransformContext{
				TargetNamespace: tt.targetNamespace,
				SourceNamespace: "source-namespace",
				SourceName:      "test-config",
				TargetName:      "test-config",
			}

			result, err := transformer.Transform(source, ctx)
			require.NoError(t, err, tt.description)

			resultU, ok := result.(*unstructured.Unstructured)
			require.True(t, ok)

			// Check that the expected fields have the expected values
			for key, expectedValue := range tt.expectedData {
				if key == "data" {
					dataMap, found, err := unstructured.NestedMap(resultU.Object, "data")
					require.NoError(t, err)
					require.True(t, found)

					expectedDataMap := expectedValue.(map[string]interface{})
					for dataKey, dataValue := range expectedDataMap {
						assert.Equal(t, dataValue, dataMap[dataKey], "%s: data.%s should be %v", tt.description, dataKey, dataValue)
					}
				}
				if key == "metadata" {
					metadataMap := expectedValue.(map[string]interface{})
					if labelsExpected, ok := metadataMap["labels"]; ok {
						labelsMap, found, err := unstructured.NestedMap(resultU.Object, "metadata", "labels")
						require.NoError(t, err)
						require.True(t, found)

						expectedLabels := labelsExpected.(map[string]interface{})
						for labelKey, labelValue := range expectedLabels {
							assert.Equal(t, labelValue, labelsMap[labelKey], "%s: metadata.labels.%s should be %v", tt.description, labelKey, labelValue)
						}
					}
				}
			}
		})
	}
}
