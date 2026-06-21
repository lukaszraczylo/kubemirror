// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mirrorDeletionOutcome describes the result of an ownership-verified mirror
// deletion attempt.
type mirrorDeletionOutcome int

const (
	// mirrorAbsent means no resource exists at the requested namespace/name.
	mirrorAbsent mirrorDeletionOutcome = iota
	// mirrorNotOwned means a resource exists but is not a kubemirror-managed
	// mirror of the given source, so it was left untouched.
	mirrorNotOwned
	// mirrorDeleted means an owned mirror was found and deleted.
	mirrorDeleted
)

// deleteOwnedMirror deletes the mirror at gvk/ns/name only if it is managed by
// kubemirror AND its source reference matches srcNs/srcName. This ownership
// guard is the single safety gate that prevents deleting an unrelated resource
// that merely shares the same name. All mirror-cleanup paths route through here
// so the guard cannot be forgotten in one place and drift in another.
//
// A NotFound on delete is treated as success (the mirror is already gone).
func deleteOwnedMirror(
	ctx context.Context,
	c client.Client,
	gvk schema.GroupVersionKind,
	ns, name, srcNs, srcName string,
) (mirrorDeletionOutcome, error) {
	mirror := &unstructured.Unstructured{}
	mirror.SetGroupVersionKind(gvk)

	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, mirror); err != nil {
		if errors.IsNotFound(err) {
			return mirrorAbsent, nil
		}
		return mirrorAbsent, err
	}

	// Verify this is actually our mirror (not someone else's resource with the same name).
	if !IsManagedByUs(mirror) {
		return mirrorNotOwned, nil
	}

	// Verify this mirror points to our source.
	refNs, refName, _, found := GetSourceReference(mirror)
	if !found || refNs != srcNs || refName != srcName {
		return mirrorNotOwned, nil
	}

	if err := c.Delete(ctx, mirror); err != nil && !errors.IsNotFound(err) {
		return mirrorNotOwned, err
	}
	return mirrorDeleted, nil
}
