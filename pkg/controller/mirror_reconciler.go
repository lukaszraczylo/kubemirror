package controller

import (
	"context"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// MirrorReconciler reconciles mirrored resources to detect and clean up orphans.
// This reconciler watches resources with the managed-by label and verifies their source still exists.
type MirrorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	GVK    schema.GroupVersionKind // The resource type this reconciler handles
}

// Reconcile checks if a mirrored resource's source still exists, and deletes the mirror if orphaned.
func (r *MirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"mirrorNamespace", req.Namespace,
		"mirrorName", req.Name,
		"kind", r.GVK.Kind,
		"group", r.GVK.Group,
		"version", r.GVK.Version,
	)

	// Fetch the mirror resource
	mirror := &unstructured.Unstructured{}
	gv := schema.GroupVersion{Group: r.GVK.Group, Version: r.GVK.Version}
	mirror.SetGroupVersionKind(gv.WithKind(r.GVK.Kind))

	if err := r.Get(ctx, req.NamespacedName, mirror); err != nil {
		// Mirror already deleted or doesn't exist - nothing to do
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Extract annotations using unstructured helper methods
	annotations := mirror.GetAnnotations()
	if annotations == nil {
		// No annotations - not a valid mirror, skip
		return ctrl.Result{}, nil
	}

	// Extract source reference from annotations
	sourceNs, hasSourceNs := annotations[constants.AnnotationSourceNamespace]
	sourceName, hasSourceName := annotations[constants.AnnotationSourceName]
	sourceUID, hasSourceUID := annotations[constants.AnnotationSourceUID]

	if !hasSourceNs || !hasSourceName || !hasSourceUID {
		// Missing source reference annotations - not a valid mirror or corrupted
		logger.V(1).Info("mirror missing source reference annotations, skipping",
			"namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, nil
	}

	// Try to fetch the source resource
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(gv.WithKind(r.GVK.Kind))
	sourceKey := types.NamespacedName{
		Namespace: sourceNs,
		Name:      sourceName,
	}

	err := r.Get(ctx, sourceKey, source)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Source not found - this is an orphaned mirror, delete it
			logger.Info("orphaned mirror detected (source deleted), cleaning up",
				"mirror", req.NamespacedName,
				"sourceNamespace", sourceNs,
				"sourceName", sourceName,
				"sourceUID", sourceUID)

			deleteErr := r.Delete(ctx, mirror)
			if deleteErr != nil {
				logger.Error(deleteErr, "failed to delete orphaned mirror")
				return ctrl.Result{}, deleteErr
			}

			logger.Info("orphaned mirror deleted successfully",
				"mirror", req.NamespacedName,
				"sourceNamespace", sourceNs,
				"sourceName", sourceName)
			return ctrl.Result{}, nil
		}

		// Some other error fetching source
		logger.Error(err, "failed to fetch source resource for mirror",
			"sourceNamespace", sourceNs, "sourceName", sourceName)
		return ctrl.Result{}, err
	}

	// Source exists - verify UID matches
	actualUID := string(source.GetUID())
	if actualUID != sourceUID {
		// Source was recreated with different UID - this is a stale mirror
		logger.Info("stale mirror detected (source recreated with different UID), cleaning up",
			"mirror", req.NamespacedName,
			"sourceNamespace", sourceNs,
			"sourceName", sourceName,
			"expectedUID", sourceUID,
			"actualUID", actualUID)

		if err := r.Delete(ctx, mirror); err != nil {
			logger.Error(err, "failed to delete stale mirror")
			return ctrl.Result{}, err
		}

		logger.Info("stale mirror deleted successfully",
			"mirror", req.NamespacedName,
			"sourceNamespace", sourceNs,
			"sourceName", sourceName)
		return ctrl.Result{}, nil
	}

	// Source exists and UID matches - mirror is valid
	logger.V(1).Info("mirror source verified",
		"mirror", req.NamespacedName,
		"sourceNamespace", sourceNs,
		"sourceName", sourceName)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorReconciler) SetupWithManager(mgr ctrl.Manager, gvk schema.GroupVersionKind) error {
	// Create a predicate that only watches resources with the managed-by label
	managedByPredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		labels := obj.GetLabels()
		if labels == nil {
			return false
		}
		managedBy, exists := labels[constants.LabelManagedBy]
		return exists && managedBy == "kubemirror"
	})

	// Convert GVK to resource object for watching
	obj := &unstructured.Unstructured{}
	gv := schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
	obj.SetGroupVersionKind(gv.WithKind(gvk.Kind))

	// Set custom controller name to avoid conflicts with source reconciler and multiple API versions
	// Include group and version to make it truly unique
	controllerName := gvk.Kind + "." + gvk.Version + "." + gvk.Group + "-mirror"

	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Named(controllerName).
		WithEventFilter(managedByPredicate).
		Complete(r)
}
