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
	"github.com/lukaszraczylo/kubemirror/pkg/transformer"
)

// CreateMirror creates a mirror resource in the target namespace.
// It copies the source resource's spec/data and adds ownership annotations.
// If transformation rules are present, they are applied to the mirror.
func CreateMirror(source runtime.Object, targetNamespace string) (runtime.Object, error) {
	// Compute content hash of source
	sourceHash, err := hash.ComputeContentHash(source)
	if err != nil {
		return nil, fmt.Errorf("failed to compute source hash: %w", err)
	}

	// Create the mirror based on type
	var mirror runtime.Object
	switch src := source.(type) {
	case *corev1.Secret:
		mirror, err = createSecretMirror(src, targetNamespace, sourceHash)
	case *corev1.ConfigMap:
		mirror, err = createConfigMapMirror(src, targetNamespace, sourceHash)
	default:
		// For unstructured/CRD resources
		mirror, err = createUnstructuredMirror(source, targetNamespace, sourceHash)
	}

	if err != nil {
		return nil, err
	}

	// Apply transformations if rules are present
	mirror, err = applyTransformations(source, mirror, targetNamespace)
	if err != nil {
		return nil, fmt.Errorf("transformation failed: %w", err)
	}

	return mirror, nil
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
	// IMPORTANT: Mirrors should never have ownerReferences from source.
	// KubeMirror manages mirrors via labels/annotations, not ownership.
	// This allows sources to be owned by other controllers (ExternalSecrets, ArgoCD, etc.)
	// while KubeMirror independently manages the mirrors.
	mirror.SetOwnerReferences(nil)

	return mirror, nil
}

// buildMirrorAnnotations builds the ownership annotations for a mirror resource.
// Returns empty map if source doesn't implement metav1.Object.
func buildMirrorAnnotations(source runtime.Object, sourceHash string) map[string]string {
	sourceObj, ok := source.(metav1.Object)
	if !ok {
		// This should never happen for valid Kubernetes resources.
		// Return minimal annotations with just the hash.
		return map[string]string{
			constants.AnnotationSourceContentHash: sourceHash,
			constants.AnnotationLastSyncTime:      time.Now().UTC().Format(time.RFC3339),
		}
	}

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
// It also applies transformations if transformation rules are present in the source.
func UpdateMirror(mirror, source runtime.Object) error {
	// Compute new source hash
	sourceHash, err := hash.ComputeContentHash(source)
	if err != nil {
		return fmt.Errorf("failed to compute source hash: %w", err)
	}

	// Update based on type
	switch m := mirror.(type) {
	case *corev1.Secret:
		src, ok := source.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("mirror is Secret but source is %T", source)
		}
		m.Data = src.Data
		m.Type = src.Type
		updateMirrorAnnotations(m, source, sourceHash)
	case *corev1.ConfigMap:
		src, ok := source.(*corev1.ConfigMap)
		if !ok {
			return fmt.Errorf("mirror is ConfigMap but source is %T", source)
		}
		m.Data = src.Data
		m.BinaryData = src.BinaryData
		updateMirrorAnnotations(m, source, sourceHash)
	default:
		// Unstructured
		err = updateUnstructuredMirror(mirror, source, sourceHash)
		if err != nil {
			return err
		}
	}

	// Apply transformations after updating data (only if transformation rules exist)
	mirrorObj, ok := mirror.(metav1.Object)
	if !ok {
		return fmt.Errorf("mirror does not implement metav1.Object, got %T", mirror)
	}
	targetNamespace := mirrorObj.GetNamespace()
	transformed, err := applyTransformations(source, mirror, targetNamespace)
	if err != nil {
		return fmt.Errorf("transformation failed: %w", err)
	}

	// Copy transformed data back to mirror if transformation was applied
	// Transformer returns unstructured when transformations are applied, original type otherwise
	if transformedU, ok := transformed.(*unstructured.Unstructured); ok {
		// Transformation was applied, copy data back to typed mirror
		switch m := mirror.(type) {
		case *corev1.Secret:
			if data, found, _ := unstructured.NestedMap(transformedU.Object, "data"); found {
				m.Data = convertToByteMap(data)
			}
			// Copy potentially transformed labels and annotations
			m.SetLabels(transformedU.GetLabels())
			m.SetAnnotations(transformedU.GetAnnotations())
		case *corev1.ConfigMap:
			if data, found, _ := unstructured.NestedMap(transformedU.Object, "data"); found {
				m.Data = convertToStringMap(data)
			}
			if binData, found, _ := unstructured.NestedMap(transformedU.Object, "binaryData"); found {
				m.BinaryData = convertToByteMap(binData)
			}
			// Copy potentially transformed labels and annotations
			m.SetLabels(transformedU.GetLabels())
			m.SetAnnotations(transformedU.GetAnnotations())
		case *unstructured.Unstructured:
			// For unstructured, the transformation is already applied in-place
			m.Object = transformedU.Object
		}
	}
	// If transformed is not unstructured, no transformation was applied (no rules)
	// and we can just use the mirror as-is

	return nil
}

// convertToStringMap converts map[string]interface{} to map[string]string.
func convertToStringMap(data map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range data {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// convertToByteMap converts map[string]interface{} to map[string][]byte.
func convertToByteMap(data map[string]interface{}) map[string][]byte {
	result := make(map[string][]byte)
	for k, v := range data {
		switch val := v.(type) {
		case string:
			result[k] = []byte(val)
		case []byte:
			result[k] = val
		}
	}
	return result
}

// updateMirrorAnnotations updates the ownership annotations on a mirror.
func updateMirrorAnnotations(mirror metav1.Object, source runtime.Object, sourceHash string) {
	annotations := mirror.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[constants.AnnotationSourceContentHash] = sourceHash
	annotations[constants.AnnotationLastSyncTime] = time.Now().UTC().Format(time.RFC3339)

	// Safely extract source metadata if available
	sourceObj, ok := source.(metav1.Object)
	if ok {
		if sourceObj.GetGeneration() > 0 {
			annotations[constants.AnnotationSourceGeneration] = fmt.Sprintf("%d", sourceObj.GetGeneration())
		}

		if sourceObj.GetResourceVersion() != "" {
			annotations[constants.AnnotationSourceResourceVersion] = sourceObj.GetResourceVersion()
		}
	}

	mirror.SetAnnotations(annotations)
}

// updateUnstructuredMirror updates an unstructured mirror.
// Uses generic field introspection to handle any resource type (Secrets, ConfigMaps, CRDs).
func updateUnstructuredMirror(mirror, source runtime.Object, sourceHash string) error {
	m, ok := mirror.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("mirror is not *unstructured.Unstructured, got %T", mirror)
	}
	s, ok := source.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("source is not *unstructured.Unstructured, got %T", source)
	}

	// Fields to skip (Kubernetes-managed fields, not user content)
	// These are managed by Kubernetes API server or controllers
	skipFields := map[string]bool{
		// Standard Kubernetes top-level fields
		"metadata":   true, // Kubernetes metadata (name, namespace, labels, etc.) - managed separately
		"status":     true, // Resource status - managed by controllers, never mirrored
		"apiVersion": true, // API group version - static, set during creation
		"kind":       true, // Resource kind - static, set during creation

		// Kubernetes internal fields (rarely at top level, but be defensive)
		"managedFields":              true, // Field management tracking - internal to Kubernetes
		"selfLink":                   true, // Deprecated but might exist - auto-generated
		"resourceVersion":            true, // Optimistic concurrency control - auto-generated
		"generation":                 true, // Spec change counter - auto-generated (but usually in metadata)
		"creationTimestamp":          true, // Resource creation time - auto-generated (but usually in metadata)
		"deletionTimestamp":          true, // Resource deletion time - auto-generated (but usually in metadata)
		"deletionGracePeriodSeconds": true, // Grace period - auto-managed (but usually in metadata)
		"uid":                        true, // Unique identifier - auto-generated (but usually in metadata)
		"ownerReferences":            true, // Ownership chain - should not be copied (but usually in metadata)
		"finalizers":                 true, // Deletion hooks - should not be copied (but usually in metadata)
	}

	// Copy all content fields from source to mirror
	// This handles:
	// - .spec (standard CRDs like Traefik Middleware)
	// - .data, .type (Secrets)
	// - .data, .binaryData (ConfigMaps)
	// - Any custom top-level fields in non-standard CRDs
	for key, value := range s.Object {
		if !skipFields[key] {
			m.Object[key] = value
		}
	}

	// Update annotations
	updateMirrorAnnotations(m, source, sourceHash)

	// Ensure mirrors never have finalizers (even if they were added before this fix)
	m.SetFinalizers(nil)

	// Ensure mirrors never have ownerReferences (clean up mirrors from before this fix)
	// KubeMirror uses labels/annotations for management, not ownerReferences
	m.SetOwnerReferences(nil)

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

// applyTransformations applies transformation rules from the source to the mirror.
// Returns the transformed mirror, or the original mirror if no rules are present.
func applyTransformations(source, mirror runtime.Object, targetNamespace string) (runtime.Object, error) {
	// Get source annotations to check for transform rules
	sourceObj, ok := source.(metav1.Object)
	if !ok {
		return mirror, nil
	}

	sourceAnnotations := sourceObj.GetAnnotations()
	if sourceAnnotations == nil {
		return mirror, nil
	}

	transformRules, hasTransform := sourceAnnotations[constants.AnnotationTransform]
	if !hasTransform || transformRules == "" {
		return mirror, nil // No transformation rules
	}

	// Temporarily copy transform annotations to mirror for Transform to read
	// The Transform function reads rules from the object being transformed
	mirrorObj, ok := mirror.(metav1.Object)
	if !ok {
		return mirror, nil
	}

	// Save original annotations to restore on failure
	originalAnnotations := mirrorObj.GetAnnotations()
	var savedAnnotations map[string]string
	if originalAnnotations != nil {
		savedAnnotations = make(map[string]string, len(originalAnnotations))
		for k, v := range originalAnnotations {
			savedAnnotations[k] = v
		}
	}

	mirrorAnnotations := mirrorObj.GetAnnotations()
	if mirrorAnnotations == nil {
		mirrorAnnotations = make(map[string]string)
	}

	// Copy transform annotations from source
	mirrorAnnotations[constants.AnnotationTransform] = transformRules
	if strictMode, hasStrict := sourceAnnotations[constants.AnnotationTransformStrict]; hasStrict {
		mirrorAnnotations[constants.AnnotationTransformStrict] = strictMode
	}
	mirrorObj.SetAnnotations(mirrorAnnotations)

	// Build transformation context
	ctx := buildTransformContext(source, mirror, targetNamespace)

	// Create transformer with default options
	t := transformer.NewDefaultTransformer()

	// Apply transformations (transformer reads rules from mirror's annotations now)
	transformed, err := t.Transform(mirror, ctx)
	if err != nil {
		// Restore original annotations on failure to avoid leaving mirror in inconsistent state
		mirrorObj.SetAnnotations(savedAnnotations)
		return nil, err
	}

	// Remove transform annotations from result (they shouldn't persist on mirrors)
	if transformedObj, ok := transformed.(metav1.Object); ok {
		annotations := transformedObj.GetAnnotations()
		delete(annotations, constants.AnnotationTransform)
		delete(annotations, constants.AnnotationTransformStrict)
		transformedObj.SetAnnotations(annotations)
	}

	return transformed, nil
}

// buildTransformContext creates a transformation context from source and mirror metadata.
func buildTransformContext(source, mirror runtime.Object, targetNamespace string) transformer.TransformContext {
	sourceObj, _ := source.(metav1.Object)
	mirrorObj, _ := mirror.(metav1.Object)

	ctx := transformer.TransformContext{
		TargetNamespace: targetNamespace,
		SourceNamespace: sourceObj.GetNamespace(),
		SourceName:      sourceObj.GetName(),
		TargetName:      mirrorObj.GetName(),
	}

	// Copy labels (if any)
	if labels := sourceObj.GetLabels(); labels != nil {
		ctx.Labels = make(map[string]string)
		for k, v := range labels {
			ctx.Labels[k] = v
		}
	}

	// Copy annotations (if any)
	if annotations := sourceObj.GetAnnotations(); annotations != nil {
		ctx.Annotations = make(map[string]string)
		for k, v := range annotations {
			ctx.Annotations[k] = v
		}
	}

	return ctx
}
