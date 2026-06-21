// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/lukaszraczylo/kubemirror/pkg/circuitbreaker"
	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/filter"
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
	CircuitBreaker  *circuitbreaker.CircuitBreaker
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

	// Don't requeue. The previous unconditional RequeueAfter caused every
	// namespace in the cluster to re-reconcile every 3 seconds forever,
	// generating constant API-server pressure scaled by namespace count.
	// Cache-staleness windows after label changes are handled by:
	//  - the manager's resync period (default 10m), which re-fires events,
	//  - source freshness verification (--verify-source-freshness, default on)
	//    in the SourceReconciler path,
	//  - and the next genuine namespace event.
	return ctrl.Result{}, nil
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

		// Skip excluded or paused sources. Excluded resources are torn down by the
		// source reconciler; paused resources must stay frozen. In both cases the
		// namespace reconciler must not create, update, or delete their mirrors.
		if annotations[constants.AnnotationExclude] == "true" || isPaused(source) {
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
			// Namespace is no longer a target - delete the mirror if we own it.
			outcome, err := deleteOwnedMirror(ctx, r.Client, source.GroupVersionKind(),
				namespaceName, source.GetName(), source.GetNamespace(), source.GetName())
			if err != nil {
				logger.Error(err, "failed to delete orphaned mirror",
					"source", source.GetName(),
					"sourceNamespace", source.GetNamespace(),
					"targetNamespace", namespaceName)
				errorCount++
				continue
			}
			if outcome == mirrorDeleted {
				reconciledCount++
				logger.V(1).Info("deleted orphaned mirror due to namespace label change",
					"source", source.GetName(),
					"sourceNamespace", source.GetNamespace(),
					"targetNamespace", namespaceName,
					"resourceType", rt.String())
			}
		}
	}

	return reconciledCount, errorCount, nil
}

// resolveTargetNamespaces determines which namespaces should receive mirrors for
// a source. It delegates to SourceReconciler so target-resolution logic
// (pattern parsing/validation, namespace listing, max-targets clamping) lives in
// exactly one place, mirroring the reconcileMirror delegation below.
func (r *NamespaceReconciler) resolveTargetNamespaces(ctx context.Context, source *unstructured.Unstructured) ([]string, error) {
	return r.newSourceReconciler(source.GroupVersionKind()).
		resolveTargetNamespaces(ctx, source)
}

// reconcileMirror creates or updates a mirror in the target namespace by
// delegating to SourceReconciler.reconcileMirror so all freshness, ownership,
// and circuit-breaker behavior stays in one place.
func (r *NamespaceReconciler) reconcileMirror(ctx context.Context, source *unstructured.Unstructured, targetNamespace string) error {
	return r.newSourceReconciler(source.GroupVersionKind()).
		reconcileMirror(ctx, source, source, targetNamespace)
}

// newSourceReconciler builds an ad-hoc SourceReconciler for delegating mirror
// reconciliation. APIReader and CircuitBreaker are forwarded so namespace-driven
// mirror creates/updates use the same freshness checks and failure throttling
// as direct source reconciles. Without this, namespace label changes would
// silently bypass --verify-source-freshness and the per-resource circuit breaker.
func (r *NamespaceReconciler) newSourceReconciler(gvk schema.GroupVersionKind) *SourceReconciler {
	return &SourceReconciler{
		Client:          r.Client,
		Scheme:          r.Scheme,
		Config:          r.Config,
		Filter:          r.Filter,
		NamespaceLister: r.NamespaceLister,
		GVK:             gvk,
		APIReader:       r.APIReader,
		CircuitBreaker:  r.CircuitBreaker,
	}
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
