// Package hash provides content hashing functionality for detecting resource changes.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
	return map[string]interface{}{
		"type":       string(secret.Type),
		"data":       secret.Data,
		"stringData": secret.StringData,
	}
}

// extractConfigMapContent extracts content from a ConfigMap.
func extractConfigMapContent(cm *corev1.ConfigMap) map[string]interface{} {
	return map[string]interface{}{
		"data":       cm.Data,
		"binaryData": cm.BinaryData,
	}
}

// extractUnstructuredContent extracts content from an unstructured resource (CRDs, etc.).
func extractUnstructuredContent(obj runtime.Object) (interface{}, error) {
	// Convert to unstructured
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{Object: unstructuredObj}

	// Make a deep copy to avoid race conditions when accessing nested fields
	// NestedMap modifies the underlying map, so we need our own copy
	uCopy := u.DeepCopy()

	// Extract spec (most resources have spec)
	spec, found, err := unstructured.NestedMap(uCopy.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to extract spec: %w", err)
	}

	content := make(map[string]interface{})
	if found {
		content["spec"] = spec
	}

	// For resources without spec, include all fields except metadata and status
	if !found {
		for key, value := range uCopy.Object {
			if key != "metadata" && key != "status" && key != "apiVersion" && key != "kind" {
				content[key] = value
			}
		}
	}

	return content, nil
}

// NeedsSync determines if a target resource needs to be synced based on content changes.
// It uses a multi-layer strategy:
// 1. Check generation field (if available) - fastest
// 2. Check content hash - universal
func NeedsSync(source, target runtime.Object, targetAnnotations map[string]string) (bool, error) {
	// Layer 1: Generation-based check (for resources that support it)
	sourceGen := getGeneration(source)
	if sourceGen > 0 {
		targetSourceGen := targetAnnotations["source-generation"]
		if fmt.Sprintf("%d", sourceGen) != targetSourceGen {
			return true, nil // Generation changed
		}
	}

	// Layer 2: Content hash check (works for all resources)
	sourceHash, err := ComputeContentHash(source)
	if err != nil {
		return false, fmt.Errorf("failed to compute source hash: %w", err)
	}

	targetSourceHash := targetAnnotations["source-content-hash"]
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
