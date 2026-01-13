// Package main is the entry point for the kubemirror controller.
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/lukaszraczylo/kubemirror/pkg/config"
	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/lukaszraczylo/kubemirror/pkg/controller"
	"github.com/lukaszraczylo/kubemirror/pkg/discovery"
	"github.com/lukaszraczylo/kubemirror/pkg/filter"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

// makeCacheSyncChecker creates a healthz.Checker that verifies informer cache sync.
// This ensures the readiness probe fails if caches are not synced.
func makeCacheSyncChecker(c cache.Cache, ctx context.Context, logger logr.Logger) healthz.Checker {
	return func(_ *http.Request) error {
		// WaitForCacheSync returns true immediately if already synced,
		// or waits until sync completes or context is cancelled.
		// With a short context timeout, this provides a quick check.
		checkCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		if !c.WaitForCacheSync(checkCtx) {
			logger.V(1).Info("informer caches not yet synced")
			return errors.New("informer caches not synced")
		}
		return nil
	}
}

func main() {
	var (
		metricsAddr           string
		probeAddr             string
		enableLeaderElection  bool
		leaderElectionID      string
		excludedNamespaces    string
		includedNamespaces    string
		resourceTypes         string
		discoveryInterval     time.Duration
		maxTargets            int
		workerThreads         int
		rateLimitQPS          float64
		rateLimitBurst        int
		resyncPeriod          time.Duration
		verifySourceFreshness bool
		lazyWatcherInit       bool
		watcherScanInterval   time.Duration
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionID, "leader-election-id", constants.LeaderElectionID,
		"The name of the leader election lease.")
	flag.StringVar(&excludedNamespaces, "excluded-namespaces", "",
		"Comma-separated list of namespaces to exclude from mirroring (in addition to defaults).")
	flag.StringVar(&includedNamespaces, "included-namespaces", "",
		"Comma-separated list of namespace patterns to include (empty = all allowed).")
	flag.StringVar(&resourceTypes, "resource-types", "",
		"Comma-separated list of resource types to mirror (e.g., 'Secret.v1,ConfigMap.v1,Ingress.v1.networking.k8s.io'). "+
			"If empty, all mirrorable resources will be auto-discovered.")
	flag.DurationVar(&discoveryInterval, "discovery-interval", 5*time.Minute,
		"Interval for rediscovering available resources (auto-discovery mode only).")
	flag.IntVar(&maxTargets, "max-targets", 100,
		"Maximum number of target namespaces per resource.")
	flag.IntVar(&workerThreads, "worker-threads", 5,
		"Number of concurrent reconciliation workers.")
	flag.Float64Var(&rateLimitQPS, "rate-limit-qps", 50.0,
		"QPS rate limit for API server requests.")
	flag.IntVar(&rateLimitBurst, "rate-limit-burst", 100,
		"Burst limit for API server requests.")
	flag.DurationVar(&resyncPeriod, "resync-period", 10*time.Minute,
		"Period for resyncing all resources (catches updates missed due to informer cache delays).")
	flag.BoolVar(&verifySourceFreshness, "verify-source-freshness", false,
		"Verify source resource freshness by comparing cache with direct API read. "+
			"Prevents mirroring stale data when cache lags behind watch events. "+
			"Trade-off: Extra API call when cache is stale.")
	flag.BoolVar(&lazyWatcherInit, "lazy-watcher-init", false,
		"Enable lazy watcher initialization - only create informers for resource types that have resources marked for mirroring. "+
			"Significantly reduces memory usage by avoiding watchers for unused resource types. "+
			"Recommended for production environments with many unused resource types.")
	flag.DurationVar(&watcherScanInterval, "watcher-scan-interval", 5*time.Minute,
		"Interval for scanning cluster to detect new resource types needing watchers (lazy-watcher-init mode only).")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("starting kubemirror controller",
		"version", "dev",
		"maxTargets", maxTargets,
		"workers", workerThreads,
	)

	// Create controller configuration
	cfg := &config.Config{
		MaxTargetsPerResource: maxTargets,
		DebounceDuration:      500 * time.Millisecond,
		WorkerThreads:         workerThreads,
		RateLimitQPS:          float32(rateLimitQPS),
		RateLimitBurst:        rateLimitBurst,
		EnableAllKeyword:      true,
		RequireNamespaceOptIn: false,
		VerifySourceFreshness: verifySourceFreshness,
		LeaderElection: config.LeaderElectionConfig{
			Enabled:           enableLeaderElection,
			ResourceName:      leaderElectionID,
			ResourceNamespace: "", // Will be auto-detected
			LeaseDuration:     15 * time.Second,
			RenewDeadline:     10 * time.Second,
			RetryPeriod:       2 * time.Second,
		},
	}

	// Parse namespace filters
	var excludedList, includedList []string
	if excludedNamespaces != "" {
		excludedList = filter.ParseTargetNamespaces(excludedNamespaces)
	}
	if includedNamespaces != "" {
		includedList = filter.ParseTargetNamespaces(includedNamespaces)
	}

	// Combine with default exclusions
	allExcluded := append(constants.DefaultExcludedNamespaces, excludedList...)
	namespaceFilter := filter.NewNamespaceFilter(allExcluded, includedList)

	setupLog.Info("namespace filters configured",
		"excluded", allExcluded,
		"included", includedList,
	)

	// Parse and configure resource types
	var mirroredResources []config.ResourceType
	if resourceTypes != "" {
		// User-specified resource types
		var err error
		mirroredResources, err = config.ParseResourceTypes(resourceTypes)
		if err != nil {
			setupLog.Error(err, "failed to parse resource types")
			os.Exit(1)
		}
		setupLog.Info("using user-specified resource types", "count", len(mirroredResources))
	} else {
		// Auto-discovery mode
		setupLog.Info("enabling resource auto-discovery", "interval", discoveryInterval)
	}

	cfg.MirroredResourceTypes = mirroredResources

	// Create cache transform function to strip unnecessary fields and reduce memory usage
	// This can reduce memory consumption by 50-70% by removing:
	// - managedFields (often several KB per resource)
	// - large annotations like kubectl.kubernetes.io/last-applied-configuration
	transformFunc := func(obj interface{}) (interface{}, error) {
		// Type assert to unstructured
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil // Not unstructured, return as-is
		}

		// Strip managedFields - can be several KB per resource
		u.SetManagedFields(nil)

		// Strip large annotations that we don't need for reconciliation
		annotations := u.GetAnnotations()
		if annotations != nil {
			// Remove kubectl last-applied-configuration (can be very large)
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			// Remove other large annotations we don't need
			delete(annotations, "deployment.kubernetes.io/revision")
			u.SetAnnotations(annotations)
		}

		return obj, nil
	}

	// Set up controller manager with cache configuration
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         cfg.LeaderElection.Enabled,
		LeaderElectionID:       cfg.LeaderElection.ResourceName,
		LeaseDuration:          &cfg.LeaderElection.LeaseDuration,
		RenewDeadline:          &cfg.LeaderElection.RenewDeadline,
		RetryPeriod:            &cfg.LeaderElection.RetryPeriod,
		Cache: cache.Options{
			// Use the transform function to reduce memory usage
			DefaultTransform: transformFunc,
			// Increase the resync period to reduce memory churn
			SyncPeriod: &resyncPeriod,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Set up signal handler context for graceful shutdown
	signalCtx := ctrl.SetupSignalHandler()

	// Set up resource discovery if auto-discovery is enabled
	if resourceTypes == "" {
		restConfig := ctrl.GetConfigOrDie()
		var discoveryClient *discovery.ResourceDiscovery
		discoveryClient, err = discovery.NewResourceDiscovery(restConfig)
		if err != nil {
			setupLog.Error(err, "unable to create discovery client")
			os.Exit(1)
		}

		discoveryMgr := discovery.NewManager(discoveryClient, discoveryInterval)

		// Start discovery manager with signal-aware context
		err = discoveryMgr.Start(signalCtx)
		if err != nil {
			setupLog.Error(err, "unable to start discovery manager")
			os.Exit(1)
		}

		// Wait for initial discovery with 30s timeout
		waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = discoveryMgr.WaitForInitialDiscovery(waitCtx, 30*time.Second)
		if err != nil {
			setupLog.Error(err, "timeout waiting for initial resource discovery")
			os.Exit(1)
		}

		// Get discovered resources and update config
		mirroredResources = discoveryMgr.GetCurrentResources()
		cfg.MirroredResourceTypes = mirroredResources

		setupLog.Info("auto-discovered resources",
			"count", len(mirroredResources),
			"interval", discoveryInterval,
		)
	}

	// Create namespace lister with API reader for fresh namespace lookups.
	// This ensures label-based queries (allow-mirrors label) return fresh data
	// and don't suffer from informer cache staleness after label changes.
	namespaceLister := controller.NewKubernetesNamespaceListerWithAPIReader(
		mgr.GetClient(),
		mgr.GetAPIReader(),
	)

	// Validate flag combinations and warn about conflicts
	if lazyWatcherInit && resourceTypes != "" {
		setupLog.Info("WARNING: --resource-types flag is ignored in lazy-watcher-init mode",
			"specifiedTypes", resourceTypes,
			"reason", "lazy watcher discovers resource types dynamically based on actual usage",
		)
	}

	// Choose between lazy watcher initialization (scan for active resources) or eager (register all)
	if lazyWatcherInit {
		setupLog.Info("using lazy watcher initialization",
			"availableResourceTypes", len(cfg.MirroredResourceTypes),
			"scanInterval", watcherScanInterval,
		)

		// Factory functions for creating reconcilers
		sourceFactory := func(gvk schema.GroupVersionKind) *controller.SourceReconciler {
			return &controller.SourceReconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				Config:          cfg,
				Filter:          namespaceFilter,
				NamespaceLister: namespaceLister,
				GVK:             gvk,
				APIReader:       mgr.GetAPIReader(),
			}
		}

		mirrorFactory := func(gvk schema.GroupVersionKind) *controller.MirrorReconciler {
			return &controller.MirrorReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
				GVK:    gvk,
			}
		}

		// Create dynamic controller manager
		dynamicMgr := controller.NewDynamicControllerManager(controller.DynamicManagerConfig{
			Client:                  mgr.GetClient(),
			Manager:                 mgr,
			Config:                  cfg,
			Filter:                  namespaceFilter,
			NamespaceLister:         namespaceLister,
			AvailableResources:      cfg.MirroredResourceTypes,
			ScanInterval:            watcherScanInterval,
			SourceReconcilerFactory: sourceFactory,
			MirrorReconcilerFactory: mirrorFactory,
		})

		// Start dynamic controller manager
		err = dynamicMgr.Start(signalCtx)
		if err != nil {
			setupLog.Error(err, "unable to start dynamic controller manager")
			os.Exit(1)
		}

		setupLog.Info("dynamic controller manager started - controllers will be registered on-demand")
	} else {
		setupLog.Info("using eager watcher initialization",
			"resourceTypes", len(cfg.MirroredResourceTypes),
		)

		// Eager mode: Register controllers for all discovered resource types upfront
		// Create a separate reconciler instance for each resource type
		for _, rt := range cfg.MirroredResourceTypes {
			gvk := rt.GroupVersionKind()
			setupLog.Info("registering controller for resource type",
				"group", gvk.Group,
				"version", gvk.Version,
				"kind", gvk.Kind,
			)

			// Create a source reconciler instance for this specific resource type
			sourceReconciler := &controller.SourceReconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				Config:          cfg,
				Filter:          namespaceFilter,
				NamespaceLister: namespaceLister,
				GVK:             gvk,
				APIReader:       mgr.GetAPIReader(), // Direct API reader (bypasses cache)
			}

			if err = sourceReconciler.SetupWithManagerForResourceType(mgr, gvk); err != nil {
				setupLog.Error(err, "unable to create source controller",
					"resourceType", rt.String(),
				)
				os.Exit(1)
			}

			// Create a mirror reconciler instance for orphan detection
			// This watches mirrored resources (with managed-by label) and verifies their source still exists
			mirrorReconciler := &controller.MirrorReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
				GVK:    gvk,
			}

			if err = mirrorReconciler.SetupWithManager(mgr, gvk); err != nil {
				setupLog.Error(err, "unable to create mirror controller",
					"resourceType", rt.String(),
				)
				os.Exit(1)
			}
		}

		setupLog.Info("registered source and mirror controllers", "count", len(cfg.MirroredResourceTypes))
	}

	// Register namespace reconciler to watch for new namespaces and label changes
	namespaceReconciler := &controller.NamespaceReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		Config:          cfg,
		Filter:          namespaceFilter,
		NamespaceLister: namespaceLister,
		ResourceTypes:   cfg.MirroredResourceTypes,
		APIReader:       mgr.GetAPIReader(), // Direct API reader for fresh namespace lookups
	}

	if err = namespaceReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create namespace reconciler")
		os.Exit(1)
	}

	setupLog.Info("registered namespace reconciler")

	// Add health checks
	// Liveness: basic ping to verify the controller process is alive
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// Readiness: check that informer caches are synced before accepting traffic.
	// This prevents reconciliation from running with incomplete/stale cache data.
	// The cache sync check ensures all informers have received initial data from the API server.
	// Note: The manager automatically waits for cache sync before starting controllers,
	// but this check ensures the readiness probe reflects cache state for external monitoring.
	cacheReadyCheck := makeCacheSyncChecker(mgr.GetCache(), signalCtx, setupLog)
	if err := mgr.AddReadyzCheck("readyz", cacheReadyCheck); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(signalCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
