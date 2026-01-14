package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSupportsRequiredVerbs(t *testing.T) {
	tests := []struct {
		name  string
		verbs metav1.Verbs
		want  bool
	}{
		{
			name:  "all required verbs present",
			verbs: metav1.Verbs{"get", "list", "watch", "create", "update", "patch", "delete"},
			want:  true,
		},
		{
			name:  "exact required verbs",
			verbs: metav1.Verbs{"get", "list", "watch", "create", "update", "delete"},
			want:  true,
		},
		{
			name:  "missing create verb",
			verbs: metav1.Verbs{"get", "list", "watch", "update", "delete"},
			want:  false,
		},
		{
			name:  "missing watch verb",
			verbs: metav1.Verbs{"get", "list", "create", "update", "delete"},
			want:  false,
		},
		{
			name:  "read-only resource",
			verbs: metav1.Verbs{"get", "list", "watch"},
			want:  false,
		},
		{
			name:  "empty verbs",
			verbs: metav1.Verbs{},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := supportsRequiredVerbs(tt.verbs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsDeniedResourceType(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want bool
	}{
		// Should be denied
		{name: "Pod", kind: "Pod", want: true},
		{name: "Event", kind: "Event", want: true},
		{name: "Endpoints", kind: "Endpoints", want: true},
		{name: "Node", kind: "Node", want: true},
		{name: "Lease", kind: "Lease", want: true},
		{name: "Namespace", kind: "Namespace", want: true},
		{name: "ClusterRole", kind: "ClusterRole", want: true},
		{name: "Certificate", kind: "Certificate", want: true}, // cert-manager resources are denied

		// Should NOT be denied
		{name: "Secret", kind: "Secret", want: false},
		{name: "ConfigMap", kind: "ConfigMap", want: false},
		{name: "Service", kind: "Service", want: false},
		{name: "Ingress", kind: "Ingress", want: false},
		{name: "Deployment", kind: "Deployment", want: false},
		{name: "StatefulSet", kind: "StatefulSet", want: false},
		{name: "Middleware", kind: "Middleware", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDeniedResourceType(tt.kind)
			assert.Equal(t, tt.want, got, "isDeniedResourceType(%s) = %v, want %v", tt.kind, got, tt.want)
		})
	}
}

func TestIsHighCardinalityResource(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want bool
	}{
		// High cardinality resources (should warn)
		{name: "ServiceAccount", kind: "ServiceAccount", want: true},
		{name: "Role", kind: "Role", want: true},
		{name: "RoleBinding", kind: "RoleBinding", want: true},
		{name: "NetworkPolicy", kind: "NetworkPolicy", want: true},
		{name: "ServiceMonitor", kind: "ServiceMonitor", want: true},
		{name: "VirtualService", kind: "VirtualService", want: true},

		// Not high cardinality (no warning needed)
		{name: "Secret", kind: "Secret", want: false},
		{name: "ConfigMap", kind: "ConfigMap", want: false},
		{name: "Service", kind: "Service", want: false},
		{name: "Deployment", kind: "Deployment", want: false},
		{name: "Middleware", kind: "Middleware", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHighCardinalityResource(tt.kind)
			assert.Equal(t, tt.want, got, "isHighCardinalityResource(%s) = %v, want %v", tt.kind, got, tt.want)
		})
	}
}
