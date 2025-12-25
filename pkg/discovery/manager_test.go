package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
)

func TestDetectChanges(t *testing.T) {
	m := &Manager{}

	tests := []struct {
		name        string
		old         []config.ResourceType
		new         []config.ResourceType
		wantAdded   []config.ResourceType
		wantRemoved []config.ResourceType
	}{
		{
			name: "no changes",
			old: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			new: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name: "new resource added",
			old: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
			},
			new: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "Service", Version: "v1", Group: ""},
			},
			wantAdded: []config.ResourceType{
				{Kind: "Service", Version: "v1", Group: ""},
			},
			wantRemoved: nil,
		},
		{
			name: "resource removed",
			old: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			new: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
			},
			wantAdded: nil,
			wantRemoved: []config.ResourceType{
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
		},
		{
			name: "multiple changes",
			old: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			new: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "Service", Version: "v1", Group: ""},
				{Kind: "Ingress", Version: "v1", Group: "networking.k8s.io"},
			},
			wantAdded: []config.ResourceType{
				{Kind: "Service", Version: "v1", Group: ""},
				{Kind: "Ingress", Version: "v1", Group: "networking.k8s.io"},
			},
			wantRemoved: []config.ResourceType{
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
		},
		{
			name: "complete replacement",
			old: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			new: []config.ResourceType{
				{Kind: "Service", Version: "v1", Group: ""},
				{Kind: "Ingress", Version: "v1", Group: "networking.k8s.io"},
			},
			wantAdded: []config.ResourceType{
				{Kind: "Service", Version: "v1", Group: ""},
				{Kind: "Ingress", Version: "v1", Group: "networking.k8s.io"},
			},
			wantRemoved: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
		},
		{
			name: "from empty to populated",
			old:  []config.ResourceType{},
			new: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			wantAdded: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			wantRemoved: nil,
		},
		{
			name: "from populated to empty",
			old: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
			new:       []config.ResourceType{},
			wantAdded: nil,
			wantRemoved: []config.ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAdded, gotRemoved := m.detectChanges(tt.old, tt.new)

			// Sort for consistent comparison
			assert.ElementsMatch(t, tt.wantAdded, gotAdded, "added resources mismatch")
			assert.ElementsMatch(t, tt.wantRemoved, gotRemoved, "removed resources mismatch")
		})
	}
}

func TestResourceTypesToStrings(t *testing.T) {
	resources := []config.ResourceType{
		{Kind: "Secret", Version: "v1", Group: ""},
		{Kind: "Ingress", Version: "v1", Group: "networking.k8s.io"},
		{Kind: "Middleware", Version: "v1alpha1", Group: "traefik.io"},
	}

	want := []string{
		"Secret.v1",
		"Ingress.v1.networking.k8s.io",
		"Middleware.v1alpha1.traefik.io",
	}

	got := resourceTypesToStrings(resources)
	assert.Equal(t, want, got)
}
