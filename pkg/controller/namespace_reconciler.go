// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"context"
	"fmt"

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

// NamespaceReconciler watches for namespace CREATE and UPDATE events
// and triggers reconciliation of source resources that match the new namespace.
type NamespaceReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Config          *config.Config
	Filter          *filter.NamespaceFilter
	NamespaceLister NamespaceLister
	// ResourceTypes contains all discovered resource types to reconcile
	ResourceTypes []config.ResourceType
}

// Reconcile processes namespace events and creates mirrors for matching sources.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("namespace", req.Name)

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

		// Resolve target namespaces for this source
		targetNamespaces, err := r.resolveTargetNamespaces(ctx, source)
		if err != nil {
			logger.Error(err, "failed to resolve target namespaces",
				"source", source.GetName(), "namespace", source.GetNamespace())
			errorCount++
			continue
		}

		// Check if the new namespace matches this source's targets
		var isTarget bool
		for _, target := range targetNamespaces {
			if target == namespaceName {
				isTarget = true
				break
			}
		}

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
			oldLabel := oldNs.Labels[constants.LabelAllowMirrors]
			newLabel := newNs.Labels[constants.LabelAllowMirrors]

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
