/*
Copyright 2019 Little Angry Clouds Inc.
*/

package main

import (
	"flag"
	"fmt"
	"os"

	networkingressv1 "github.com/little-angry-clouds/haproxy-network-ingress/api/v1"
	"github.com/little-angry-clouds/haproxy-network-ingress/controllers"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = networkingressv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var configmapName string
	var backendDeploymentName string
	var networkIngressClass string
	var electionID string

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&configmapName, "configmap-name", "network-ingress-configuration", "Backend's configmap name.")
	flag.StringVar(&backendDeploymentName, "backend-name", "network-ingress-backend", "Backend's deployment name.")
	flag.StringVar(&networkIngressClass, "network-ingress-class", "haproxy", "Name of the network ingress class.")
	flag.Parse()
	ctrl.SetLogger(zap.Logger(true))

	electionID = fmt.Sprintf("%v-%v", networkIngressClass, "network-ingress-controller-leader")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   electionID,
		Port:               9443,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.NetworkIngressReconciler{
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controllers").WithName("NetworkIngress"),
		ConfigmapName:         configmapName,
		BackendDeploymentName: backendDeploymentName,
		NetworkIngressClass:   networkIngressClass,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NetworkIngress")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
