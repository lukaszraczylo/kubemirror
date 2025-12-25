// Package discovery provides automatic resource type discovery for Kubernetes clusters.
package discovery

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
)

// ResourceDiscovery discovers all mirrorable resource types in a cluster.
type ResourceDiscovery struct {
	discoveryClient discovery.DiscoveryInterface
}

// NewResourceDiscovery creates a new resource discovery client.
func NewResourceDiscovery(cfg *rest.Config) (*ResourceDiscovery, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	return &ResourceDiscovery{
		discoveryClient: dc,
	}, nil
}

// DiscoverMirrorableResources discovers all resource types that can be mirrored.
// It filters out resources that shouldn't be mirrored based on a deny list.
func (d *ResourceDiscovery) DiscoverMirrorableResources(ctx context.Context) ([]config.ResourceType, error) {
	// Get all API resources in the cluster
	_, apiResourceLists, err := d.discoveryClient.ServerGroupsAndResources()
	if err != nil {
		// Partial errors are common (some APIs might not be fully available)
		// Continue with what we have
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return nil, fmt.Errorf("failed to discover API resources: %w", err)
		}
	}

	var resources []config.ResourceType
	seen := make(map[string]bool) // Deduplicate

	for _, apiResourceList := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, apiResource := range apiResourceList.APIResources {
			// Skip subresources (status, scale, etc.)
			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			// Skip if not namespaced (we only mirror namespaced resources)
			if !apiResource.Namespaced {
				continue
			}

			// Skip if resource doesn't support required verbs
			if !supportsRequiredVerbs(apiResource.Verbs) {
				continue
			}

			// Skip denied resource types
			if isDeniedResourceType(apiResource.Kind) {
				continue
			}

			rt := config.ResourceType{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    apiResource.Kind,
			}

			// Deduplicate by string representation
			key := rt.String()
			if seen[key] {
				continue
			}
			seen[key] = true

			resources = append(resources, rt)
		}
	}

	return resources, nil
}

// supportsRequiredVerbs checks if a resource supports the verbs needed for mirroring.
func supportsRequiredVerbs(verbs metav1.Verbs) bool {
	required := []string{"get", "list", "watch", "create", "update", "delete"}
	verbSet := make(map[string]bool)
	for _, v := range verbs {
		verbSet[v] = true
	}

	for _, req := range required {
		if !verbSet[req] {
			return false
		}
	}

	return true
}

// isDeniedResourceType checks if a resource type should never be mirrored.
var deniedKinds = map[string]bool{
	// Kubernetes core resources that shouldn't be mirrored
	"Pod":                   true,
	"Node":                  true,
	"Event":                 true,
	"Endpoints":             true,
	"EndpointSlice":         true,
	"ComponentStatus":       true,
	"Binding":               true,
	"ReplicationController": true, // Deprecated, use Deployment

	// Resources that are auto-generated or managed
	"ControllerRevision": true,
	"PodMetrics":         true,
	"NodeMetrics":        true,

	// Lease resources (used for leader election)
	"Lease": true,

	// CSI and storage resources
	"CSIDriver":          true,
	"CSINode":            true,
	"CSIStorageCapacity": true,
	"VolumeAttachment":   true,

	// Cluster-scoped resources that we filtered out but double-check
	"Namespace":                      true,
	"PersistentVolume":               true,
	"ClusterRole":                    true,
	"ClusterRoleBinding":             true,
	"CustomResourceDefinition":       true,
	"APIService":                     true,
	"ValidatingWebhookConfiguration": true,
	"MutatingWebhookConfiguration":   true,
}

func isDeniedResourceType(kind string) bool {
	return deniedKinds[kind]
}
