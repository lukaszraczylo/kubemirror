// Package controller implements dynamic controller registration for kubemirror.
package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/filter"
)

// DynamicControllerManager manages lazy initialization of controllers
// for resource types that actually have resources marked for mirroring.
//
// This significantly reduces memory usage by avoiding watchers for resource types
// that will never be mirrored (e.g., watching 204 resource types but only using 2).
//
// How it works:
// 1. Periodically scans cluster for resources with kubemirror.raczylo.com/enabled=true label
// 2. Tracks which resource types have active source resources
// 3. Dynamically registers controllers only for resource types in use
// 4. Optionally unregisters controllers for resource types no longer in use
type DynamicControllerManager struct {
	client          client.Client
	mgr             ctrl.Manager
	config          *config.Config
	filter          *filter.NamespaceFilter
	namespaceLister NamespaceLister
	scanInterval    time.Duration

	// Tracking state
	mu                     sync.RWMutex
	registeredControllers  map[string]bool // GVK string -> registered
	activeResourceTypes    map[string]schema.GroupVersionKind
	availableResourceTypes []config.ResourceType

	// Reconciler factories
	sourceReconcilerFactory SourceReconcilerFactory
	mirrorReconcilerFactory MirrorReconcilerFactory
}

// SourceReconcilerFactory creates source reconcilers for a given GVK
type SourceReconcilerFactory func(gvk schema.GroupVersionKind) *SourceReconciler

// MirrorReconcilerFactory creates mirror reconcilers for a given GVK
type MirrorReconcilerFactory func(gvk schema.GroupVersionKind) *MirrorReconciler

// DynamicManagerConfig configures the dynamic controller manager
type DynamicManagerConfig struct {
	Client                  client.Client
	Manager                 ctrl.Manager
	Config                  *config.Config
	Filter                  *filter.NamespaceFilter
	NamespaceLister         NamespaceLister
	AvailableResources      []config.ResourceType
	ScanInterval            time.Duration // How often to scan for new resources (default: 5m)
	SourceReconcilerFactory SourceReconcilerFactory
	MirrorReconcilerFactory MirrorReconcilerFactory
}

// NewDynamicControllerManager creates a new dynamic controller manager
func NewDynamicControllerManager(cfg DynamicManagerConfig) *DynamicControllerManager {
	if cfg.ScanInterval == 0 {
		cfg.ScanInterval = 5 * time.Minute
	}

	return &DynamicControllerManager{
		client:                  cfg.Client,
		mgr:                     cfg.Manager,
		config:                  cfg.Config,
		filter:                  cfg.Filter,
		namespaceLister:         cfg.NamespaceLister,
		scanInterval:            cfg.ScanInterval,
		registeredControllers:   make(map[string]bool),
		activeResourceTypes:     make(map[string]schema.GroupVersionKind),
		availableResourceTypes:  cfg.AvailableResources,
		sourceReconcilerFactory: cfg.SourceReconcilerFactory,
		mirrorReconcilerFactory: cfg.MirrorReconcilerFactory,
	}
}

// Start begins the dynamic controller management loop
func (d *DynamicControllerManager) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")

	// Initial scan and registration
	if err := d.scanAndRegister(ctx); err != nil {
		return fmt.Errorf("initial scan failed: %w", err)
	}

	// Start periodic scanning
	go d.run(ctx)

	logger.Info("dynamic controller manager started",
		"scanInterval", d.scanInterval,
	)

	return nil
}

// run is the main loop for periodic scanning
func (d *DynamicControllerManager) run(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")
	ticker := time.NewTicker(d.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("dynamic controller manager stopped")
			return
		case <-ticker.C:
			if err := d.scanAndRegister(ctx); err != nil {
				logger.Error(err, "periodic scan failed")
			}
		}
	}
}

// scanAndRegister scans the cluster for resources needing watchers and registers controllers
func (d *DynamicControllerManager) scanAndRegister(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")

	// Find resource types that have active source resources
	activeTypes, err := d.findActiveResourceTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to find active resource types: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Track changes
	var newlyRegistered, alreadyRegistered int

	// Register controllers for active resource types
	for gvkStr, gvk := range activeTypes {
		if d.registeredControllers[gvkStr] {
			alreadyRegistered++
			continue
		}

		// Register new controller
		if err := d.registerController(ctx, gvk); err != nil {
			logger.Error(err, "failed to register controller",
				"gvk", gvkStr,
			)
			continue
		}

		d.registeredControllers[gvkStr] = true
		d.activeResourceTypes[gvkStr] = gvk
		newlyRegistered++

		logger.Info("registered controller for active resource type",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
		)
	}

	logger.Info("scan completed",
		"activeResourceTypes", len(activeTypes),
		"alreadyRegistered", alreadyRegistered,
		"newlyRegistered", newlyRegistered,
		"totalRegistered", len(d.registeredControllers),
	)

	return nil
}

// findActiveResourceTypes scans the cluster for resources with the enabled label
// and returns a map of GVK strings to their schema.GroupVersionKind
func (d *DynamicControllerManager) findActiveResourceTypes(ctx context.Context) (map[string]schema.GroupVersionKind, error) {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")
	activeTypes := make(map[string]schema.GroupVersionKind)

	// For each available resource type, check if any resources exist with the enabled label
	for _, rt := range d.availableResourceTypes {
		gvk := rt.GroupVersionKind()
		gvkStr := rt.String()

		// Create unstructured list to query resources
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List", // List suffix
		})

		// Query with label selector
		opts := []client.ListOption{
			client.MatchingLabels{
				constants.LabelEnabled: "true",
			},
		}

		if err := d.client.List(ctx, list, opts...); err != nil {
			// Ignore errors for resource types that don't exist or we can't access
			logger.V(2).Info("failed to list resources (ignoring)",
				"gvk", gvkStr,
				"error", err.Error(),
			)
			continue
		}

		// If we found any resources with the label, mark this type as active
		if len(list.Items) > 0 {
			activeTypes[gvkStr] = gvk
			logger.V(1).Info("found active resources",
				"gvk", gvkStr,
				"count", len(list.Items),
			)
		}
	}

	return activeTypes, nil
}

// registerController registers source and mirror controllers for a GVK
func (d *DynamicControllerManager) registerController(ctx context.Context, gvk schema.GroupVersionKind) error {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")

	// Create source reconciler using factory
	sourceReconciler := d.sourceReconcilerFactory(gvk)

	// Register source controller
	if err := sourceReconciler.SetupWithManagerForResourceType(d.mgr, gvk); err != nil {
		return fmt.Errorf("failed to register source controller: %w", err)
	}

	// Create mirror reconciler using factory
	mirrorReconciler := d.mirrorReconcilerFactory(gvk)

	// Register mirror controller
	if err := mirrorReconciler.SetupWithManager(d.mgr, gvk); err != nil {
		return fmt.Errorf("failed to register mirror controller: %w", err)
	}

	logger.Info("registered controllers",
		"group", gvk.Group,
		"version", gvk.Version,
		"kind", gvk.Kind,
	)

	return nil
}
