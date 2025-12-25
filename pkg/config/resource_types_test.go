package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseResourceType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ResourceType
		wantErr bool
	}{
		{
			name:  "core resource - Secret",
			input: "Secret.v1",
			want: ResourceType{
				Kind:    "Secret",
				Version: "v1",
				Group:   "",
			},
		},
		{
			name:  "core resource - ConfigMap",
			input: "ConfigMap.v1",
			want: ResourceType{
				Kind:    "ConfigMap",
				Version: "v1",
				Group:   "",
			},
		},
		{
			name:  "resource with simple group",
			input: "Ingress.v1.networking.k8s.io",
			want: ResourceType{
				Kind:    "Ingress",
				Version: "v1",
				Group:   "networking.k8s.io",
			},
		},
		{
			name:  "resource with complex group",
			input: "Middleware.v1alpha1.traefik.io",
			want: ResourceType{
				Kind:    "Middleware",
				Version: "v1alpha1",
				Group:   "traefik.io",
			},
		},
		{
			name:  "CRD example",
			input: "Certificate.v1.cert-manager.io",
			want: ResourceType{
				Kind:    "Certificate",
				Version: "v1",
				Group:   "cert-manager.io",
			},
		},
		{
			name:    "invalid format - single part",
			input:   "Secret",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResourceType(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceType_GroupVersionKind(t *testing.T) {
	tests := []struct {
		name string
		rt   ResourceType
		want schema.GroupVersionKind
	}{
		{
			name: "core resource",
			rt: ResourceType{
				Kind:    "Secret",
				Version: "v1",
				Group:   "",
			},
			want: schema.GroupVersionKind{
				Kind:    "Secret",
				Version: "v1",
				Group:   "",
			},
		},
		{
			name: "resource with group",
			rt: ResourceType{
				Kind:    "Ingress",
				Version: "v1",
				Group:   "networking.k8s.io",
			},
			want: schema.GroupVersionKind{
				Kind:    "Ingress",
				Version: "v1",
				Group:   "networking.k8s.io",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rt.GroupVersionKind()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceType_String(t *testing.T) {
	tests := []struct {
		name string
		rt   ResourceType
		want string
	}{
		{
			name: "core resource",
			rt: ResourceType{
				Kind:    "Secret",
				Version: "v1",
				Group:   "",
			},
			want: "Secret.v1",
		},
		{
			name: "resource with group",
			rt: ResourceType{
				Kind:    "Ingress",
				Version: "v1",
				Group:   "networking.k8s.io",
			},
			want: "Ingress.v1.networking.k8s.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rt.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseResourceTypes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []ResourceType
		wantErr bool
	}{
		{
			name:  "empty string returns defaults",
			input: "",
			want:  DefaultResourceTypes(),
		},
		{
			name:  "single resource type",
			input: "Secret.v1",
			want: []ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
			},
		},
		{
			name:  "multiple resource types",
			input: "Secret.v1,ConfigMap.v1,Ingress.v1.networking.k8s.io",
			want: []ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
				{Kind: "Ingress", Version: "v1", Group: "networking.k8s.io"},
			},
		},
		{
			name:  "with whitespace",
			input: " Secret.v1 , ConfigMap.v1 ",
			want: []ResourceType{
				{Kind: "Secret", Version: "v1", Group: ""},
				{Kind: "ConfigMap", Version: "v1", Group: ""},
			},
		},
		{
			name:    "invalid format in list",
			input:   "Secret.v1,Invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResourceTypes(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultResourceTypes(t *testing.T) {
	defaults := DefaultResourceTypes()
	assert.Len(t, defaults, 2)
	assert.Contains(t, defaults, ResourceType{Kind: "Secret", Version: "v1", Group: ""})
	assert.Contains(t, defaults, ResourceType{Kind: "ConfigMap", Version: "v1", Group: ""})
}
