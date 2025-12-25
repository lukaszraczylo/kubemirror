// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/hash"
)

// CreateMirror creates a mirror resource in the target namespace.
// It copies the source resource's spec/data and adds ownership annotations.
func CreateMirror(source runtime.Object, targetNamespace string) (runtime.Object, error) {
	// Compute content hash of source
	sourceHash, err := hash.ComputeContentHash(source)
	if err != nil {
		return nil, fmt.Errorf("failed to compute source hash: %w", err)
	}

	// Handle typed resources
	switch src := source.(type) {
	case *corev1.Secret:
		return createSecretMirror(src, targetNamespace, sourceHash)
	case *corev1.ConfigMap:
		return createConfigMapMirror(src, targetNamespace, sourceHash)
	default:
		// For unstructured/CRD resources
		return createUnstructuredMirror(source, targetNamespace, sourceHash)
	}
}

// createSecretMirror creates a mirror of a Secret.
func createSecretMirror(source *corev1.Secret, targetNamespace, sourceHash string) (*corev1.Secret, error) {
	mirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      source.Name,
			Namespace: targetNamespace,
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ControllerName,
				constants.LabelMirror:    "true",
			},
			Annotations: buildMirrorAnnotations(source, sourceHash),
		},
		Type: source.Type,
		Data: source.Data,
		// Note: Don't copy StringData as it's write-only and gets converted to Data
	}

	return mirror, nil
}

// createConfigMapMirror creates a mirror of a ConfigMap.
func createConfigMapMirror(source *corev1.ConfigMap, targetNamespace, sourceHash string) (*corev1.ConfigMap, error) {
	mirror := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      source.Name,
			Namespace: targetNamespace,
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ControllerName,
				constants.LabelMirror:    "true",
			},
			Annotations: buildMirrorAnnotations(source, sourceHash),
		},
		Data:       source.Data,
		BinaryData: source.BinaryData,
	}

	return mirror, nil
}

// filterKubeMirrorMetadata removes all kubemirror.raczylo.com/* keys from metadata.
// This prevents source kubemirror labels/annotations from being copied to mirrors.
func filterKubeMirrorMetadata(metadata map[string]string) map[string]string {
	filtered := make(map[string]string)
	for k, v := range metadata {
		// Skip all kubemirror.raczylo.com keys
		if !strings.HasPrefix(k, "kubemirror.raczylo.com/") {
			filtered[k] = v
		}
	}
	return filtered
}

// createUnstructuredMirror creates a mirror of an unstructured resource (CRD).
func createUnstructuredMirror(source runtime.Object, targetNamespace, sourceHash string) (*unstructured.Unstructured, error) {
	// Convert to unstructured
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(source)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{Object: unstructuredObj}

	// Create mirror
	mirror := u.DeepCopy()
	mirror.SetNamespace(targetNamespace)

	// Remove kubemirror labels from source (don't propagate to mirrors)
	labels := mirror.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels = filterKubeMirrorMetadata(labels)
	labels[constants.LabelManagedBy] = constants.ControllerName
	labels[constants.LabelMirror] = "true"
	mirror.SetLabels(labels)

	// Remove kubemirror annotations from source (don't propagate to mirrors)
	existingAnnotations := mirror.GetAnnotations()
	if existingAnnotations == nil {
		existingAnnotations = make(map[string]string)
	}
	existingAnnotations = filterKubeMirrorMetadata(existingAnnotations)

	// Add mirror-specific annotations
	annotations := buildMirrorAnnotations(source, sourceHash)
	for k, v := range annotations {
		existingAnnotations[k] = v
	}
	mirror.SetAnnotations(existingAnnotations)

	// Remove status (never mirror status)
	unstructured.RemoveNestedField(mirror.Object, "status")

	// Clear resource-specific metadata
	mirror.SetResourceVersion("")
	mirror.SetUID("")
	mirror.SetGeneration(0)
	mirror.SetCreationTimestamp(metav1.Time{})
	mirror.SetFinalizers(nil) // Mirrors should not have finalizers

	return mirror, nil
}

// buildMirrorAnnotations builds the ownership annotations for a mirror resource.
func buildMirrorAnnotations(source runtime.Object, sourceHash string) map[string]string {
	sourceObj, _ := source.(metav1.Object)

	annotations := map[string]string{
		constants.AnnotationSourceNamespace:   sourceObj.GetNamespace(),
		constants.AnnotationSourceName:        sourceObj.GetName(),
		constants.AnnotationSourceUID:         string(sourceObj.GetUID()),
		constants.AnnotationSourceContentHash: sourceHash,
		constants.AnnotationLastSyncTime:      time.Now().UTC().Format(time.RFC3339),
	}

	// Add generation if available
	if sourceObj.GetGeneration() > 0 {
		annotations[constants.AnnotationSourceGeneration] = fmt.Sprintf("%d", sourceObj.GetGeneration())
	}

	// Add resource version for debugging
	if sourceObj.GetResourceVersion() != "" {
		annotations[constants.AnnotationSourceResourceVersion] = sourceObj.GetResourceVersion()
	}

	return annotations
}

// UpdateMirror updates an existing mirror with new source content.
func UpdateMirror(mirror, source runtime.Object) error {
	// Compute new source hash
	sourceHash, err := hash.ComputeContentHash(source)
	if err != nil {
		return fmt.Errorf("failed to compute source hash: %w", err)
	}

	// Update based on type
	switch m := mirror.(type) {
	case *corev1.Secret:
		src := source.(*corev1.Secret)
		m.Data = src.Data
		m.Type = src.Type
		updateMirrorAnnotations(m, source, sourceHash)
	case *corev1.ConfigMap:
		src := source.(*corev1.ConfigMap)
		m.Data = src.Data
		m.BinaryData = src.BinaryData
		updateMirrorAnnotations(m, source, sourceHash)
	default:
		// Unstructured
		return updateUnstructuredMirror(mirror, source, sourceHash)
	}

	return nil
}

// updateMirrorAnnotations updates the ownership annotations on a mirror.
func updateMirrorAnnotations(mirror metav1.Object, source runtime.Object, sourceHash string) {
	sourceObj, _ := source.(metav1.Object)

	annotations := mirror.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[constants.AnnotationSourceContentHash] = sourceHash
	annotations[constants.AnnotationLastSyncTime] = time.Now().UTC().Format(time.RFC3339)

	if sourceObj.GetGeneration() > 0 {
		annotations[constants.AnnotationSourceGeneration] = fmt.Sprintf("%d", sourceObj.GetGeneration())
	}

	if sourceObj.GetResourceVersion() != "" {
		annotations[constants.AnnotationSourceResourceVersion] = sourceObj.GetResourceVersion()
	}

	mirror.SetAnnotations(annotations)
}

// updateUnstructuredMirror updates an unstructured mirror.
func updateUnstructuredMirror(mirror, source runtime.Object, sourceHash string) error {
	m := mirror.(*unstructured.Unstructured)
	s := source.(*unstructured.Unstructured)

	// Update spec
	sourceSpec, found, err := unstructured.NestedMap(s.Object, "spec")
	if err != nil {
		return fmt.Errorf("failed to get source spec: %w", err)
	}
	if found {
		if err := unstructured.SetNestedMap(m.Object, sourceSpec, "spec"); err != nil {
			return fmt.Errorf("failed to set mirror spec: %w", err)
		}
	}

	// Update annotations
	updateMirrorAnnotations(m, source, sourceHash)

	// Ensure mirrors never have finalizers (even if they were added before this fix)
	m.SetFinalizers(nil)

	return nil
}

// IsManagedByUs checks if a resource is managed by kubemirror.
func IsManagedByUs(obj metav1.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	return labels[constants.LabelManagedBy] == constants.ControllerName
}

// IsMirrorResource checks if a resource is a mirror (not a source).
func IsMirrorResource(obj metav1.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	return labels[constants.LabelMirror] == "true"
}

// GetSourceReference extracts the source reference from a mirror's annotations.
func GetSourceReference(mirror metav1.Object) (namespace, name, uid string, found bool) {
	annotations := mirror.GetAnnotations()
	if annotations == nil {
		return "", "", "", false
	}

	namespace = annotations[constants.AnnotationSourceNamespace]
	name = annotations[constants.AnnotationSourceName]
	uid = annotations[constants.AnnotationSourceUID]

	if namespace == "" || name == "" {
		return "", "", "", false
	}

	return namespace, name, uid, true
}
