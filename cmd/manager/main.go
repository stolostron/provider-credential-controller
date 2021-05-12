// Copyright Contributors to the Open Cluster Management project.

package main

import (
	"flag"
	"os"

	"github.com/open-cluster-management/provider-credential-controller/controllers"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = corev1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	// To run in debug change zapcore.InfoLevel to zapcore.DebugLevel
	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.Level(zapcore.InfoLevel)))

	//re := regexp.MustCompile(`.*`)
	// /fmt.Printf("%q\n", re.FindString("seafood fool"))

	labelSet := map[string]string{
		//"cluster.open-cluster-management.io/cloudconnection": "",
		"cluster.open-cluster-management.io/cloudconnection": "",
		//controllers.CopiedFromNamespaceLabel: "open-cluster-management",
	}
	//labels.Parse()

	// str := fmt.Sprintf("%s,cluster.open-cluster-management.io/copiedFromNamespace notin (open-cluster-management)", controllers.ProviderLabel)
	// fmt.Println("str: ", str)
	// set, err := labels.Parse(str)
	// s, err := labels.ConvertSelectorToLabelsMap(controllers.ProviderLabel)
	// fmt.Println("s: ", s)
	//set1, err := labels.Parse("cluster.open-cluster-management.io/copiedFromNamespace")
	// fmt.Println("err: ", err)
	// fmt.Println("set: ", set)
	//labelSelector := fmt.Sprintf("%s, %s", controllers.ProviderLabel, controllers.CopiedFromNamespaceLabel)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		NewCache: cache.BuilderWithOptions(cache.Options{
			SelectorsByObject: cache.SelectorsByObject{
				&corev1.Secret{}: {
					Label: labels.SelectorFromSet(labelSet),
					//Label: labels.Selector.RequiresExactMatch(controllers.ProviderLabel),
					//Label: set,
				},
			},
		},
		),
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "provider-credential-controller.open-cluster-management.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.ProviderCredentialSecretReconciler{
		//Client: controllers.NewCustomClient(mgr.GetClient(), mgr.GetAPIReader()),
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Log:       ctrl.Log.WithName("controllers").WithName("ProviderCredentialSecretReconciler"),
		Scheme:    mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ProviderCredentialSecretReconciler")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
