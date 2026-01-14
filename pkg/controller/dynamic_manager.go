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

// RegistrationState tracks the granular state of controller registration
type RegistrationState int

const (
	// StateNotRegistered means no controllers are registered for this GVK
	StateNotRegistered RegistrationState = iota
	// StateSourceOnly means only the source controller is registered (partial failure)
	StateSourceOnly
	// StateFullyRegistered means both source and mirror controllers are registered
	StateFullyRegistered
)

// String returns a human-readable representation of the registration state
func (rs RegistrationState) String() string {
	switch rs {
	case StateNotRegistered:
		return "not-registered"
	case StateSourceOnly:
		return "source-only"
	case StateFullyRegistered:
		return "fully-registered"
	default:
		return "unknown"
	}
}

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
	client                  client.Client
	apiReader               client.Reader // Direct API reader (bypasses cache)
	mgr                     ctrl.Manager
	namespaceLister         NamespaceLister
	config                  *config.Config
	filter                  *filter.NamespaceFilter
	registrationState       map[string]RegistrationState // Granular registration state tracking
	activeResourceTypes     map[string]schema.GroupVersionKind
	sourceReconcilerFactory SourceReconcilerFactory
	mirrorReconcilerFactory MirrorReconcilerFactory
	availableResourceTypes  []config.ResourceType
	scanInterval            time.Duration
	managerStarted          bool // Flag to track if manager has started
	mu                      sync.RWMutex
}

// SourceReconcilerFactory creates source reconcilers for a given GVK
type SourceReconcilerFactory func(gvk schema.GroupVersionKind) *SourceReconciler

// MirrorReconcilerFactory creates mirror reconcilers for a given GVK
type MirrorReconcilerFactory func(gvk schema.GroupVersionKind) *MirrorReconciler

// DynamicManagerConfig configures the dynamic controller manager
type DynamicManagerConfig struct {
	Client                  client.Client
	APIReader               client.Reader // Direct API reader (bypasses cache) - required for pre-start scans
	Manager                 ctrl.Manager
	NamespaceLister         NamespaceLister
	Config                  *config.Config
	Filter                  *filter.NamespaceFilter
	SourceReconcilerFactory SourceReconcilerFactory
	MirrorReconcilerFactory MirrorReconcilerFactory
	AvailableResources      []config.ResourceType
	ScanInterval            time.Duration
}

// NewDynamicControllerManager creates a new dynamic controller manager
func NewDynamicControllerManager(cfg DynamicManagerConfig) *DynamicControllerManager {
	if cfg.ScanInterval == 0 {
		cfg.ScanInterval = 5 * time.Minute
	}

	return &DynamicControllerManager{
		client:                  cfg.Client,
		apiReader:               cfg.APIReader,
		mgr:                     cfg.Manager,
		config:                  cfg.Config,
		filter:                  cfg.Filter,
		namespaceLister:         cfg.NamespaceLister,
		scanInterval:            cfg.ScanInterval,
		registrationState:       make(map[string]RegistrationState),
		activeResourceTypes:     make(map[string]schema.GroupVersionKind),
		managerStarted:          false,
		availableResourceTypes:  cfg.AvailableResources,
		sourceReconcilerFactory: cfg.SourceReconcilerFactory,
		mirrorReconcilerFactory: cfg.MirrorReconcilerFactory,
	}
}

// Start begins the dynamic controller management loop.
// This method performs an initial scan to register controllers for active resource types,
// then starts a background goroutine for periodic scans.
// IMPORTANT: This should be called BEFORE mgr.Start() to ensure controllers are registered
// before the manager starts. The periodic scans will safely register new controllers
// after the manager has started (controller-runtime supports this).
func (d *DynamicControllerManager) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")

	// Initial scan and registration (before main manager starts)
	logger.Info("performing initial scan for active resource types")
	if err := d.scanAndRegister(ctx); err != nil {
		return fmt.Errorf("initial scan failed: %w", err)
	}

	// Start periodic scanning (will run after main manager starts)
	go d.run(ctx)

	logger.Info("dynamic controller manager started",
		"scanInterval", d.scanInterval,
		"initialControllersRegistered", d.GetRegisteredCount(),
	)

	return nil
}

// MarkManagerStarted notifies the dynamic controller manager that the main manager has started.
// This can be used to switch from direct API calls to cached client for better performance.
// Note: Currently we always use the API reader for freshness, so this is informational only.
func (d *DynamicControllerManager) MarkManagerStarted() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.managerStarted = true
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
	var newlyRegistered, alreadyRegistered, partialRetried int

	// Register controllers for active resource types
	for gvkStr, gvk := range activeTypes {
		state := d.registrationState[gvkStr]

		switch state {
		case StateFullyRegistered:
			// Already fully registered, nothing to do
			alreadyRegistered++
			continue

		case StateSourceOnly:
			// Partial registration - retry mirror controller only
			partialRetried++
			if err := d.registerMirrorControllerOnly(ctx, gvk); err != nil {
				logger.Error(err, "failed to complete partial registration (mirror controller)",
					"gvk", gvkStr,
					"currentState", state.String(),
				)
				continue
			}

			d.registrationState[gvkStr] = StateFullyRegistered
			logger.Info("completed partial registration",
				"group", gvk.Group,
				"version", gvk.Version,
				"kind", gvk.Kind,
			)

		case StateNotRegistered:
			// New registration - register both controllers
			newState, err := d.registerController(ctx, gvk)
			if err != nil {
				logger.Error(err, "failed to register controller",
					"gvk", gvkStr,
					"achievedState", newState.String(),
				)
				// Save partial state if source was registered
				if newState == StateSourceOnly {
					d.registrationState[gvkStr] = newState
					d.activeResourceTypes[gvkStr] = gvk
					logger.Info("partial registration - source controller only",
						"group", gvk.Group,
						"version", gvk.Version,
						"kind", gvk.Kind,
					)
				}
				continue
			}

			d.registrationState[gvkStr] = StateFullyRegistered
			d.activeResourceTypes[gvkStr] = gvk
			newlyRegistered++

			logger.Info("registered controller for active resource type",
				"group", gvk.Group,
				"version", gvk.Version,
				"kind", gvk.Kind,
			)
		}
	}

	// Count fully registered controllers
	fullyRegistered := 0
	for _, state := range d.registrationState {
		if state == StateFullyRegistered {
			fullyRegistered++
		}
	}

	logger.Info("scan completed",
		"activeResourceTypes", len(activeTypes),
		"alreadyRegistered", alreadyRegistered,
		"newlyRegistered", newlyRegistered,
		"partialRetried", partialRetried,
		"fullyRegistered", fullyRegistered,
	)

	return nil
}

// getReader returns the appropriate reader based on whether the manager has started.
// Before manager starts, we must use the API reader (direct API calls).
// After manager starts, we can use the cached client for better performance.
func (d *DynamicControllerManager) getReader() client.Reader {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Always use API reader if available - it bypasses cache and gives fresh data
	// This is important for finding newly-labeled resources that might not be in cache yet
	if d.apiReader != nil {
		return d.apiReader
	}
	return d.client
}

// findActiveResourceTypes scans the cluster for resources with the enabled label
// and returns a map of GVK strings to their schema.GroupVersionKind
func (d *DynamicControllerManager) findActiveResourceTypes(ctx context.Context) (map[string]schema.GroupVersionKind, error) {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")
	activeTypes := make(map[string]schema.GroupVersionKind)

	reader := d.getReader()

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

		if err := reader.List(ctx, list, opts...); err != nil {
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

// registerController registers source and mirror controllers for a GVK.
// Returns the achieved registration state and any error.
// If source registration succeeds but mirror fails, returns StateSourceOnly to allow retry.
func (d *DynamicControllerManager) registerController(ctx context.Context, gvk schema.GroupVersionKind) (RegistrationState, error) {
	logger := log.FromContext(ctx).WithName("dynamic-controller-manager")

	// Create source reconciler using factory
	sourceReconciler := d.sourceReconcilerFactory(gvk)

	// Register source controller
	if err := sourceReconciler.SetupWithManagerForResourceType(d.mgr, gvk); err != nil {
		return StateNotRegistered, fmt.Errorf("failed to register source controller: %w", err)
	}

	// Source registered successfully, now try mirror
	logger.V(1).Info("source controller registered",
		"group", gvk.Group,
		"version", gvk.Version,
		"kind", gvk.Kind,
	)

	// Create mirror reconciler using factory
	mirrorReconciler := d.mirrorReconcilerFactory(gvk)

	// Register mirror controller
	if err := mirrorReconciler.SetupWithManager(d.mgr, gvk); err != nil {
		// Source is registered but mirror failed - return partial state
		return StateSourceOnly, fmt.Errorf("source registered but mirror failed: %w", err)
	}

	logger.Info("registered both controllers",
		"group", gvk.Group,
		"version", gvk.Version,
		"kind", gvk.Kind,
	)

	return StateFullyRegistered, nil
}

// registerMirrorControllerOnly registers only the mirror controller for a GVK.
// Used to complete partial registrations where source was registered but mirror failed.
func (d *DynamicControllerManager) registerMirrorControllerOnly(ctx context.Context, gvk schema.GroupVersionKind) error {
	// Create mirror reconciler using factory
	mirrorReconciler := d.mirrorReconcilerFactory(gvk)

	// Register mirror controller
	if err := mirrorReconciler.SetupWithManager(d.mgr, gvk); err != nil {
		return fmt.Errorf("failed to register mirror controller: %w", err)
	}

	return nil
}

// GetRegisteredCount returns the number of fully registered controllers
func (d *DynamicControllerManager) GetRegisteredCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	count := 0
	for _, state := range d.registrationState {
		if state == StateFullyRegistered {
			count++
		}
	}
	return count
}

// GetRegistrationState returns the registration state for a specific GVK
func (d *DynamicControllerManager) GetRegistrationState(gvkStr string) RegistrationState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.registrationState[gvkStr]
}

// GetRegistrationStats returns counts of controllers in each state
func (d *DynamicControllerManager) GetRegistrationStats() (fullyRegistered, sourceOnly, notRegistered int) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, state := range d.registrationState {
		switch state {
		case StateFullyRegistered:
			fullyRegistered++
		case StateSourceOnly:
			sourceOnly++
		default:
			notRegistered++
		}
	}
	return
}

// GetActiveResourceTypes returns a copy of the active resource types map
func (d *DynamicControllerManager) GetActiveResourceTypes() map[string]schema.GroupVersionKind {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]schema.GroupVersionKind, len(d.activeResourceTypes))
	for k, v := range d.activeResourceTypes {
		result[k] = v
	}
	return result
}
