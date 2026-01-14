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
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
)

var discoveryLog = ctrl.Log.WithName("discovery")

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
	logger := discoveryLog.WithName("discover")

	// Get all API resources in the cluster
	_, apiResourceLists, err := d.discoveryClient.ServerGroupsAndResources()
	if err != nil {
		// Partial errors are common (some APIs might not be fully available)
		// Continue with what we have
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return nil, fmt.Errorf("failed to discover API resources: %w", err)
		}
		logger.V(1).Info("some API groups had discovery errors, continuing with available resources")
	}

	var resources []config.ResourceType
	seen := make(map[string]bool) // Deduplicate
	var deniedCount int

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
				deniedCount++
				logger.V(2).Info("skipping denied resource type",
					"kind", apiResource.Kind,
					"group", gv.Group,
					"version", gv.Version)
				continue
			}

			// Warn about potentially high-cardinality resource types that aren't in deny list
			if isHighCardinalityResource(apiResource.Kind) {
				logger.Info("WARNING: discovered potentially high-cardinality resource type",
					"kind", apiResource.Kind,
					"group", gv.Group,
					"version", gv.Version,
					"recommendation", "Consider adding to deny list if high volume is observed")
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

	logger.Info("resource discovery complete",
		"discovered", len(resources),
		"denied", deniedCount)

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
	"ReplicaSet":         true, // Usually managed by Deployment

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

	// Storage resources - usually shouldn't be mirrored
	"PersistentVolumeClaim": true,
	"VolumeSnapshot":        true,
	"VolumeSnapshotContent": true,

	// Longhorn resources - storage controller specific
	"Engine":                 true,
	"Replica":                true,
	"InstanceManager":        true,
	"ShareManager":           true,
	"BackingImageManager":    true,
	"BackingImageDataSource": true,
	"Orphan":                 true,
	"RecurringJob":           true,
	"EngineImage":            true,
	"BackingImage":           true,
	"BackupTarget":           true,
	"BackupVolume":           true,
	"Setting":                true,

	// ArgoCD/Argo resources - gitops/workflow specific
	"Application":            true,
	"ApplicationSet":         true,
	"AppProject":             true,
	"Workflow":               true,
	"WorkflowTemplate":       true,
	"CronWorkflow":           true,
	"EventSource":            true,
	"EventBus":               true,
	"Sensor":                 true,
	"AnalysisRun":            true,
	"AnalysisTemplate":       true,
	"Experiment":             true,
	"Rollout":                true,
	"WorkflowArtifactGCTask": true,
	"WorkflowEventBinding":   true,
	"WorkflowTaskResult":     true,
	"WorkflowTaskSet":        true,

	// Cert-manager resources - certificate operator specific
	"Certificate":        true,
	"CertificateRequest": true,
	"Issuer":             true,
	"ClusterIssuer":      true,

	// External Secrets resources - secrets operator specific
	"ExternalSecret":     true,
	"SecretStore":        true,
	"ClusterSecretStore": true,
	"PushSecret":         true,
	// Generator resources
	"ACRAccessToken":        true,
	"CloudsmithAccessToken": true,
	"ECRAuthorizationToken": true,
	"Fake":                  true,
	"GCRAccessToken":        true,
	"GeneratorState":        true,
	"GithubAccessToken":     true,
	"Grafana":               true,
	"MFA":                   true,
	"Password":              true,
	"QuayAccessToken":       true,
	"SSHKey":                true,
	"STSSessionToken":       true,
	"UUID":                  true,
	"VaultDynamicSecret":    true,
	"Webhook":               true,

	// Kyverno resources - policy operator specific
	"Policy":                          true,
	"ClusterPolicy":                   true,
	"PolicyException":                 true,
	"NamespacedDeletingPolicy":        true,
	"NamespacedImageValidatingPolicy": true,
	"NamespacedValidatingPolicy":      true,
	"CleanupPolicy":                   true,
	"AdmissionReport":                 true,
	"BackgroundScanReport":            true,
	"ClusterAdmissionReport":          true,
	"ClusterBackgroundScanReport":     true,
	"EphemeralReport":                 true,
	"PolicyReport":                    true,
	"UpdateRequest":                   true,

	// Cilium resources - networking operator specific
	"CiliumNetworkPolicy":            true,
	"CiliumClusterwideNetworkPolicy": true,
	"CiliumEndpoint":                 true,
	"CiliumIdentity":                 true,
	"CiliumNode":                     true,
	"CiliumExternalWorkload":         true,
	"CiliumLocalRedirectPolicy":      true,
	"CiliumEgressGatewayPolicy":      true,
	"CiliumGatewayClassConfig":       true,
	"CiliumNodeConfig":               true,
	"CiliumEnvoyConfig":              true,
	"CiliumClusterwideEnvoyConfig":   true,

	// Traefik Hub resources - API management specific
	"API":                 true,
	"APIAccess":           true,
	"APIAuth":             true,
	"APIBundle":           true,
	"APICatalogItem":      true,
	"APIPlan":             true,
	"APIPortal":           true,
	"APIPortalAuth":       true,
	"APIRateLimit":        true,
	"APIVersion":          true,
	"AIService":           true,
	"ManagedApplication":  true,
	"ManagedSubscription": true,

	// Kong resources - API gateway specific
	"KongConsumer":           true,
	"KongIngress":            true,
	"KongPlugin":             true,
	"KongClusterPlugin":      true,
	"KongUpstreamPolicy":     true,
	"KongConsumerGroup":      true,
	"TCPIngress":             true,
	"UDPIngress":             true,
	"IngressClassParameters": true,

	// System Upgrade Controller
	"Plan": true,

	// Tor operator resources
	"OnionService":         true,
	"OnionBalancedService": true,
	"Tor":                  true,

	// Gateway API resources - usually not mirrored
	"Gateway":          true,
	"GatewayClass":     true,
	"HTTPRoute":        true,
	"TLSRoute":         true,
	"TCPRoute":         true,
	"UDPRoute":         true,
	"GRPCRoute":        true,
	"ReferenceGrant":   true,
	"BackendTLSPolicy": true,

	// VictoriaMetrics operator resources
	"VMAgent":              true,
	"VMAlert":              true,
	"VMAlertmanager":       true,
	"VMAlertmanagerConfig": true,
	"VMAuth":               true,
	"VMCluster":            true,
	"VMNodeScrape":         true,
	"VMPodScrape":          true,
	"VMProbe":              true,
	"VMRule":               true,
	"VMServiceScrape":      true,
	"VMSingle":             true,
	"VMStaticScrape":       true,
	"VMScrapeConfig":       true,
	"VMUser":               true,
	"VMAnomaly":            true,

	// Jobs and workloads - usually shouldn't be mirrored
	"Job":     true,
	"CronJob": true}

func isDeniedResourceType(kind string) bool {
	return deniedKinds[kind]
}

// highCardinalityKinds are resource types that might generate high volumes of objects.
// These aren't denied by default but warrant monitoring when discovered.
var highCardinalityKinds = map[string]bool{
	// Resources that might have many instances per namespace
	"ServiceAccount":          true, // Often auto-created per deployment
	"Role":                    true, // Can be many per namespace
	"RoleBinding":             true, // Can be many per namespace
	"NetworkPolicy":           true, // Can be many per namespace
	"LimitRange":              true, // Usually few but triggers on all namespace changes
	"ResourceQuota":           true, // Usually few but triggers on all namespace changes
	"HorizontalPodAutoscaler": true, // One per deployment/statefulset

	// CRD resources that might have high cardinality
	"ServiceEntry":       true, // Istio - can have many
	"VirtualService":     true, // Istio - can have many
	"DestinationRule":    true, // Istio - can have many
	"EnvoyFilter":        true, // Istio - can have many
	"Sidecar":            true, // Istio - can have many
	"PeerAuthentication": true, // Istio - can have many

	// Prometheus-style monitoring resources
	"ServiceMonitor": true, // Often one per service
	"PodMonitor":     true, // Often one per pod type
	"PrometheusRule": true, // Can have many rules
}

// isHighCardinalityResource checks if a resource type might generate high volumes.
func isHighCardinalityResource(kind string) bool {
	return highCardinalityKinds[kind]
}
