// Package main is the entry point for the kubemirror controller.
package main

import (
	"context"
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
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

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
		leaderElectionID     string
		excludedNamespaces   string
		includedNamespaces   string
		resourceTypes        string
		discoveryInterval    time.Duration
		maxTargets           int
		workerThreads        int
		rateLimitQPS         float64
		rateLimitBurst       int
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

	// Set up controller manager
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
		discoveryClient, err := discovery.NewResourceDiscovery(restConfig)
		if err != nil {
			setupLog.Error(err, "unable to create discovery client")
			os.Exit(1)
		}

		discoveryMgr := discovery.NewManager(discoveryClient, discoveryInterval)

		// Start discovery manager with signal-aware context
		if err := discoveryMgr.Start(signalCtx); err != nil {
			setupLog.Error(err, "unable to start discovery manager")
			os.Exit(1)
		}

		// Wait for initial discovery with 30s timeout
		waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := discoveryMgr.WaitForInitialDiscovery(waitCtx, 30*time.Second); err != nil {
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

	// Create namespace lister
	namespaceLister := controller.NewKubernetesNamespaceLister(mgr.GetClient())

	// Dynamically register controllers for all discovered resource types
	// Create a separate reconciler instance for each resource type
	for _, rt := range cfg.MirroredResourceTypes {
		gvk := rt.GroupVersionKind()
		setupLog.Info("registering controller for resource type",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
		)

		// Create a reconciler instance for this specific resource type
		reconciler := &controller.SourceReconciler{
			Client:          mgr.GetClient(),
			Scheme:          mgr.GetScheme(),
			Config:          cfg,
			Filter:          namespaceFilter,
			NamespaceLister: namespaceLister,
			GVK:             gvk,
		}

		if err = reconciler.SetupWithManagerForResourceType(mgr, gvk); err != nil {
			setupLog.Error(err, "unable to create controller",
				"resourceType", rt.String(),
			)
			os.Exit(1)
		}
	}

	setupLog.Info("registered controllers", "count", len(cfg.MirroredResourceTypes))

	// Add health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(signalCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
