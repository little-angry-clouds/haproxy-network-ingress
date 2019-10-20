/*
Copyright 2019 alexppg.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	littleangrycloudsv1 "github.com/little-angry-clouds/network-ingress-controller/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NetworkIngressReconciler reconciles a NetworkIngress object
type NetworkIngressReconciler struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=littleangryclouds.my.domain,resources=networkingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=littleangryclouds.my.domain,resources=networkingresses/status,verbs=get;update;patch

func (r *NetworkIngressReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("networkingress", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

func (r *NetworkIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&littleangrycloudsv1.NetworkIngress{}).
		Complete(r)
}
