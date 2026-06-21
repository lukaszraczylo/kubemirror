// Package constants defines all annotation keys, label keys, and constant values
// used by the kubemirror controller.
//
// # Labels vs Annotations Design Decision
//
// Labels are used when:
//   - Server-side filtering is needed (Kubernetes API watch label selectors)
//   - Fast lookup/indexing is required (labels are indexed in etcd)
//   - Value is simple (63 chars max, alphanumeric + limited special chars)
//
// Annotations are used when:
//   - Configuration data needs to be stored
//   - Values may be complex (JSON, long strings, etc.)
//   - Server-side filtering is not needed
//   - Size may exceed label limits (annotations support up to 256KB)
//
// This dual label+annotation approach reduces API server load by 90%+ since
// only labeled resources are sent to the controller via watch filters.
package constants

const (
	// Domain is the base domain for all kubemirror annotations and labels
	Domain = "kubemirror.raczylo.com"

	// ====================
	// LABELS
	// ====================
	// Labels enable server-side filtering and must follow Kubernetes naming rules:
	// - 63 chars max
	// - alphanumeric, '-', '_', '.'
	// - must start and end with alphanumeric

	// LabelEnabled is the primary label for server-side filtering.
	// Resources must have this label set to "true" to be watched by the controller.
	// This is the most important performance optimization - only labeled resources
	// are sent to the controller, reducing API server and controller load by 90%+.
	// REQUIRED on source resources for mirroring.
	LabelEnabled = Domain + "/enabled"

	// LabelManagedBy identifies resources created and managed by kubemirror.
	// Used for server-side filtering when finding mirrors to reconcile.
	// Value: "kubemirror"
	LabelManagedBy = Domain + "/managed-by"

	// LabelMirror marks a resource as a mirror (target resource, not source).
	// Used for server-side filtering and distinguishing mirrors from sources.
	// Value: "true"
	LabelMirror = Domain + "/mirror"

	// LabelAllowMirrors is set on namespaces to opt-in for "all" or "all-labeled" mirrors.
	// Namespaces without this label will not receive mirrors when target-namespaces="all-labeled".
	// Value: "true"
	LabelAllowMirrors = Domain + "/allow-mirrors"

	// ====================
	// ANNOTATIONS
	// ====================
	// Annotations store configuration and tracking data. They support larger values
	// and complex data (JSON, lists, etc.) but cannot be used for server-side filtering.

	// --- Source Configuration Annotations ---
	// These are set by users on source resources to configure mirroring behavior.

	// AnnotationSync marks a resource for mirroring when set to "true".
	// Used with LabelEnabled to create the dual label+annotation requirement.
	// Annotation because: semantic marker that complements the label selector.
	AnnotationSync = Domain + "/sync"

	// AnnotationTargetNamespaces specifies target namespaces.
	// Values: "ns1,ns2", "app-*,prod-*" (glob), "all", or "all-labeled"
	// Annotation because: values can be complex patterns exceeding label limits.
	AnnotationTargetNamespaces = Domain + "/target-namespaces"

	// AnnotationExclude opts a resource out of mirroring when "true", overriding
	// the enabled label and sync annotation. Any mirrors it previously created are
	// removed. Use AnnotationPaused instead to freeze mirrors without deleting them.
	// Annotation because: used for configuration, not filtering.
	AnnotationExclude = Domain + "/exclude"

	// AnnotationPaused freezes a source's mirrors when "true": existing mirrors
	// are left untouched (no updates, no orphan cleanup) until the annotation is
	// removed. Unlike AnnotationExclude, pausing does not delete existing mirrors.
	// Annotation because: operational control, not used for filtering.
	AnnotationPaused = Domain + "/paused"

	// --- Mirror Ownership Annotations ---
	// These are set by kubemirror on mirror resources to track their source.
	// All are annotations because they store tracking data, not used for filtering.

	// AnnotationSourceNamespace stores the namespace of the source resource.
	AnnotationSourceNamespace = Domain + "/source-namespace"

	// AnnotationSourceName stores the name of the source resource.
	AnnotationSourceName = Domain + "/source-name"

	// AnnotationSourceUID stores the UID of the source resource.
	// Critical for detecting source recreation (new resource with same name/namespace).
	AnnotationSourceUID = Domain + "/source-uid"

	// AnnotationSourceGeneration stores the generation of the source when last synced.
	AnnotationSourceGeneration = Domain + "/source-generation"

	// AnnotationSourceContentHash stores the content hash of the source when last synced.
	// Compared against source's current hash to detect changes.
	AnnotationSourceContentHash = Domain + "/source-content-hash"

	// AnnotationSourceResourceVersion stores the resourceVersion for debugging.
	AnnotationSourceResourceVersion = Domain + "/source-resource-version"

	// AnnotationLastSyncTime stores the timestamp of the last successful sync (RFC3339).
	AnnotationLastSyncTime = Domain + "/last-sync-time"

	// --- Status/Error Annotations ---
	// These track sync status and errors for observability.

	// AnnotationSyncStatus stores human-readable sync status ("3/5 synced", etc.).
	AnnotationSyncStatus = Domain + "/sync-status"

	// --- Transformation Annotations ---
	// These configure resource transformation during mirroring.

	// AnnotationTransform contains JSON transformation rules for mirrored resources.
	// Annotation because: complex JSON data, can be large.
	AnnotationTransform = Domain + "/transform"

	// AnnotationTransformStrict enables strict mode when "true".
	// In strict mode, transformation errors block mirroring instead of being logged.
	AnnotationTransformStrict = Domain + "/transform-strict"

	// Finalizers

	// FinalizerName is the finalizer added to source resources.
	FinalizerName = Domain + "/finalizer"

	// Controller Configuration

	// ControllerName is the name of the controller (for field manager, metrics, etc.).
	ControllerName = "kubemirror"

	// LeaderElectionID is the name of the leader election lease.
	LeaderElectionID = "kubemirror-controller-leader"

	// Special Values

	// TargetNamespacesAll is the special keyword for mirroring to all namespaces.
	TargetNamespacesAll = "all"

	// TargetNamespacesAllLabeled mirrors to namespaces with allow-mirrors label.
	TargetNamespacesAllLabeled = "all-labeled"
)

// Default System Namespaces (excluded by default)
var (
	DefaultExcludedNamespaces = []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
	}

	// Blacklisted Secret Types (never mirrored)
	BlacklistedSecretTypes = []string{
		"kubernetes.io/service-account-token",
		"bootstrap.kubernetes.io/token",
		"helm.sh/release.v1",
	}
)
