// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/filter"
	"github.com/lukaszraczylo/kubemirror/pkg/hash"
)

// SourceReconciler reconciles source resources that need mirroring.
type SourceReconciler struct {
	client.Client
	NamespaceLister NamespaceLister
	APIReader       client.Reader
	Scheme          *runtime.Scheme
	Config          *config.Config
	Filter          *filter.NamespaceFilter
	GVK             schema.GroupVersionKind
}

// NamespaceLister provides a list of all namespaces in the cluster.
// This interface allows for testing with mocks.
type NamespaceLister interface {
	ListNamespaces(ctx context.Context) ([]string, error)
	ListAllowMirrorsNamespaces(ctx context.Context) ([]string, error)
	ListOptOutNamespaces(ctx context.Context) ([]string, error)
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch

// getSourceWithFreshness fetches a source resource with optional freshness verification.
// This implements a hybrid caching strategy:
// 1. First read from informer cache (fast, local)
// 2. If VerifySourceFreshness is enabled, make direct API call via APIReader
// 3. If resourceVersions differ, cache is stale - return fresh version from API
// 4. If resourceVersions match, cache is current - return cached version
//
// This prevents the race condition where:
// - Watch event arrives: "Secret changed!"
// - Reconciliation starts immediately
// - Cache hasn't updated yet (5-20 second lag)
// - We read stale data and mirror it
//
// Trade-off: 2x API calls when cache is stale, but guarantees data freshness.
func (r *SourceReconciler) getSourceWithFreshness(ctx context.Context, key client.ObjectKey, gvk schema.GroupVersionKind) (*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	// First try: Read from cache (fast)
	cached := &unstructured.Unstructured{}
	cached.SetGroupVersionKind(gvk)
	if err := r.Get(ctx, key, cached); err != nil {
		return nil, err
	}

	// If freshness verification is disabled, return cached version immediately
	if !r.Config.VerifySourceFreshness {
		logger.V(2).Info("using cached source (freshness check disabled)", "resourceVersion", cached.GetResourceVersion())
		return cached, nil
	}

	// If APIReader is not available (e.g., in tests), fall back to cached version
	if r.APIReader == nil {
		logger.V(2).Info("using cached source (no APIReader available)", "resourceVersion", cached.GetResourceVersion())
		return cached, nil
	}

	cachedRV := cached.GetResourceVersion()

	// Second try: Direct API read to verify freshness (bypasses cache)
	fresh := &unstructured.Unstructured{}
	fresh.SetGroupVersionKind(gvk)
	if err := r.APIReader.Get(ctx, key, fresh); err != nil {
		// If direct API read fails, fall back to cached version
		logger.V(1).Info("direct API read failed, using cached version", "error", err, "cachedRV", cachedRV)
		return cached, nil
	}

	freshRV := fresh.GetResourceVersion()

	// Compare resource versions
	if cachedRV != freshRV {
		// Cache is stale - return fresh version from API
		logger.V(1).Info("cache stale, using fresh API version",
			"cachedRV", cachedRV,
			"freshRV", freshRV)
		return fresh, nil
	}

	// Cache is current - return cached version (saves memory allocation)
	logger.V(2).Info("cache current", "resourceVersion", cachedRV)
	return cached, nil
}

// Reconcile processes a single source resource.
func (r *SourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"namespace", req.Namespace,
		"name", req.Name,
		"kind", r.GVK.Kind,
		"group", r.GVK.Group,
		"version", r.GVK.Version,
	)

	// Fetch the source resource with optional freshness verification
	source, err := r.getSourceWithFreshness(ctx, req.NamespacedName, r.GVK)
	if err != nil {
		if errors.IsNotFound(err) {
			// Resource deleted - nothing to do
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get resource")
		return ctrl.Result{}, err
	}

	sourceObj := source

	// Check if this is a mirror resource (shouldn't reconcile mirrors as sources)
	if IsMirrorResource(sourceObj) {
		// Silently skip - mirrors reconcile via watch, not as sources
		return ctrl.Result{}, nil
	}

	// Check if resource is enabled for mirroring
	// Check if resource is being deleted
	if !sourceObj.GetDeletionTimestamp().IsZero() {
		// Resource is being deleted - clean up mirrors and remove finalizer
		if slices.Contains(sourceObj.GetFinalizers(), constants.FinalizerName) {
			logger.Info("source being deleted, cleaning up all mirrors")
			deleteErr := r.deleteAllMirrors(ctx, sourceObj)
			if deleteErr != nil {
				logger.Error(deleteErr, "failed to delete all mirrors during source deletion")
				return ctrl.Result{}, deleteErr
			}

			// Remove finalizer to allow resource deletion
			logger.Info("removing finalizer from source resource")
			finalizers := removeString(sourceObj.GetFinalizers(), constants.FinalizerName)
			sourceObj.SetFinalizers(finalizers)
			updateErr := r.Update(ctx, source)
			if updateErr != nil {
				logger.Error(updateErr, "failed to remove finalizer")
				return ctrl.Result{}, updateErr
			}
			logger.Info("finalizer removed, resource can now be deleted")
		}
		return ctrl.Result{}, nil
	}

	if !isEnabledForMirroring(sourceObj) {
		// Resource is disabled - remove finalizer if present and delete all mirrors
		if slices.Contains(sourceObj.GetFinalizers(), constants.FinalizerName) {
			return r.handleDisabled(ctx, sourceObj)
		}
		// No finalizer, just skip
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !slices.Contains(sourceObj.GetFinalizers(), constants.FinalizerName) {
		logger.Info("adding finalizer to source resource")
		finalizers := append(sourceObj.GetFinalizers(), constants.FinalizerName)
		sourceObj.SetFinalizers(finalizers)
		addFinalizerErr := r.Update(ctx, source)
		if addFinalizerErr != nil {
			logger.Error(addFinalizerErr, "failed to add finalizer")
			return ctrl.Result{}, addFinalizerErr
		}
		logger.Info("finalizer added")
		// Requeue to continue with reconciliation after finalizer is added
		return ctrl.Result{Requeue: true}, nil
	}

	// Get target namespaces
	targetNamespaces, err := r.resolveTargetNamespaces(ctx, sourceObj)
	if err != nil {
		logger.Error(err, "failed to resolve target namespaces")
		return ctrl.Result{}, err
	}

	if len(targetNamespaces) == 0 {
		logger.V(1).Info("no target namespaces resolved")
		return ctrl.Result{}, nil
	}

	logger.V(1).Info("reconciling mirrors", "targetCount", len(targetNamespaces))

	// Reconcile each target namespace
	var reconciledCount, errorCount int
	for _, targetNs := range targetNamespaces {
		reconcileErr := r.reconcileMirror(ctx, source, sourceObj, targetNs)
		if reconcileErr != nil {
			logger.Error(reconcileErr, "failed to reconcile mirror", "targetNamespace", targetNs)
			errorCount++
		} else {
			reconciledCount++
		}
	}

	// Clean up orphaned mirrors (namespaces that no longer match the target criteria)
	orphanedCount, err := r.cleanupOrphanedMirrors(ctx, sourceObj, targetNamespaces)
	if err != nil {
		logger.Error(err, "failed to cleanup orphaned mirrors")
		// Don't fail reconciliation for cleanup errors, just log them
	} else if orphanedCount > 0 {
		logger.Info("cleaned up orphaned mirrors", "count", orphanedCount)
	}

	// Update status annotation with last sync info
	if err := r.updateLastSyncStatus(ctx, source, sourceObj, reconciledCount, errorCount); err != nil {
		logger.Error(err, "failed to update sync status")
		return ctrl.Result{}, err
	}

	logger.Info("reconciliation complete",
		"reconciled", reconciledCount,
		"errors", errorCount,
		"total", len(targetNamespaces))

	// Return error if there were errors (controller-runtime will automatically requeue with exponential backoff)
	if errorCount > 0 {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile %d/%d mirrors", errorCount, len(targetNamespaces))
	}

	return ctrl.Result{}, nil
}

// handleDisabled removes mirrors when a resource is disabled.
func (r *SourceReconciler) handleDisabled(ctx context.Context, sourceObj metav1.Object) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Delete all mirrors for this disabled source
	if err := r.deleteAllMirrors(ctx, sourceObj); err != nil {
		logger.Error(err, "failed to delete mirrors for disabled resource")
		return ctrl.Result{}, err
	}

	// Remove finalizer if present
	if slices.Contains(sourceObj.GetFinalizers(), constants.FinalizerName) {
		logger.Info("removing finalizer from disabled resource")
		finalizers := removeString(sourceObj.GetFinalizers(), constants.FinalizerName)
		sourceObj.SetFinalizers(finalizers)

		// Get the unstructured object to update - sourceObj is already *unstructured.Unstructured
		source := sourceObj.(*unstructured.Unstructured)
		if err := r.Update(ctx, source); err != nil {
			logger.Error(err, "failed to remove finalizer from disabled resource")
			return ctrl.Result{}, err
		}
		logger.V(1).Info("finalizer removed from disabled resource")
	}

	logger.V(1).Info("mirrors deleted for disabled resource")
	return ctrl.Result{}, nil
}

// reconcileMirror creates or updates a mirror in the target namespace.
func (r *SourceReconciler) reconcileMirror(ctx context.Context, source runtime.Object, sourceObj metav1.Object, targetNs string) error {
	logger := log.FromContext(ctx).WithValues("targetNamespace", targetNs)

	// Try to get existing mirror as unstructured
	sourceUnstructured := source.(*unstructured.Unstructured)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(sourceUnstructured.GroupVersionKind())

	err := r.Get(ctx, client.ObjectKey{Namespace: targetNs, Name: sourceObj.GetName()}, existing)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get existing mirror: %w", err)
	}

	// If freshness verification is enabled and mirror exists, verify it's fresh too
	if err == nil && r.Config.VerifySourceFreshness && r.APIReader != nil {
		fresh := &unstructured.Unstructured{}
		fresh.SetGroupVersionKind(sourceUnstructured.GroupVersionKind())
		if apiErr := r.APIReader.Get(ctx, client.ObjectKey{Namespace: targetNs, Name: sourceObj.GetName()}, fresh); apiErr == nil {
			if fresh.GetResourceVersion() != existing.GetResourceVersion() {
				logger.V(2).Info("mirror cache stale, using fresh API version",
					"cachedRV", existing.GetResourceVersion(),
					"freshRV", fresh.GetResourceVersion())
				existing = fresh
			}
		}
	}

	if err == nil {
		// Mirror exists - check if it's managed by us
		if !IsManagedByUs(existing) {
			logger.V(1).Info("target resource exists but not managed by kubemirror, skipping")
			return nil
		}

		// Check if update is needed
		needsSync, syncCheckErr := hash.NeedsSync(source, existing, existing.GetAnnotations())
		if syncCheckErr != nil {
			return fmt.Errorf("failed to check if sync needed: %w", syncCheckErr)
		}

		if !needsSync {
			logger.V(2).Info("mirror is up to date")
			return nil
		}

		// Update mirror
		updateErr := UpdateMirror(existing, source)
		if updateErr != nil {
			return fmt.Errorf("failed to update mirror: %w", updateErr)
		}

		clusterUpdateErr := r.Update(ctx, existing)
		if clusterUpdateErr != nil {
			return fmt.Errorf("failed to update mirror in cluster: %w", clusterUpdateErr)
		}

		logger.V(1).Info("mirror updated")
		return nil
	}

	// Create new mirror
	mirror, err := CreateMirror(source, targetNs)
	if err != nil {
		return fmt.Errorf("failed to create mirror: %w", err)
	}

	if err := r.Create(ctx, mirror.(client.Object)); err != nil {
		return fmt.Errorf("failed to create mirror in cluster: %w", err)
	}

	logger.V(1).Info("mirror created")
	return nil
}

// deleteAllMirrors deletes all mirrors for a source resource.
func (r *SourceReconciler) deleteAllMirrors(ctx context.Context, sourceObj metav1.Object) error {
	logger := log.FromContext(ctx)

	// List all namespaces
	allNamespaces, err := r.NamespaceLister.ListNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Get GVK from source object
	sourceUnstructured, ok := sourceObj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("source object is not unstructured")
	}

	var deleteCount int
	for _, ns := range allNamespaces {
		// Skip source namespace
		if ns == sourceObj.GetNamespace() {
			continue
		}

		// Create mirror reference for deletion
		mirror := &unstructured.Unstructured{}
		mirror.SetGroupVersionKind(sourceUnstructured.GroupVersionKind())
		mirror.SetNamespace(ns)
		mirror.SetName(sourceObj.GetName())

		err := r.Delete(ctx, mirror)
		if err == nil {
			deleteCount++
		} else if !errors.IsNotFound(err) {
			logger.Error(err, "failed to delete mirror", "namespace", ns)
		}
	}

	logger.Info("deleted mirrors", "count", deleteCount)
	return nil
}

// cleanupOrphanedMirrors removes mirrors that exist but are no longer in the target list.
// This handles cases where target-namespaces annotation changes (e.g., "all" → "all-labeled" or "app-*" → "prod-*").
func (r *SourceReconciler) cleanupOrphanedMirrors(ctx context.Context, sourceObj metav1.Object, targetNamespaces []string) (int, error) {
	logger := log.FromContext(ctx)

	// List all namespaces
	allNamespaces, err := r.NamespaceLister.ListNamespaces(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Get GVK from source object
	sourceUnstructured, ok := sourceObj.(*unstructured.Unstructured)
	if !ok {
		return 0, fmt.Errorf("source object is not unstructured")
	}

	// Create a set of target namespaces for quick lookup
	targetSet := make(map[string]bool)
	for _, ns := range targetNamespaces {
		targetSet[ns] = true
	}

	var deletedCount int
	for _, ns := range allNamespaces {
		// Skip source namespace
		if ns == sourceObj.GetNamespace() {
			continue
		}

		// Skip if this namespace IS in the current target list
		if targetSet[ns] {
			continue
		}

		// Check if a mirror exists in this namespace
		mirror := &unstructured.Unstructured{}
		mirror.SetGroupVersionKind(sourceUnstructured.GroupVersionKind())
		mirror.SetNamespace(ns)
		mirror.SetName(sourceObj.GetName())

		err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: sourceObj.GetName()}, mirror)
		if errors.IsNotFound(err) {
			// No mirror exists, nothing to clean up
			continue
		}
		if err != nil {
			logger.Error(err, "failed to check for mirror", "namespace", ns)
			continue
		}

		// Verify this is actually our mirror (not someone else's resource with the same name)
		if !IsManagedByUs(mirror) {
			continue
		}

		// Verify this mirror points to our source
		srcNs, srcName, _, found := GetSourceReference(mirror)
		if !found || srcNs != sourceObj.GetNamespace() || srcName != sourceObj.GetName() {
			continue
		}

		// This is an orphaned mirror - delete it
		if err := r.Delete(ctx, mirror); err != nil {
			logger.Error(err, "failed to delete orphaned mirror", "namespace", ns)
			continue
		}

		deletedCount++
		logger.V(1).Info("deleted orphaned mirror", "namespace", ns)
	}

	return deletedCount, nil
}

// resolveTargetNamespaces determines which namespaces should receive mirrors.
func (r *SourceReconciler) resolveTargetNamespaces(ctx context.Context, sourceObj metav1.Object) ([]string, error) {
	annotations := sourceObj.GetAnnotations()
	if annotations == nil {
		return nil, nil
	}

	targetNsAnnotation := annotations[constants.AnnotationTargetNamespaces]
	if targetNsAnnotation == "" {
		return nil, nil
	}

	// Parse patterns
	patterns := filter.ParseTargetNamespaces(targetNsAnnotation)
	if len(patterns) == 0 {
		return nil, nil
	}

	// Validate patterns and log warnings for invalid ones
	validationResults, allValid := filter.ValidatePatterns(patterns)
	if !allValid {
		logger := log.FromContext(ctx)
		invalidPatterns := filter.InvalidPatterns(validationResults)
		for _, invalid := range invalidPatterns {
			logger.Info("invalid glob pattern in target-namespaces annotation, pattern will be skipped",
				"pattern", invalid.Pattern,
				"error", invalid.Error.Error(),
				"source", sourceObj.GetName(),
				"namespace", sourceObj.GetNamespace(),
			)
		}

		// Filter to only valid patterns
		var validPatterns []string
		for _, result := range validationResults {
			if result.Valid {
				validPatterns = append(validPatterns, result.Pattern)
			}
		}
		patterns = validPatterns

		// If no valid patterns remain, return empty
		if len(patterns) == 0 {
			return nil, nil
		}
	}

	// Get all namespaces
	allNamespaces, err := r.NamespaceLister.ListNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Get namespaces with allow-mirrors label
	allowMirrorsNamespaces, err := r.NamespaceLister.ListAllowMirrorsNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list allow-mirrors namespaces: %w", err)
	}

	// Get namespaces that have explicitly opted out (allow-mirrors="false")
	optOutNamespaces, err := r.NamespaceLister.ListOptOutNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list opt-out namespaces: %w", err)
	}

	// Resolve target namespaces
	targetNamespaces := filter.ResolveTargetNamespaces(
		patterns,
		allNamespaces,
		allowMirrorsNamespaces,
		optOutNamespaces,
		sourceObj.GetNamespace(),
		r.Filter,
	)

	// Enforce max targets limit
	if r.Config != nil && r.Config.MaxTargetsPerResource > 0 && len(targetNamespaces) > r.Config.MaxTargetsPerResource {
		targetNamespaces = targetNamespaces[:r.Config.MaxTargetsPerResource]
	}

	return targetNamespaces, nil
}

// updateLastSyncStatus updates the source resource's annotations with sync status.
func (r *SourceReconciler) updateLastSyncStatus(ctx context.Context, source runtime.Object, sourceObj metav1.Object, reconciledCount, errorCount int) error {
	annotations := sourceObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[constants.AnnotationSyncStatus] = fmt.Sprintf("reconciled:%d,errors:%d", reconciledCount, errorCount)

	sourceObj.SetAnnotations(annotations)
	// source (*unstructured.Unstructured) already implements client.Object
	return r.Update(ctx, source.(*unstructured.Unstructured))
}

// isEnabledForMirroring checks if a resource has both the label and annotation for mirroring.
func isEnabledForMirroring(obj metav1.Object) bool {
	// Check label
	labels := obj.GetLabels()
	if labels == nil || labels[constants.LabelEnabled] != "true" {
		return false
	}

	// Check annotation
	annotations := obj.GetAnnotations()
	if annotations == nil || annotations[constants.AnnotationSync] != "true" {
		return false
	}

	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *SourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Build predicate to only watch resources with enabled label
	// This reduces API server load by ~90%
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Complete(r)
}

// SetupWithManagerForResourceType sets up a controller for a specific resource type.
// This allows dynamic controller registration for any discovered resource type.
func (r *SourceReconciler) SetupWithManagerForResourceType(
	mgr ctrl.Manager,
	gvk schema.GroupVersionKind,
) error {
	// Create an unstructured object for this GVK
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	// Create unique controller name including version and group to avoid collisions
	// e.g., "HorizontalPodAutoscaler.v1.autoscaling" or "Secret.v1." (empty group for core resources)
	// This matches the naming convention used by mirror reconcilers
	controllerName := gvk.Kind + "." + gvk.Version + "." + gvk.Group

	// Create mirror object for watching
	mirrorObj := &unstructured.Unstructured{}
	mirrorObj.SetGroupVersionKind(gvk)

	// Create predicates to only watch mirror deletions
	mirrorDeletePredicate := predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return false },
		UpdateFunc:  func(e event.UpdateEvent) bool { return false },
		DeleteFunc:  func(e event.DeleteEvent) bool { return IsMirrorResource(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Named(controllerName).
		// Watch mirror resources - when deleted, enqueue source for reconciliation
		Watches(
			mirrorObj,
			handler.EnqueueRequestsFromMapFunc(r.mapMirrorToSource),
			builder.WithPredicates(mirrorDeletePredicate),
		).
		Complete(r)
}

// mapMirrorToSource maps a mirror resource to its source for reconciliation.
func (r *SourceReconciler) mapMirrorToSource(ctx context.Context, obj client.Object) []reconcile.Request {
	// Only process if this is a mirror
	if !IsMirrorResource(obj) {
		return nil
	}

	// Get source reference from annotations
	sourceNs, sourceName, _, found := GetSourceReference(obj)
	if !found {
		return nil
	}

	// Enqueue reconciliation request for the source
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: sourceNs,
				Name:      sourceName,
			},
		},
	}
}

// removeString removes a string from a slice.
func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
