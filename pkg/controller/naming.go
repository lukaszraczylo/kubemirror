// Package controller implements the kubemirror reconciliation logic.
package controller

import "k8s.io/apimachinery/pkg/runtime/schema"

// gvkControllerName builds the unique controller-runtime controller name for a
// GVK. Including version and group avoids collisions across API versions, e.g.
// "HorizontalPodAutoscaler.v1.autoscaling" or "Secret.v1." (empty group for
// core resources). Source and mirror reconcilers for the same GVK must differ,
// so mirror controllers get a "-mirror" suffix. This is the single source of
// truth for the convention shared by both reconcilers.
func gvkControllerName(gvk schema.GroupVersionKind, mirror bool) string {
	name := gvk.Kind + "." + gvk.Version + "." + gvk.Group
	if mirror {
		name += "-mirror"
	}
	return name
}
