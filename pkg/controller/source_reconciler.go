// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"context"
	"fmt"

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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	Scheme          *runtime.Scheme
	Config          *config.Config
	Filter          *filter.NamespaceFilter
	NamespaceLister NamespaceLister
	GVK             schema.GroupVersionKind // The resource type this reconciler handles
}

// NamespaceLister provides a list of all namespaces in the cluster.
// This interface allows for testing with mocks.
type NamespaceLister interface {
	ListNamespaces(ctx context.Context) ([]string, error)
	ListAllowMirrorsNamespaces(ctx context.Context) ([]string, error)
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch

// Reconcile processes a single source resource.
func (r *SourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "name", req.Name)

	// Fetch the source resource as unstructured (works for all resource types)
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(r.GVK) // Set the GVK so the client knows what to fetch
	if err := r.Get(ctx, req.NamespacedName, source); err != nil {
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
	if !isEnabledForMirroring(sourceObj) {
		// Silently skip - don't log as it would be too noisy
		return r.handleDisabled(ctx, sourceObj)
	}

	// Handle deletion
	if !sourceObj.GetDeletionTimestamp().IsZero() {
		return r.handleDeletion(ctx, source, sourceObj)
	}

	// Add finalizer if not present
	// source (*unstructured.Unstructured) already implements client.Object
	if !controllerutil.ContainsFinalizer(source, constants.FinalizerName) {
		controllerutil.AddFinalizer(source, constants.FinalizerName)
		if err := r.Update(ctx, source); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		logger.V(1).Info("added finalizer")
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
		if err := r.reconcileMirror(ctx, source, sourceObj, targetNs); err != nil {
			logger.Error(err, "failed to reconcile mirror", "targetNamespace", targetNs)
			errorCount++
		} else {
			reconciledCount++
		}
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

// handleDeletion removes finalizer after cleaning up all mirrors.
func (r *SourceReconciler) handleDeletion(ctx context.Context, source runtime.Object, sourceObj metav1.Object) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// source (*unstructured.Unstructured) already implements client.Object
	sourceUnstructured := source.(*unstructured.Unstructured)
	if !controllerutil.ContainsFinalizer(sourceUnstructured, constants.FinalizerName) {
		return ctrl.Result{}, nil
	}

	// Delete all mirrors
	if err := r.deleteAllMirrors(ctx, sourceObj); err != nil {
		logger.Error(err, "failed to delete mirrors")
		return ctrl.Result{}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(sourceUnstructured, constants.FinalizerName)
	if err := r.Update(ctx, sourceUnstructured); err != nil {
		logger.Error(err, "failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("finalizer removed, mirrors deleted")
	return ctrl.Result{}, nil
}

// handleDisabled removes mirrors when a resource is disabled.
func (r *SourceReconciler) handleDisabled(ctx context.Context, sourceObj metav1.Object) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Source is already a client.Object (unstructured implements it)
	sourceClient := sourceObj.(client.Object)

	// If resource has finalizer, clean up mirrors and remove it
	if controllerutil.ContainsFinalizer(sourceClient, constants.FinalizerName) {
		if err := r.deleteAllMirrors(ctx, sourceObj); err != nil {
			logger.Error(err, "failed to delete mirrors for disabled resource")
			return ctrl.Result{}, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(sourceClient, constants.FinalizerName)
		if err := r.Update(ctx, sourceClient); err != nil {
			logger.Error(err, "failed to remove finalizer from disabled resource")
			return ctrl.Result{}, err
		}

		logger.Info("mirrors deleted and finalizer removed for disabled resource")
	}

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

	if err == nil {
		// Mirror exists - check if it's managed by us
		if !IsManagedByUs(existing) {
			logger.Info("target resource exists but not managed by kubemirror, skipping")
			return nil
		}

		// Check if update is needed
		needsSync, err := hash.NeedsSync(source, existing, existing.GetAnnotations())
		if err != nil {
			return fmt.Errorf("failed to check if sync needed: %w", err)
		}

		if !needsSync {
			logger.V(1).Info("mirror is up to date")
			return nil
		}

		// Update mirror
		if err := UpdateMirror(existing, source); err != nil {
			return fmt.Errorf("failed to update mirror: %w", err)
		}

		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update mirror in cluster: %w", err)
		}

		logger.Info("mirror updated")
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

	logger.Info("mirror created")
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

	// Resolve target namespaces
	targetNamespaces := filter.ResolveTargetNamespaces(
		patterns,
		allNamespaces,
		allowMirrorsNamespaces,
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

	// Create unique controller name including version to avoid collisions
	// e.g., "HorizontalPodAutoscaler.v1.autoscaling"
	controllerName := gvk.Kind + "." + gvk.Version
	if gvk.Group != "" {
		controllerName += "." + gvk.Group
	}

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
