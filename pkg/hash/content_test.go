package hash

import (
	"testing"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestComputeContentHash_Secret(t *testing.T) {
	tests := []struct {
		secret1   *corev1.Secret
		secret2   *corev1.Secret
		name      string
		wantSame  bool
		wantError bool
	}{
		{
			name: "identical secrets produce same hash",
			secret1: &corev1.Secret{
				Data: map[string][]byte{
					"password": []byte("secret123"),
				},
				Type: corev1.SecretTypeOpaque,
			},
			secret2: &corev1.Secret{
				Data: map[string][]byte{
					"password": []byte("secret123"),
				},
				Type: corev1.SecretTypeOpaque,
			},
			wantSame:  true,
			wantError: false,
		},
		{
			name: "different data produces different hash",
			secret1: &corev1.Secret{
				Data: map[string][]byte{
					"password": []byte("secret123"),
				},
			},
			secret2: &corev1.Secret{
				Data: map[string][]byte{
					"password": []byte("different"),
				},
			},
			wantSame:  false,
			wantError: false,
		},
		{
			name: "different type produces different hash",
			secret1: &corev1.Secret{
				Data: map[string][]byte{"key": []byte("value")},
				Type: corev1.SecretTypeOpaque,
			},
			secret2: &corev1.Secret{
				Data: map[string][]byte{"key": []byte("value")},
				Type: corev1.SecretTypeTLS,
			},
			wantSame:  false,
			wantError: false,
		},
		{
			name: "metadata changes don't affect hash",
			secret1: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "secret1",
					Namespace:       "default",
					ResourceVersion: "100",
					Generation:      1,
				},
				Data: map[string][]byte{"key": []byte("value")},
			},
			secret2: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "secret2",
					Namespace:       "different",
					ResourceVersion: "200",
					Generation:      2,
				},
				Data: map[string][]byte{"key": []byte("value")},
			},
			wantSame:  true,
			wantError: false,
		},
		{
			name: "stringData included in hash",
			secret1: &corev1.Secret{
				StringData: map[string]string{"key": "value"},
			},
			secret2: &corev1.Secret{
				StringData: map[string]string{"key": "different"},
			},
			wantSame:  false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1, err1 := ComputeContentHash(tt.secret1)
			hash2, err2 := ComputeContentHash(tt.secret2)

			if tt.wantError {
				require.Error(t, err1)
				require.Error(t, err2)
				return
			}

			require.NoError(t, err1)
			require.NoError(t, err2)
			assert.NotEmpty(t, hash1)
			assert.NotEmpty(t, hash2)

			if tt.wantSame {
				assert.Equal(t, hash1, hash2, "hashes should be identical")
			} else {
				assert.NotEqual(t, hash1, hash2, "hashes should be different")
			}
		})
	}
}

func TestComputeContentHash_ConfigMap(t *testing.T) {
	tests := []struct {
		cm1       *corev1.ConfigMap
		cm2       *corev1.ConfigMap
		name      string
		wantSame  bool
		wantError bool
	}{
		{
			name: "identical configmaps produce same hash",
			cm1: &corev1.ConfigMap{
				Data: map[string]string{
					"config.yaml": "setting: value",
				},
			},
			cm2: &corev1.ConfigMap{
				Data: map[string]string{
					"config.yaml": "setting: value",
				},
			},
			wantSame:  true,
			wantError: false,
		},
		{
			name: "different data produces different hash",
			cm1: &corev1.ConfigMap{
				Data: map[string]string{
					"key": "value1",
				},
			},
			cm2: &corev1.ConfigMap{
				Data: map[string]string{
					"key": "value2",
				},
			},
			wantSame:  false,
			wantError: false,
		},
		{
			name: "binaryData included in hash",
			cm1: &corev1.ConfigMap{
				BinaryData: map[string][]byte{
					"file": {0x00, 0x01, 0x02},
				},
			},
			cm2: &corev1.ConfigMap{
				BinaryData: map[string][]byte{
					"file": {0x00, 0x01, 0xFF},
				},
			},
			wantSame:  false,
			wantError: false,
		},
		{
			name: "metadata changes don't affect hash",
			cm1: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "100",
					Generation:      1,
				},
				Data: map[string]string{"key": "value"},
			},
			cm2: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "200",
					Generation:      5,
				},
				Data: map[string]string{"key": "value"},
			},
			wantSame:  true,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1, err1 := ComputeContentHash(tt.cm1)
			hash2, err2 := ComputeContentHash(tt.cm2)

			if tt.wantError {
				require.Error(t, err1)
				require.Error(t, err2)
				return
			}

			require.NoError(t, err1)
			require.NoError(t, err2)
			assert.NotEmpty(t, hash1)
			assert.NotEmpty(t, hash2)

			if tt.wantSame {
				assert.Equal(t, hash1, hash2)
			} else {
				assert.NotEqual(t, hash1, hash2)
			}
		})
	}
}

func TestComputeContentHash_Unstructured(t *testing.T) {
	tests := []struct {
		obj1      *unstructured.Unstructured
		obj2      *unstructured.Unstructured
		name      string
		wantSame  bool
		wantError bool
	}{
		{
			name: "identical specs produce same hash",
			obj1: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Custom",
					"spec": map[string]interface{}{
						"field": "value",
					},
				},
			},
			obj2: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Custom",
					"spec": map[string]interface{}{
						"field": "value",
					},
				},
			},
			wantSame:  true,
			wantError: false,
		},
		{
			name: "different specs produce different hash",
			obj1: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"field": "value1",
					},
				},
			},
			obj2: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"field": "value2",
					},
				},
			},
			wantSame:  false,
			wantError: false,
		},
		{
			name: "metadata excluded from hash",
			obj1: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"resourceVersion": "100",
					},
					"spec": map[string]interface{}{
						"field": "value",
					},
				},
			},
			obj2: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"resourceVersion": "200",
					},
					"spec": map[string]interface{}{
						"field": "value",
					},
				},
			},
			wantSame:  true,
			wantError: false,
		},
		{
			name: "status excluded from hash",
			obj1: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"field": "value",
					},
					"status": map[string]interface{}{
						"condition": "Ready",
					},
				},
			},
			obj2: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"field": "value",
					},
					"status": map[string]interface{}{
						"condition": "NotReady",
					},
				},
			},
			wantSame:  true,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1, err1 := ComputeContentHash(tt.obj1)
			hash2, err2 := ComputeContentHash(tt.obj2)

			if tt.wantError {
				require.Error(t, err1)
				require.Error(t, err2)
				return
			}

			require.NoError(t, err1)
			require.NoError(t, err2)
			assert.NotEmpty(t, hash1)
			assert.NotEmpty(t, hash2)

			if tt.wantSame {
				assert.Equal(t, hash1, hash2)
			} else {
				assert.NotEqual(t, hash1, hash2)
			}
		})
	}
}

func TestNeedsSync(t *testing.T) {
	tests := []struct {
		source            runtime.Object
		target            runtime.Object
		targetAnnotations map[string]string
		name              string
		want              bool
		wantError         bool
	}{
		{
			name: "needs sync when generation changed",
			source: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"generation": int64(5),
					},
					"spec": map[string]interface{}{
						"field": "value",
					},
				},
			},
			target: &unstructured.Unstructured{},
			targetAnnotations: map[string]string{
				constants.AnnotationSourceGeneration:  "3",
				constants.AnnotationSourceContentHash: "abc123",
			},
			want:      true,
			wantError: false,
		},
		{
			name: "doesn't need sync when generation same and hash same",
			source: &corev1.Secret{
				Data: map[string][]byte{"key": []byte("value")},
			},
			target: &corev1.Secret{},
			targetAnnotations: map[string]string{
				constants.AnnotationSourceGeneration:  "0",
				constants.AnnotationSourceContentHash: mustComputeHash(t, &corev1.Secret{Data: map[string][]byte{"key": []byte("value")}}),
			},
			want:      false,
			wantError: false,
		},
		{
			name: "needs sync when content hash changed",
			source: &corev1.ConfigMap{
				Data: map[string]string{"key": "newvalue"},
			},
			target: &corev1.ConfigMap{},
			targetAnnotations: map[string]string{
				constants.AnnotationSourceContentHash: "oldhash",
			},
			want:      true,
			wantError: false,
		},
		{
			name: "needs sync when no previous hash",
			source: &corev1.Secret{
				Data: map[string][]byte{"key": []byte("value")},
			},
			target:            &corev1.Secret{},
			targetAnnotations: map[string]string{},
			want:              true,
			wantError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NeedsSync(tt.source, tt.target, tt.targetAnnotations)

			if tt.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetGeneration(t *testing.T) {
	tests := []struct {
		obj  runtime.Object
		name string
		want int64
	}{
		{
			name: "returns generation for resource with generation",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"generation": int64(42),
					},
				},
			},
			want: 42,
		},
		{
			name: "returns 0 for resource without generation",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			want: 0,
		},
		{
			name: "returns 0 for nil metadata",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getGeneration(tt.obj)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Helper function to compute hash for test setup
func mustComputeHash(t *testing.T, obj runtime.Object) string {
	t.Helper()
	hash, err := ComputeContentHash(obj)
	require.NoError(t, err)
	return hash
}

// TestComputeContentHash_NoMutation verifies that hash computation doesn't mutate the input object.
// This is critical because NestedMap can modify the underlying map.
func TestComputeContentHash_NoMutation(t *testing.T) {
	t.Run("unstructured object is not mutated", func(t *testing.T) {
		// Create an unstructured object with nested spec
		original := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Custom",
				"metadata": map[string]interface{}{
					"name":      "test-resource",
					"namespace": "default",
					"annotations": map[string]interface{}{
						constants.AnnotationTransform: `{"rules":[{"field":"spec.value","action":"base64encode"}]}`,
					},
				},
				"spec": map[string]interface{}{
					"field1": "value1",
					"nested": map[string]interface{}{
						"deep": "data",
					},
				},
				"status": map[string]interface{}{
					"condition": "Ready",
				},
			},
		}

		// Deep copy the original to compare after hash computation
		expectedCopy := original.DeepCopy()

		// Compute hash multiple times
		hash1, err := ComputeContentHash(original)
		require.NoError(t, err)

		hash2, err := ComputeContentHash(original)
		require.NoError(t, err)

		// Hashes should be consistent (object wasn't modified)
		assert.Equal(t, hash1, hash2, "hash should be consistent across calls")

		// Original object should be unchanged
		assert.Equal(t, expectedCopy.Object, original.Object, "original object should not be mutated")
	})

	t.Run("secret is not mutated", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "default",
				Annotations: map[string]string{
					constants.AnnotationTransform: `{"rules":[]}`,
				},
			},
			Data: map[string][]byte{
				"password": []byte("secret123"),
			},
			Type: corev1.SecretTypeOpaque,
		}

		// Copy for comparison
		originalData := make(map[string][]byte)
		for k, v := range secret.Data {
			originalData[k] = append([]byte(nil), v...)
		}
		originalAnnotations := make(map[string]string)
		for k, v := range secret.Annotations {
			originalAnnotations[k] = v
		}

		// Compute hash
		_, err := ComputeContentHash(secret)
		require.NoError(t, err)

		// Verify no mutation
		assert.Equal(t, originalData, secret.Data, "secret data should not be mutated")
		assert.Equal(t, originalAnnotations, secret.Annotations, "secret annotations should not be mutated")
	})

	t.Run("configmap is not mutated", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cm",
				Namespace: "default",
			},
			Data: map[string]string{
				"config.yaml": "key: value",
			},
			BinaryData: map[string][]byte{
				"binary": {0x00, 0x01, 0x02},
			},
		}

		// Copy for comparison
		originalData := make(map[string]string)
		for k, v := range cm.Data {
			originalData[k] = v
		}
		originalBinaryData := make(map[string][]byte)
		for k, v := range cm.BinaryData {
			originalBinaryData[k] = append([]byte(nil), v...)
		}

		// Compute hash
		_, err := ComputeContentHash(cm)
		require.NoError(t, err)

		// Verify no mutation
		assert.Equal(t, originalData, cm.Data, "configmap data should not be mutated")
		assert.Equal(t, originalBinaryData, cm.BinaryData, "configmap binary data should not be mutated")
	})
}

// Benchmark tests
func BenchmarkComputeContentHash_Secret(b *testing.B) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"password": []byte("secret123"),
			"username": []byte("admin"),
		},
		Type: corev1.SecretTypeOpaque,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ComputeContentHash(secret)
	}
}

func BenchmarkComputeContentHash_ConfigMap(b *testing.B) {
	cm := &corev1.ConfigMap{
		Data: map[string]string{
			"config.yaml": "setting: value\nother: data",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ComputeContentHash(cm)
	}
}

func BenchmarkNeedsSync(b *testing.B) {
	source := &corev1.Secret{
		Data: map[string][]byte{"key": []byte("value")},
	}
	target := &corev1.Secret{}
	hash, _ := ComputeContentHash(source)
	annotations := map[string]string{
		"source-content-hash": hash,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = NeedsSync(source, target, annotations)
	}
}
