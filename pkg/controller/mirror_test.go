package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
)

func TestCreateMirror_Secret(t *testing.T) {
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-secret",
			Namespace:       "default",
			UID:             "source-uid-123",
			ResourceVersion: "100",
			Generation:      5,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte("secret123"),
		},
	}

	mirror, err := CreateMirror(source, "app1")
	require.NoError(t, err)
	require.NotNil(t, mirror)

	secretMirror, ok := mirror.(*corev1.Secret)
	require.True(t, ok, "mirror should be a Secret")

	// Verify mirror properties
	assert.Equal(t, "test-secret", secretMirror.Name)
	assert.Equal(t, "app1", secretMirror.Namespace)
	assert.Equal(t, corev1.SecretTypeOpaque, secretMirror.Type)
	assert.Equal(t, source.Data, secretMirror.Data)

	// Verify ownership labels
	assert.Equal(t, constants.ControllerName, secretMirror.Labels[constants.LabelManagedBy])
	assert.Equal(t, "true", secretMirror.Labels[constants.LabelMirror])

	// Verify ownership annotations
	assert.Equal(t, "default", secretMirror.Annotations[constants.AnnotationSourceNamespace])
	assert.Equal(t, "test-secret", secretMirror.Annotations[constants.AnnotationSourceName])
	assert.Equal(t, "source-uid-123", secretMirror.Annotations[constants.AnnotationSourceUID])
	assert.Equal(t, "5", secretMirror.Annotations[constants.AnnotationSourceGeneration])
	assert.NotEmpty(t, secretMirror.Annotations[constants.AnnotationSourceContentHash])
	assert.NotEmpty(t, secretMirror.Annotations[constants.AnnotationLastSyncTime])
}

func TestCreateMirror_ConfigMap(t *testing.T) {
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-config",
			Namespace:       "default",
			UID:             "config-uid-456",
			ResourceVersion: "200",
		},
		Data: map[string]string{
			"config.yaml": "setting: value",
		},
		BinaryData: map[string][]byte{
			"binary": {0x00, 0x01, 0x02},
		},
	}

	mirror, err := CreateMirror(source, "prod-ns")
	require.NoError(t, err)
	require.NotNil(t, mirror)

	cmMirror, ok := mirror.(*corev1.ConfigMap)
	require.True(t, ok, "mirror should be a ConfigMap")

	// Verify mirror properties
	assert.Equal(t, "test-config", cmMirror.Name)
	assert.Equal(t, "prod-ns", cmMirror.Namespace)
	assert.Equal(t, source.Data, cmMirror.Data)
	assert.Equal(t, source.BinaryData, cmMirror.BinaryData)

	// Verify ownership labels
	assert.Equal(t, constants.ControllerName, cmMirror.Labels[constants.LabelManagedBy])
	assert.Equal(t, "true", cmMirror.Labels[constants.LabelMirror])

	// Verify ownership annotations
	assert.Equal(t, "default", cmMirror.Annotations[constants.AnnotationSourceNamespace])
	assert.Equal(t, "test-config", cmMirror.Annotations[constants.AnnotationSourceName])
	assert.Equal(t, "config-uid-456", cmMirror.Annotations[constants.AnnotationSourceUID])
	assert.NotEmpty(t, cmMirror.Annotations[constants.AnnotationSourceContentHash])
}

func TestCreateMirror_Unstructured(t *testing.T) {
	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "traefik.io/v1alpha1",
			"kind":       "Middleware",
			"metadata": map[string]interface{}{
				"name":            "test-middleware",
				"namespace":       "traefik",
				"uid":             "middleware-uid-789",
				"resourceVersion": "300",
				"generation":      int64(3),
			},
			"spec": map[string]interface{}{
				"basicAuth": map[string]interface{}{
					"secret": "auth-secret",
				},
			},
			"status": map[string]interface{}{
				"condition": "Ready",
			},
		},
	}

	mirror, err := CreateMirror(source, "app-ns")
	require.NoError(t, err)
	require.NotNil(t, mirror)

	uMirror, ok := mirror.(*unstructured.Unstructured)
	require.True(t, ok, "mirror should be Unstructured")

	// Verify mirror properties
	assert.Equal(t, "test-middleware", uMirror.GetName())
	assert.Equal(t, "app-ns", uMirror.GetNamespace())

	// Verify spec is copied
	spec, found, err := unstructured.NestedMap(uMirror.Object, "spec")
	require.NoError(t, err)
	require.True(t, found)
	assert.NotNil(t, spec)

	// Verify status is NOT copied
	_, found, err = unstructured.NestedMap(uMirror.Object, "status")
	require.NoError(t, err)
	assert.False(t, found, "status should not be mirrored")

	// Verify metadata is cleared
	assert.Empty(t, uMirror.GetResourceVersion())
	assert.Empty(t, uMirror.GetUID())
	assert.Equal(t, int64(0), uMirror.GetGeneration())

	// Verify ownership labels
	assert.Equal(t, constants.ControllerName, uMirror.GetLabels()[constants.LabelManagedBy])
	assert.Equal(t, "true", uMirror.GetLabels()[constants.LabelMirror])

	// Verify ownership annotations
	annotations := uMirror.GetAnnotations()
	assert.Equal(t, "traefik", annotations[constants.AnnotationSourceNamespace])
	assert.Equal(t, "test-middleware", annotations[constants.AnnotationSourceName])
	assert.Equal(t, "middleware-uid-789", annotations[constants.AnnotationSourceUID])
	assert.Equal(t, "3", annotations[constants.AnnotationSourceGeneration])
}

func TestCreateMirror_Unstructured_StripsOwnerReferences(t *testing.T) {
	// Create source with ownerReferences (e.g., managed by ExternalSecrets)
	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":            "external-secret",
				"namespace":       "default",
				"uid":             "secret-uid-123",
				"resourceVersion": "100",
				"generation":      int64(1),
				// Source has ownerReferences (e.g., set by ExternalSecrets operator)
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "external-secrets.io/v1",
						"kind":       "ExternalSecret",
						"name":       "1p-docker-config",
						"uid":        "externalsecret-uid-456",
						"controller": true,
					},
				},
				// Source has finalizers
				"finalizers": []interface{}{
					"externalsecrets.external-secrets.io/externalsecret-cleanup",
				},
			},
			"data": map[string]interface{}{
				"password": "c2VjcmV0",
			},
		},
	}

	mirror, err := CreateMirror(source, "target-ns")
	require.NoError(t, err)
	require.NotNil(t, mirror)

	uMirror, ok := mirror.(*unstructured.Unstructured)
	require.True(t, ok, "mirror should be Unstructured")

	// CRITICAL: Verify ownerReferences are NOT copied to mirror
	ownerRefs := uMirror.GetOwnerReferences()
	assert.Nil(t, ownerRefs, "mirror should not have ownerReferences from source")

	// CRITICAL: Verify finalizers are NOT copied to mirror
	finalizers := uMirror.GetFinalizers()
	assert.Nil(t, finalizers, "mirror should not have finalizers from source")

	// Verify mirror is properly managed by KubeMirror via labels/annotations
	assert.Equal(t, constants.ControllerName, uMirror.GetLabels()[constants.LabelManagedBy])
	assert.Equal(t, "true", uMirror.GetLabels()[constants.LabelMirror])
	assert.Equal(t, "default", uMirror.GetAnnotations()[constants.AnnotationSourceNamespace])
	assert.Equal(t, "external-secret", uMirror.GetAnnotations()[constants.AnnotationSourceName])
	assert.Equal(t, "secret-uid-123", uMirror.GetAnnotations()[constants.AnnotationSourceUID])
}

func TestUpdateMirror_Unstructured_ClearsOwnerReferences(t *testing.T) {
	// Create mirror that somehow has ownerReferences (e.g., from before the fix)
	mirror := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "traefik.io/v1alpha1",
			"kind":       "Middleware",
			"metadata": map[string]interface{}{
				"name":      "test-middleware",
				"namespace": "target-ns",
				"labels": map[string]interface{}{
					constants.LabelManagedBy: constants.ControllerName,
					constants.LabelMirror:    "true",
				},
				"annotations": map[string]interface{}{
					constants.AnnotationSourceNamespace:   "default",
					constants.AnnotationSourceName:        "test-middleware",
					constants.AnnotationSourceContentHash: "oldhash",
				},
				// Mirror has ownerReferences (from before fix or external modification)
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "external-secrets.io/v1",
						"kind":       "ExternalSecret",
						"name":       "1p-docker-config",
						"uid":        "externalsecret-uid-456",
					},
				},
				// Mirror has finalizers (from before fix or external modification)
				"finalizers": []interface{}{
					"some-finalizer",
				},
			},
			"spec": map[string]interface{}{
				"basicAuth": map[string]interface{}{
					"secret": "old-secret",
				},
			},
		},
	}

	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "traefik.io/v1alpha1",
			"kind":       "Middleware",
			"metadata": map[string]interface{}{
				"name":       "test-middleware",
				"namespace":  "default",
				"generation": int64(2),
			},
			"spec": map[string]interface{}{
				"basicAuth": map[string]interface{}{
					"secret": "new-secret",
				},
			},
		},
	}

	err := UpdateMirror(mirror, source)
	require.NoError(t, err)

	// CRITICAL: Verify ownerReferences are cleared from mirror
	ownerRefs := mirror.GetOwnerReferences()
	assert.Nil(t, ownerRefs, "mirror should not have ownerReferences after update")

	// CRITICAL: Verify finalizers are cleared from mirror
	finalizers := mirror.GetFinalizers()
	assert.Nil(t, finalizers, "mirror should not have finalizers after update")

	// Verify spec was updated
	secret, found, err := unstructured.NestedString(mirror.Object, "spec", "basicAuth", "secret")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "new-secret", secret)

	// Verify hash was updated
	assert.NotEqual(t, "oldhash", mirror.GetAnnotations()[constants.AnnotationSourceContentHash])
}

func TestUpdateMirror_Secret(t *testing.T) {
	mirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "app1",
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ControllerName,
			},
			Annotations: map[string]string{
				constants.AnnotationSourceContentHash: "oldhash",
			},
		},
		Data: map[string][]byte{
			"password": []byte("old"),
		},
		Type: corev1.SecretTypeOpaque,
	}

	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-secret",
			Namespace:  "default",
			Generation: 10,
		},
		Data: map[string][]byte{
			"password": []byte("new"),
		},
		Type: corev1.SecretTypeTLS,
	}

	err := UpdateMirror(mirror, source)
	require.NoError(t, err)

	// Verify data updated
	assert.Equal(t, source.Data, mirror.Data)
	assert.Equal(t, source.Type, mirror.Type)

	// Verify hash updated
	assert.NotEqual(t, "oldhash", mirror.Annotations[constants.AnnotationSourceContentHash])
	assert.Equal(t, "10", mirror.Annotations[constants.AnnotationSourceGeneration])
	assert.NotEmpty(t, mirror.Annotations[constants.AnnotationLastSyncTime])
}

func TestUpdateMirror_ConfigMap(t *testing.T) {
	mirror := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "app1",
			Annotations: map[string]string{
				constants.AnnotationSourceContentHash: "oldhash",
			},
		},
		Data: map[string]string{
			"key": "old",
		},
	}

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"key": "new",
		},
		BinaryData: map[string][]byte{
			"binary": {0xFF},
		},
	}

	err := UpdateMirror(mirror, source)
	require.NoError(t, err)

	// Verify data updated
	assert.Equal(t, source.Data, mirror.Data)
	assert.Equal(t, source.BinaryData, mirror.BinaryData)
	assert.NotEqual(t, "oldhash", mirror.Annotations[constants.AnnotationSourceContentHash])
}

func TestUpdateMirror_UnstructuredSecret(t *testing.T) {
	// This test validates the fix for the bug where Unstructured Secrets
	// would update annotations but not data fields during UpdateMirror
	mirror := &unstructured.Unstructured{
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
				"annotations": map[string]interface{}{
					constants.AnnotationSourceContentHash: "oldhash",
				},
			},
			"type": "Opaque",
			"data": map[string]interface{}{
				"password": "b2xkLXZhbHVl", // base64 encoded "old-value"
			},
		},
	}

	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":       "test-secret",
				"namespace":  "default",
				"generation": int64(10),
			},
			"type": "kubernetes.io/tls",
			"data": map[string]interface{}{
				"password": "bmV3LXZhbHVl", // base64 encoded "new-value"
				"username": "YWRtaW4=",     // base64 encoded "admin"
			},
		},
	}

	err := UpdateMirror(mirror, source)
	require.NoError(t, err)

	// Verify data was updated (this was the bug - data wasn't being updated)
	mirrorData, found, err := unstructured.NestedMap(mirror.Object, "data")
	require.NoError(t, err)
	require.True(t, found, "mirror should have data field")
	sourceData, _, _ := unstructured.NestedMap(source.Object, "data")
	assert.Equal(t, sourceData, mirrorData, "mirror data should match source data")

	// Verify type was updated
	mirrorType, found, err := unstructured.NestedString(mirror.Object, "type")
	require.NoError(t, err)
	require.True(t, found, "mirror should have type field")
	assert.Equal(t, "kubernetes.io/tls", mirrorType, "mirror type should be updated")

	// Verify annotations were updated
	annotations := mirror.GetAnnotations()
	assert.NotEqual(t, "oldhash", annotations[constants.AnnotationSourceContentHash], "hash should be updated")
	assert.Equal(t, "10", annotations[constants.AnnotationSourceGeneration], "generation should be updated")
	assert.NotEmpty(t, annotations[constants.AnnotationLastSyncTime], "sync time should be set")
}

func TestUpdateMirror_UnstructuredConfigMap(t *testing.T) {
	// Test Unstructured ConfigMap to ensure data and binaryData are updated
	mirror := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-config",
				"namespace": "app1",
				"annotations": map[string]interface{}{
					constants.AnnotationSourceContentHash: "oldhash",
				},
			},
			"data": map[string]interface{}{
				"key": "old-value",
			},
		},
	}

	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-config",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"key":  "new-value",
				"key2": "another-value",
			},
			"binaryData": map[string]interface{}{
				"binary": "AAECAwQ=", // base64 binary data
			},
		},
	}

	err := UpdateMirror(mirror, source)
	require.NoError(t, err)

	// Verify data was updated
	mirrorData, found, err := unstructured.NestedMap(mirror.Object, "data")
	require.NoError(t, err)
	require.True(t, found, "mirror should have data field")
	sourceData, _, _ := unstructured.NestedMap(source.Object, "data")
	assert.Equal(t, sourceData, mirrorData, "mirror data should match source data")

	// Verify binaryData was updated
	mirrorBinaryData, found, err := unstructured.NestedMap(mirror.Object, "binaryData")
	require.NoError(t, err)
	require.True(t, found, "mirror should have binaryData field")
	sourceBinaryData, _, _ := unstructured.NestedMap(source.Object, "binaryData")
	assert.Equal(t, sourceBinaryData, mirrorBinaryData, "mirror binaryData should match source binaryData")

	// Verify annotations were updated
	annotations := mirror.GetAnnotations()
	assert.NotEqual(t, "oldhash", annotations[constants.AnnotationSourceContentHash], "hash should be updated")
}

func TestIsManagedByUs(t *testing.T) {
	tests := []struct {
		obj  metav1.Object
		name string
		want bool
	}{
		{
			name: "managed by us",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelManagedBy: constants.ControllerName,
					},
				},
			},
			want: true,
		},
		{
			name: "not managed by us",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelManagedBy: "other-controller",
					},
				},
			},
			want: false,
		},
		{
			name: "no labels",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{},
			},
			want: false,
		},
		{
			name: "nil labels",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: nil,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsManagedByUs(tt.obj)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsMirrorResource(t *testing.T) {
	tests := []struct {
		obj  metav1.Object
		name string
		want bool
	}{
		{
			name: "is mirror",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelMirror: "true",
					},
				},
			},
			want: true,
		},
		{
			name: "not mirror",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelMirror: "false",
					},
				},
			},
			want: false,
		},
		{
			name: "no labels",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMirrorResource(tt.obj)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetSourceReference(t *testing.T) {
	tests := []struct {
		name          string
		obj           metav1.Object
		wantNamespace string
		wantName      string
		wantUID       string
		wantFound     bool
	}{
		{
			name: "valid source reference",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationSourceNamespace: "default",
						constants.AnnotationSourceName:      "my-secret",
						constants.AnnotationSourceUID:       "uid-123",
					},
				},
			},
			wantNamespace: "default",
			wantName:      "my-secret",
			wantUID:       "uid-123",
			wantFound:     true,
		},
		{
			name: "missing annotations",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{},
			},
			wantFound: false,
		},
		{
			name: "incomplete annotations - missing name",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationSourceNamespace: "default",
					},
				},
			},
			wantFound: false,
		},
		{
			name: "incomplete annotations - missing namespace",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationSourceName: "my-secret",
					},
				},
			},
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNS, gotName, gotUID, gotFound := GetSourceReference(tt.obj)
			assert.Equal(t, tt.wantFound, gotFound)
			if tt.wantFound {
				assert.Equal(t, tt.wantNamespace, gotNS)
				assert.Equal(t, tt.wantName, gotName)
				assert.Equal(t, tt.wantUID, gotUID)
			}
		})
	}
}

// Test that mirrors don't include sync annotations (prevent infinite loop)
func TestCreateMirror_NoSyncAnnotations(t *testing.T) {
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				constants.LabelEnabled: "true",
			},
			Annotations: map[string]string{
				constants.AnnotationSync:                      "true",
				constants.AnnotationTargetNamespaces:          "app1,app2",
				constants.AnnotationExclude:                   "false",
				constants.AnnotationRecreateOnImmutableChange: "true",
			},
		},
		Data: map[string][]byte{"key": []byte("value")},
	}

	mirror, err := CreateMirror(source, "app1")
	require.NoError(t, err)

	secretMirror := mirror.(*corev1.Secret)

	// Verify sync annotations are NOT copied
	assert.NotContains(t, secretMirror.Annotations, constants.AnnotationSync)
	assert.NotContains(t, secretMirror.Annotations, constants.AnnotationTargetNamespaces)

	// Verify enabled label is NOT copied
	assert.NotContains(t, secretMirror.Labels, constants.LabelEnabled)

	// Verify ownership annotations ARE present
	assert.Contains(t, secretMirror.Annotations, constants.AnnotationSourceNamespace)
}

// Benchmarks for critical paths

func BenchmarkCreateMirror_Secret(b *testing.B) {
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "bench-secret",
			Namespace:       "default",
			UID:             "uid-123",
			ResourceVersion: "100",
			Generation:      1,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte("secret123"),
			"username": []byte("admin"),
			"token":    []byte("abcdef123456"),
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = CreateMirror(source, "target-ns")
	}
}

func BenchmarkCreateMirror_ConfigMap(b *testing.B) {
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "bench-config",
			Namespace:       "default",
			UID:             "uid-456",
			ResourceVersion: "200",
		},
		Data: map[string]string{
			"config.yaml": "key1: value1\nkey2: value2\nkey3: value3",
			"app.conf":    "setting=value",
		},
		BinaryData: map[string][]byte{
			"binary": {0x00, 0x01, 0x02, 0x03, 0x04},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = CreateMirror(source, "target-ns")
	}
}

func BenchmarkCreateMirror_Unstructured(b *testing.B) {
	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "traefik.io/v1alpha1",
			"kind":       "Middleware",
			"metadata": map[string]interface{}{
				"name":            "bench-middleware",
				"namespace":       "traefik",
				"uid":             "uid-789",
				"resourceVersion": "300",
				"generation":      int64(3),
			},
			"spec": map[string]interface{}{
				"basicAuth": map[string]interface{}{
					"secret": "auth-secret",
				},
				"headers": map[string]interface{}{
					"customRequestHeaders": map[string]interface{}{
						"X-Custom-Header": "value",
					},
				},
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = CreateMirror(source, "target-ns")
	}
}

func BenchmarkUpdateMirror_Secret(b *testing.B) {
	mirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "app1",
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ControllerName,
			},
			Annotations: map[string]string{
				constants.AnnotationSourceContentHash: "oldhash",
			},
		},
		Data: map[string][]byte{
			"password": []byte("old"),
		},
		Type: corev1.SecretTypeOpaque,
	}

	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-secret",
			Namespace:  "default",
			Generation: 10,
		},
		Data: map[string][]byte{
			"password": []byte("new"),
		},
		Type: corev1.SecretTypeOpaque,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = UpdateMirror(mirror, source)
	}
}

func BenchmarkUpdateMirror_ConfigMap(b *testing.B) {
	mirror := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "app1",
			Annotations: map[string]string{
				constants.AnnotationSourceContentHash: "oldhash",
			},
		},
		Data: map[string]string{
			"key": "old",
		},
	}

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"key": "new",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = UpdateMirror(mirror, source)
	}
}

func BenchmarkIsManagedByUs(b *testing.B) {
	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ControllerName,
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = IsManagedByUs(obj)
	}
}

func BenchmarkGetSourceReference(b *testing.B) {
	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				constants.AnnotationSourceNamespace: "default",
				constants.AnnotationSourceName:      "my-secret",
				constants.AnnotationSourceUID:       "uid-123",
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = GetSourceReference(obj)
	}
}
