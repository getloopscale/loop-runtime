package main

import (
	"flag"
	"os" // used for Exit and Getenv

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/controller"
	"github.com/loopzero/loopzero/operators/internal/gitea"
)

var scheme = runtime.NewScheme()

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = loopzerov1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var healthProbeAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&healthProbeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	opts := zap.Options{Development: false}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: healthProbeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "loopzero-operators.loopzero.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Real provisioners — NO fakes.
	giteaURL := os.Getenv("GITEA_URL")
	if giteaURL == "" {
		giteaURL = "http://gitea-http.loopzero-system.svc.cluster.local:3000"
	}
	giteaToken := os.Getenv("GITEA_TOKEN")
	gitProvisioner := gitea.NewRealProvisioner(giteaURL, giteaToken, "loopzero")

	argoCDNamespace := os.Getenv("ARGOCD_NAMESPACE")
	if argoCDNamespace == "" {
		argoCDNamespace = "argocd"
	}
	argoCDProvisioner, err := argocd.NewRealProvisioner(argoCDNamespace)
	if err != nil {
		setupLog.Error(err, "unable to create ArgoCD provisioner")
		os.Exit(1)
	}

	if err := controller.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner, giteaURL); err != nil {
		setupLog.Error(err, "unable to setup TenantController")
		os.Exit(1)
	}

	if err := controller.SetupClusterCredentialController(mgr); err != nil {
		setupLog.Error(err, "unable to setup ClusterCredentialController")
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

	setupLog.Info("starting manager", "leaderElection", enableLeaderElection)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
