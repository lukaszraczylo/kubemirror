// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/filter"
)

const (
	// cacheSettleDelay is the time to wait after namespace label changes
	// to allow informer caches to sync. This addresses the race condition
	// where namespace watch events fire before the cache is updated.
	cacheSettleDelay = 3 * time.Second
)

// NamespaceReconciler watches for namespace CREATE and UPDATE events
// and triggers reconciliation of source resources that match the new namespace.
type NamespaceReconciler struct {
	client.Client
	NamespaceLister NamespaceLister
	APIReader       client.Reader
	Scheme          *runtime.Scheme
	Config          *config.Config
	Filter          *filter.NamespaceFilter
	ResourceTypes   []config.ResourceType
}

// Reconcile processes namespace events and creates mirrors for matching sources.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"namespace", req.Name,
		"reconciler", "namespace",
	)

	// Fetch the namespace
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, namespace); err != nil {
		// Namespace was deleted - nothing to do (source reconcilers will handle cleanup)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip system namespaces
	if r.Filter != nil && !r.Filter.IsAllowed(namespace.Name) {
		logger.V(1).Info("namespace filtered out, skipping")
		return ctrl.Result{}, nil
	}

	logger.Info("namespace event detected, reconciling source resources")

	// Query all source resources that have mirroring enabled
	// For each resource type, find resources with the sync annotation
	var totalReconciled, totalErrors int

	for _, rt := range r.ResourceTypes {
		reconciled, errors, err := r.reconcileResourceType(ctx, rt, namespace.Name)
		if err != nil {
			logger.Error(err, "failed to reconcile resource type",
				"group", rt.Group, "version", rt.Version, "kind", rt.Kind)
			totalErrors++
			continue
		}
		totalReconciled += reconciled
		totalErrors += errors
	}

	logger.Info("namespace reconciliation complete",
		"reconciled", totalReconciled,
		"errors", totalErrors,
		"resourceTypes", len(r.ResourceTypes))

	if totalErrors > 0 {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile %d source resources", totalErrors)
	}

	// Requeue with delay to catch any updates missed due to cache staleness.
	// This is particularly important for namespace label changes where the
	// informer cache may not yet reflect the new label state. The delay allows
	// the cache to settle and ensures all relevant source resources are reconciled.
	return ctrl.Result{RequeueAfter: cacheSettleDelay}, nil
}

// reconcileResourceType finds and reconciles all sources of a specific resource type
// that match the namespace.
func (r *NamespaceReconciler) reconcileResourceType(ctx context.Context, rt config.ResourceType, namespaceName string) (int, int, error) {
	logger := log.FromContext(ctx)

	gvk := rt.GroupVersionKind()

	// List all resources of this type with the enabled label
	// Using label selector for server-side filtering
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	listOpts := []client.ListOption{
		client.HasLabels{constants.LabelEnabled},
	}

	if err := r.List(ctx, list, listOpts...); err != nil {
		return 0, 0, fmt.Errorf("failed to list resources: %w", err)
	}

	var reconciledCount, errorCount int

	for i := range list.Items {
		source := &list.Items[i]

		// Check if source has sync annotation
		annotations := source.GetAnnotations()
		if annotations == nil || annotations[constants.AnnotationSync] != "true" {
			continue
		}

		// Skip if this is a mirror resource itself
		if IsMirrorResource(source) {
			continue
		}

		// Resolve target namespaces for this source
		targetNamespaces, err := r.resolveTargetNamespaces(ctx, source)
		if err != nil {
			logger.Error(err, "failed to resolve target namespaces",
				"source", source.GetName(), "namespace", source.GetNamespace())
			errorCount++
			continue
		}

		// Check if the new namespace matches this source's targets
		isTarget := slices.Contains(targetNamespaces, namespaceName)

		if isTarget {
			// Create or update mirror in the namespace
			if err := r.reconcileMirror(ctx, source, namespaceName); err != nil {
				logger.Error(err, "failed to create mirror",
					"source", source.GetName(),
					"sourceNamespace", source.GetNamespace(),
					"targetNamespace", namespaceName)
				errorCount++
				continue
			}

			reconciledCount++
			logger.V(1).Info("mirror created/updated for namespace",
				"source", source.GetName(),
				"sourceNamespace", source.GetNamespace(),
				"targetNamespace", namespaceName,
				"resourceType", rt.String())
		} else {
			// Namespace is no longer a target - check if mirror exists and delete it
			mirror := &unstructured.Unstructured{}
			mirror.SetGroupVersionKind(source.GroupVersionKind())
			mirror.SetNamespace(namespaceName)
			mirror.SetName(source.GetName())

			err := r.Get(ctx, client.ObjectKey{Namespace: namespaceName, Name: source.GetName()}, mirror)
			if errors.IsNotFound(err) {
				// No mirror exists, nothing to clean up
				continue
			}
			if err != nil {
				logger.Error(err, "failed to check for mirror",
					"source", source.GetName(),
					"namespace", namespaceName)
				errorCount++
				continue
			}

			// Verify this is actually our mirror (not someone else's resource with the same name)
			if !IsManagedByUs(mirror) {
				continue
			}

			// Verify this mirror points to our source
			srcNs, srcName, _, found := GetSourceReference(mirror)
			if !found || srcNs != source.GetNamespace() || srcName != source.GetName() {
				continue
			}

			// This mirror should be deleted (namespace no longer a valid target)
			if err := r.Delete(ctx, mirror); err != nil {
				logger.Error(err, "failed to delete orphaned mirror",
					"source", source.GetName(),
					"sourceNamespace", source.GetNamespace(),
					"targetNamespace", namespaceName)
				errorCount++
				continue
			}

			reconciledCount++
			logger.V(1).Info("deleted orphaned mirror due to namespace label change",
				"source", source.GetName(),
				"sourceNamespace", source.GetNamespace(),
				"targetNamespace", namespaceName,
				"resourceType", rt.String())
		}
	}

	return reconciledCount, errorCount, nil
}

// resolveTargetNamespaces determines which namespaces should receive mirrors for a source.
// Uses the same logic as SourceReconciler.resolveTargetNamespaces.
func (r *NamespaceReconciler) resolveTargetNamespaces(ctx context.Context, source *unstructured.Unstructured) ([]string, error) {
	annotations := source.GetAnnotations()
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
				"source", source.GetName(),
				"namespace", source.GetNamespace(),
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

	// Get all namespace info in a single API call (more efficient than 3 separate calls)
	nsInfo, err := r.NamespaceLister.ListNamespacesWithLabels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Resolve target namespaces using the pre-categorized namespace info
	targetNamespaces := filter.ResolveTargetNamespaces(
		patterns,
		nsInfo.All,
		nsInfo.AllowMirrors,
		nsInfo.OptOut,
		source.GetNamespace(),
		r.Filter,
	)

	// Enforce max targets limit
	if r.Config != nil && r.Config.MaxTargetsPerResource > 0 && len(targetNamespaces) > r.Config.MaxTargetsPerResource {
		targetNamespaces = targetNamespaces[:r.Config.MaxTargetsPerResource]
	}

	return targetNamespaces, nil
}

// reconcileMirror creates or updates a mirror in the target namespace.
// This calls the mirror creation logic from the SourceReconciler.
func (r *NamespaceReconciler) reconcileMirror(ctx context.Context, source *unstructured.Unstructured, targetNamespace string) error {
	// Create a temporary SourceReconciler to use its mirror creation logic
	// This avoids code duplication
	sourceReconciler := &SourceReconciler{
		Client:          r.Client,
		Scheme:          r.Scheme,
		Config:          r.Config,
		Filter:          r.Filter,
		NamespaceLister: r.NamespaceLister,
		GVK:             source.GroupVersionKind(),
	}

	return sourceReconciler.reconcileMirror(ctx, source, source, targetNamespace)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create predicate to only watch for relevant namespace events
	namespacePredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Always reconcile new namespaces
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Only reconcile if labels changed (specifically allow-mirrors label)
			oldNs, okOld := e.ObjectOld.(*corev1.Namespace)
			newNs, okNew := e.ObjectNew.(*corev1.Namespace)
			if !okOld || !okNew {
				return false
			}

			// Check if allow-mirrors label changed
			// Use GetLabels() to safely handle nil labels map
			oldLabels := oldNs.GetLabels()
			newLabels := newNs.GetLabels()

			// Get label values with nil-safe access
			var oldLabel, newLabel string
			if oldLabels != nil {
				oldLabel = oldLabels[constants.LabelAllowMirrors]
			}
			if newLabels != nil {
				newLabel = newLabels[constants.LabelAllowMirrors]
			}

			return oldLabel != newLabel
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Don't reconcile on delete - source reconcilers will handle cleanup via finalizers
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		WithEventFilter(namespacePredicate).
		Complete(r)
}
