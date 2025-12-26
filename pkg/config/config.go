// Package config provides configuration for the kubemirror controller.
package config

import (
	"time"
)

// Config holds all configuration for the controller.
type Config struct {
	// MetricsBindAddress is the address for the metrics endpoint
	MetricsBindAddress string
	// HealthProbeBindAddress is the address for health probes
	HealthProbeBindAddress string

	// WatchNamespaces is the list of namespaces to watch (empty = all namespaces)
	WatchNamespaces []string
	// ExcludedNamespaces is the list of namespaces to never mirror to
	ExcludedNamespaces []string
	// MirroredResourceTypes is the list of resource types to mirror
	// If empty, defaults to Secret and ConfigMap only
	MirroredResourceTypes []ResourceType
	// DeniedResourceTypes is the deny-list of resource types (by name, for backward compatibility)
	DeniedResourceTypes []string

	// LeaderElection configuration
	LeaderElection LeaderElectionConfig

	// ReconcileInterval is how often to re-check all resources
	ReconcileInterval time.Duration

	// WorkerThreads is the number of concurrent reconciliation workers
	WorkerThreads int
	// RateLimitBurst is the burst capacity for rate limiting
	RateLimitBurst int
	// MemoryLimitMB is the memory limit in megabytes
	MemoryLimitMB int

	// DebounceDuration is the debounce window for source updates
	DebounceDuration time.Duration

	// MaxTargetsPerResource is the maximum number of target namespaces per resource
	MaxTargetsPerResource int

	// RateLimitQPS is the maximum queries per second to the API server
	RateLimitQPS float32

	// RequireNamespaceOptIn requires namespaces to have label for "all" mirrors
	RequireNamespaceOptIn bool
	// EnableAllKeyword enables the "all" keyword for target namespaces
	EnableAllKeyword bool
	// DryRun mode logs what would happen without actually making changes
	DryRun bool
	// VerifySourceFreshness checks cache staleness and re-fetches from API if needed
	// Prevents mirroring stale data when cache hasn't updated yet after watch event
	// Trades some API load for guaranteed data freshness
	VerifySourceFreshness bool
}

// LeaderElectionConfig holds leader election settings.
type LeaderElectionConfig struct {
	// ResourceName is the name of the leader election resource
	ResourceName string
	// ResourceNamespace is the namespace for the leader election resource
	ResourceNamespace string

	// LeaseDuration is the lease duration
	LeaseDuration time.Duration
	// RenewDeadline is the renew deadline
	RenewDeadline time.Duration
	// RetryPeriod is the retry period
	RetryPeriod time.Duration

	// Enabled enables leader election
	Enabled bool
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	// Add validation logic if needed
	return nil
}
