// Package constants defines all annotation keys, label keys, and constant values
// used by the kubemirror controller.
package constants

const (
	// Domain is the base domain for all kubemirror annotations and labels
	Domain = "kubemirror.raczylo.com"

	// Labels

	// LabelEnabled is the label used for server-side filtering in watches.
	// Resources must have this label set to "true" to be processed by the controller.
	LabelEnabled = Domain + "/enabled"

	// LabelManagedBy identifies resources managed by kubemirror.
	LabelManagedBy = Domain + "/managed-by"

	// LabelMirror marks a resource as a mirror (target resource).
	LabelMirror = Domain + "/mirror"

	// LabelAllowMirrors is set on namespaces to opt-in for "all" mirrors.
	LabelAllowMirrors = Domain + "/allow-mirrors"

	// Annotations

	// AnnotationSync marks a resource for mirroring when set to "true".
	AnnotationSync = Domain + "/sync"

	// AnnotationTargetNamespaces specifies target namespaces (comma-separated or "all").
	AnnotationTargetNamespaces = Domain + "/target-namespaces"

	// AnnotationExclude explicitly excludes a resource from mirroring.
	AnnotationExclude = Domain + "/exclude"

	// AnnotationMaxTargets overrides the default maximum target limit per resource.
	AnnotationMaxTargets = Domain + "/max-targets"

	// AnnotationRecreateOnImmutableChange controls whether to delete/recreate on immutable field changes.
	AnnotationRecreateOnImmutableChange = Domain + "/recreate-on-immutable-change"

	// AnnotationPaused on controller deployment pauses all reconciliation.
	AnnotationPaused = Domain + "/paused"

	// Source Resource Annotations (tracking)

	// AnnotationContentHash stores the SHA256 hash of the source resource content.
	AnnotationContentHash = Domain + "/content-hash"

	// Target Resource Annotations (ownership and tracking)

	// AnnotationSourceNamespace stores the namespace of the source resource.
	AnnotationSourceNamespace = Domain + "/source-namespace"

	// AnnotationSourceName stores the name of the source resource.
	AnnotationSourceName = Domain + "/source-name"

	// AnnotationSourceUID stores the UID of the source resource.
	AnnotationSourceUID = Domain + "/source-uid"

	// AnnotationSourceGeneration stores the generation of the source when last synced.
	AnnotationSourceGeneration = Domain + "/source-generation"

	// AnnotationSourceContentHash stores the content hash of the source when last synced.
	AnnotationSourceContentHash = Domain + "/source-content-hash"

	// AnnotationSourceResourceVersion stores the resourceVersion for debugging.
	AnnotationSourceResourceVersion = Domain + "/source-resource-version"

	// AnnotationLastSyncTime stores the timestamp of the last successful sync.
	AnnotationLastSyncTime = Domain + "/last-sync-time"

	// AnnotationSyncStatus stores the sync status ("3/5 synced", etc.).
	AnnotationSyncStatus = Domain + "/sync-status"

	// AnnotationFailedTargets stores comma-separated list of failed target namespaces.
	AnnotationFailedTargets = Domain + "/failed-targets"

	// AnnotationWebhookError stores webhook rejection error message.
	AnnotationWebhookError = Domain + "/webhook-error"

	// AnnotationTargetNamespaceUID tracks the UID of the target namespace.
	AnnotationTargetNamespaceUID = Domain + "/target-namespace-uid"

	// AnnotationDeletionAttempts tracks number of failed deletion attempts.
	AnnotationDeletionAttempts = Domain + "/deletion-attempts"

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

	// Default Denied Resource Types
	DefaultDeniedResourceTypes = []string{
		"events",
		"pods",
		"replicasets",
		"endpoints",
		"endpointslices",
	}
)
