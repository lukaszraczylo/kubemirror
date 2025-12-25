package config

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResourceType defines a Kubernetes resource type to mirror.
type ResourceType struct {
	Group   string
	Version string
	Kind    string
}

// GroupVersionKind returns the GVK for this resource type.
func (r ResourceType) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   r.Group,
		Version: r.Version,
		Kind:    r.Kind,
	}
}

// String returns a string representation of the resource type.
func (r ResourceType) String() string {
	if r.Group == "" {
		return fmt.Sprintf("%s.%s", r.Kind, r.Version)
	}
	return fmt.Sprintf("%s.%s.%s", r.Kind, r.Version, r.Group)
}

// ParseResourceType parses a resource type string in the format "kind.version.group" or "kind.version".
// Examples: "Secret.v1", "Ingress.v1.networking.k8s.io", "Middleware.v1alpha1.traefik.io"
func ParseResourceType(s string) (ResourceType, error) {
	parts := strings.Split(s, ".")

	switch len(parts) {
	case 2:
		// Core resources: "Secret.v1"
		return ResourceType{
			Kind:    parts[0],
			Version: parts[1],
			Group:   "",
		}, nil
	case 3:
		// Resources with group: "Ingress.v1.networking.k8s.io"
		return ResourceType{
			Kind:    parts[0],
			Version: parts[1],
			Group:   parts[2],
		}, nil
	default:
		// Support more complex groups with dots: "Middleware.v1alpha1.traefik.io"
		if len(parts) >= 3 {
			return ResourceType{
				Kind:    parts[0],
				Version: parts[1],
				Group:   strings.Join(parts[2:], "."),
			}, nil
		}
		return ResourceType{}, fmt.Errorf("invalid resource type format: %s (expected kind.version or kind.version.group)", s)
	}
}

// DefaultResourceTypes returns the default set of resource types to mirror.
func DefaultResourceTypes() []ResourceType {
	return []ResourceType{
		{Kind: "Secret", Version: "v1", Group: ""},
		{Kind: "ConfigMap", Version: "v1", Group: ""},
	}
}

// ParseResourceTypes parses a comma-separated list of resource type strings.
func ParseResourceTypes(s string) ([]ResourceType, error) {
	if s == "" {
		return DefaultResourceTypes(), nil
	}

	parts := strings.Split(s, ",")
	types := make([]ResourceType, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		rt, err := ParseResourceType(part)
		if err != nil {
			return nil, fmt.Errorf("failed to parse resource type %q: %w", part, err)
		}
		types = append(types, rt)
	}

	return types, nil
}
