// Package controller implements the kubemirror reconciliation logic.
package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
)

// KubernetesNamespaceLister implements NamespaceLister using the Kubernetes API.
type KubernetesNamespaceLister struct {
	client client.Client
	// apiReader provides direct API access bypassing cache (optional).
	// When set, it's used for label-based queries where cache staleness
	// can cause missed namespaces after label changes.
	apiReader client.Reader
}

// NewKubernetesNamespaceLister creates a new KubernetesNamespaceLister.
func NewKubernetesNamespaceLister(client client.Client) *KubernetesNamespaceLister {
	return &KubernetesNamespaceLister{
		client: client,
	}
}

// NewKubernetesNamespaceListerWithAPIReader creates a KubernetesNamespaceLister
// that uses direct API reads for label-based queries. This is more expensive
// but ensures fresh data for critical queries like allow-mirrors label lookups.
func NewKubernetesNamespaceListerWithAPIReader(c client.Client, apiReader client.Reader) *KubernetesNamespaceLister {
	return &KubernetesNamespaceLister{
		client:    c,
		apiReader: apiReader,
	}
}

// getReader returns the appropriate reader to use.
// Returns apiReader if available (for fresh reads), otherwise falls back to cached client.
func (k *KubernetesNamespaceLister) getReader() client.Reader {
	if k.apiReader != nil {
		return k.apiReader
	}
	return k.client
}

// ListNamespaces returns all namespace names in the cluster.
func (k *KubernetesNamespaceLister) ListNamespaces(ctx context.Context) ([]string, error) {
	namespaceList := &corev1.NamespaceList{}
	if err := k.client.List(ctx, namespaceList); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(namespaceList.Items))
	for _, ns := range namespaceList.Items {
		names = append(names, ns.Name)
	}

	return names, nil
}

// ListAllowMirrorsNamespaces returns namespaces that have the allow-mirrors label.
// Uses direct API reads if apiReader is configured to avoid cache staleness issues.
func (k *KubernetesNamespaceLister) ListAllowMirrorsNamespaces(ctx context.Context) ([]string, error) {
	namespaceList := &corev1.NamespaceList{}

	// Use direct API reader for label queries to ensure fresh data.
	// This is critical because cache staleness can cause namespaces with
	// newly added allow-mirrors labels to be missed.
	reader := k.getReader()
	if err := reader.List(ctx, namespaceList, client.MatchingLabels{
		constants.LabelAllowMirrors: "true",
	}); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(namespaceList.Items))
	for _, ns := range namespaceList.Items {
		names = append(names, ns.Name)
	}

	return names, nil
}

// ListOptOutNamespaces returns namespaces that have explicitly opted out of mirrors.
// These are namespaces with allow-mirrors="false".
// Uses direct API reads if apiReader is configured to avoid cache staleness issues.
func (k *KubernetesNamespaceLister) ListOptOutNamespaces(ctx context.Context) ([]string, error) {
	namespaceList := &corev1.NamespaceList{}

	// Use direct API reader for label queries to ensure fresh data.
	reader := k.getReader()
	if err := reader.List(ctx, namespaceList, client.MatchingLabels{
		constants.LabelAllowMirrors: "false",
	}); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(namespaceList.Items))
	for _, ns := range namespaceList.Items {
		names = append(names, ns.Name)
	}

	return names, nil
}
