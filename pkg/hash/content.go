// Package hash provides content hashing functionality for detecting resource changes.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
)

// ComputeContentHash computes a SHA256 hash of the resource's actual content.
// It excludes metadata fields (resourceVersion, managedFields, etc.) and status.
// This detects actual content changes vs Kubernetes metadata changes.
func ComputeContentHash(obj runtime.Object) (string, error) {
	content, err := extractContent(obj)
	if err != nil {
		return "", fmt.Errorf("failed to extract content: %w", err)
	}

	// Convert to JSON for consistent hashing
	jsonBytes, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("failed to marshal content: %w", err)
	}

	// Compute SHA256
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:]), nil
}

// extractContent extracts only the content fields from a resource.
// Excludes all metadata except name, namespace, labels, and annotations we care about.
func extractContent(obj runtime.Object) (interface{}, error) {
	// Try typed resources first
	switch resource := obj.(type) {
	case *corev1.Secret:
		return extractSecretContent(resource), nil
	case *corev1.ConfigMap:
		return extractConfigMapContent(resource), nil
	default:
		// Fall back to unstructured for CRDs and unknown types
		return extractUnstructuredContent(obj)
	}
}

// extractSecretContent extracts content from a Secret.
func extractSecretContent(secret *corev1.Secret) map[string]interface{} {
	content := map[string]interface{}{
		"type":       string(secret.Type),
		"data":       secret.Data,
		"stringData": secret.StringData,
	}

	// Include transform annotation in hash so changes to transformation rules trigger updates
	if secret.Annotations != nil {
		if transform, exists := secret.Annotations[constants.AnnotationTransform]; exists {
			content["transform"] = transform
		}
	}

	return content
}

// extractConfigMapContent extracts content from a ConfigMap.
func extractConfigMapContent(cm *corev1.ConfigMap) map[string]interface{} {
	content := map[string]interface{}{
		"data":       cm.Data,
		"binaryData": cm.BinaryData,
	}

	// Include transform annotation in hash so changes to transformation rules trigger updates
	if cm.Annotations != nil {
		if transform, exists := cm.Annotations[constants.AnnotationTransform]; exists {
			content["transform"] = transform
		}
	}

	return content
}

// extractUnstructuredContent extracts content from an unstructured resource (CRDs, etc.).
//
// Hashes every non-Kubernetes-managed field at the top level — not only spec
// — so resources with both spec and data (e.g. an unstructured Secret/CM, or
// a CRD using a custom schema) detect drift on every content field, matching
// the fields that updateUnstructuredMirror copies to the mirror.
//
// When a transform annotation is present the source's labels and annotations
// are also folded into the hash, because templates can read them via
// TransformContext.Labels / .Annotations and a label change would otherwise
// be invisible to NeedsSync.
func extractUnstructuredContent(obj runtime.Object) (interface{}, error) {
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	// Make a deep copy to avoid race conditions when accessing nested fields
	// (NestedMap may modify the underlying map).
	u := (&unstructured.Unstructured{Object: unstructuredObj}).DeepCopy()

	skipFields := map[string]bool{
		"metadata":   true,
		"status":     true,
		"apiVersion": true,
		"kind":       true,
	}

	content := make(map[string]interface{})
	for key, value := range u.Object {
		if !skipFields[key] {
			content[key] = value
		}
	}

	annotations := u.GetAnnotations()
	if transform, exists := annotations[constants.AnnotationTransform]; exists && transform != "" {
		content["transform"] = transform
		// Templates can read source labels and annotations; include them so a
		// label/annotation change triggers re-render of transformed mirrors.
		// Filter out the kubemirror.raczylo.com/* keys to avoid the source's
		// own bookkeeping (sync-status annotation, etc.) churning the hash.
		content["sourceLabels"] = filterKubeMirror(u.GetLabels())
		content["sourceAnnotations"] = filterKubeMirror(annotations)
	}

	return content, nil
}

// filterKubeMirror returns a copy of m with all kubemirror.raczylo.com/* keys
// removed. Used to exclude controller-managed keys from content hashing so
// the controller's own writes don't churn the hash.
func filterKubeMirror(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if !strings.HasPrefix(k, constants.Domain+"/") {
			out[k] = v
		}
	}
	return out
}

// NeedsSync determines if a target resource needs to be synced based on content changes.
// It uses a multi-layer strategy:
// 1. Check generation field (if available) - fastest
// 2. Check content hash - universal
func NeedsSync(source, target runtime.Object, targetAnnotations map[string]string) (bool, error) {
	// Layer 1: Generation-based check (for resources that support it)
	sourceGen := getGeneration(source)
	if sourceGen > 0 {
		targetSourceGen := targetAnnotations[constants.AnnotationSourceGeneration]
		if fmt.Sprintf("%d", sourceGen) != targetSourceGen {
			return true, nil // Generation changed
		}
	}

	// Layer 2: Content hash check (works for all resources)
	sourceHash, err := ComputeContentHash(source)
	if err != nil {
		return false, fmt.Errorf("failed to compute source hash: %w", err)
	}

	targetSourceHash := targetAnnotations[constants.AnnotationSourceContentHash]
	if sourceHash != targetSourceHash {
		return true, nil // Content changed
	}

	// No changes detected
	return false, nil
}

// getGeneration extracts the generation field from a resource if it exists.
// Returns 0 if the resource doesn't have a generation field.
func getGeneration(obj runtime.Object) int64 {
	// Convert to unstructured to access generation
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return 0
	}

	u := &unstructured.Unstructured{Object: unstructuredObj}
	return u.GetGeneration()
}
