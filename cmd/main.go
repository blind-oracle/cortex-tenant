package main

import (
	"flag"
	"os"

	_ "github.com/KimMachineGun/automemlimit"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	_ "go.uber.org/automaxprocs"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/projectcapsule/cortex-tenant/internal/config"
	"github.com/projectcapsule/cortex-tenant/internal/controllers"
	"github.com/projectcapsule/cortex-tenant/internal/metrics"
	"github.com/projectcapsule/cortex-tenant/internal/processor"
	"github.com/projectcapsule/cortex-tenant/internal/stores"
)

var Version string

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

//nolint:wsl
func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr, cfgFile string

	var enablePprof bool

	var probeAddr string

	ctx := ctrl.SetupSignalHandler()

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8081", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":10080", "The address the probe endpoint binds to.")
	flag.BoolVar(&enablePprof, "enable-pprof", false, "Enables Pprof endpoint for profiling (not recommend in production)")
	flag.StringVar(&cfgFile, "config", "", "Path to a config file")

	opts := zap.Options{
		Development: true,
	}

	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		setupLog.Error(err, "unable to load config")
		os.Exit(1)
	}

	setupLog.Info("loaded config", "config", cfg)

	ctrlConfig := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
	}

	if enablePprof {
		ctrlConfig.PprofBindAddress = ":8082"
	}

	setupLog.Info("initializing manager")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrlConfig)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	directClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: mgr.GetScheme(),
		Mapper: mgr.GetRESTMapper(),
	})
	if err != nil {
		setupLog.Error(err, "unable to initialize client")
		os.Exit(1)
	}

	store := stores.NewTenantStore()
	metricsRecorder := metrics.MustMakeRecorder()

	tenants := &controllers.TenantController{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		// Log:     ctrl.Log.WithName("Store").WithName("Config"),
		Metrics: metricsRecorder,
		Store:   store,
	}

	if err = tenants.Init(ctx, directClient); err != nil {
		setupLog.Error(err, "unable to initialize settings")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	proc := processor.NewProcessor(ctrl.Log.WithName("processor"), *cfg, store, metricsRecorder)
	if err := mgr.Add(proc); err != nil {
		setupLog.Error(err, "unable to add processor to manager")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
