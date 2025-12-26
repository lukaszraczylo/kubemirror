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
}

// NewKubernetesNamespaceLister creates a new KubernetesNamespaceLister.
func NewKubernetesNamespaceLister(client client.Client) *KubernetesNamespaceLister {
	return &KubernetesNamespaceLister{
		client: client,
	}
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
func (k *KubernetesNamespaceLister) ListAllowMirrorsNamespaces(ctx context.Context) ([]string, error) {
	namespaceList := &corev1.NamespaceList{}

	// List namespaces with the allow-mirrors label
	if err := k.client.List(ctx, namespaceList, client.MatchingLabels{
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
func (k *KubernetesNamespaceLister) ListOptOutNamespaces(ctx context.Context) ([]string, error) {
	namespaceList := &corev1.NamespaceList{}

	// List namespaces with allow-mirrors label set to false
	if err := k.client.List(ctx, namespaceList, client.MatchingLabels{
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
