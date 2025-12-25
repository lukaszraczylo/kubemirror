package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
)

// Manager handles periodic resource discovery and controller registration.
type Manager struct {
	discovery        *ResourceDiscovery
	logger           logr.Logger
	currentResources []config.ResourceType
	interval         time.Duration
	mu               sync.RWMutex
}

// NewManager creates a new discovery manager.
func NewManager(discovery *ResourceDiscovery, interval time.Duration) *Manager {
	return &Manager{
		discovery:        discovery,
		interval:         interval,
		currentResources: []config.ResourceType{},
	}
}

// Start begins periodic resource discovery.
// It performs an initial discovery immediately, then rediscovers on the specified interval.
func (m *Manager) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("discovery-manager")
	m.logger = logger

	// Initial discovery
	if err := m.discover(ctx); err != nil {
		return fmt.Errorf("initial resource discovery failed: %w", err)
	}

	// Start periodic rediscovery
	go m.run(ctx)

	return nil
}

// GetCurrentResources returns the currently discovered resource types.
func (m *Manager) GetCurrentResources() []config.ResourceType {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent concurrent modification
	result := make([]config.ResourceType, len(m.currentResources))
	copy(result, m.currentResources)
	return result
}

// run is the main discovery loop.
func (m *Manager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	logger := log.FromContext(ctx).WithName("discovery-manager")

	for {
		select {
		case <-ctx.Done():
			logger.Info("discovery manager stopped due to context cancellation")
			return
		case <-ticker.C:
			if err := m.discover(ctx); err != nil {
				logger.Error(err, "periodic resource discovery failed")
			}
		}
	}
}

// discover performs resource discovery and detects changes.
func (m *Manager) discover(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("discovery-manager")

	// Discover current resources
	discovered, err := m.discovery.DiscoverMirrorableResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover resources: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Detect changes
	added, removed := m.detectChanges(m.currentResources, discovered)

	if len(added) > 0 {
		logger.Info("new resource types discovered",
			"count", len(added),
			"resources", resourceTypesToStrings(added),
		)
	}

	if len(removed) > 0 {
		logger.Info("resource types removed",
			"count", len(removed),
			"resources", resourceTypesToStrings(removed),
		)
	}

	if len(added) == 0 && len(removed) == 0 {
		logger.V(1).Info("no changes in discovered resources",
			"total", len(discovered),
		)
	}

	// Update current resources
	m.currentResources = discovered

	logger.Info("resource discovery completed",
		"total", len(discovered),
		"added", len(added),
		"removed", len(removed),
	)

	return nil
}

// detectChanges compares old and new resource lists to find additions and removals.
func (m *Manager) detectChanges(old, new []config.ResourceType) (added, removed []config.ResourceType) {
	oldMap := make(map[string]config.ResourceType)
	newMap := make(map[string]config.ResourceType)

	for _, rt := range old {
		oldMap[rt.String()] = rt
	}

	for _, rt := range new {
		newMap[rt.String()] = rt
	}

	// Find added resources
	for key, rt := range newMap {
		if _, exists := oldMap[key]; !exists {
			added = append(added, rt)
		}
	}

	// Find removed resources
	for key, rt := range oldMap {
		if _, exists := newMap[key]; !exists {
			removed = append(removed, rt)
		}
	}

	return added, removed
}

// resourceTypesToStrings converts a slice of ResourceType to strings for logging.
func resourceTypesToStrings(resources []config.ResourceType) []string {
	result := make([]string, len(resources))
	for i, rt := range resources {
		result[i] = rt.String()
	}
	return result
}

// WaitForInitialDiscovery blocks until the first discovery completes.
// Useful for ensuring resources are discovered before starting controllers.
func (m *Manager) WaitForInitialDiscovery(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for initial discovery")
		case <-ticker.C:
			m.mu.RLock()
			hasResources := len(m.currentResources) > 0
			m.mu.RUnlock()

			if hasResources {
				return nil
			}
		}
	}
}
