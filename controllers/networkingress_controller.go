/*
Copyright 2019 alexppg.
*/

package controllers

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	littleangrycloudsv1 "github.com/little-angry-clouds/network-ingress-controller/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Se usa para hacer un sort de los puertos por nombre
type ByName []corev1.ContainerPort

func (a ByName) Len() int           { return len(a) }
func (a ByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool { return a[i].Name < a[j].Name }

// NetworkIngressReconciler reconciles a NetworkIngress object
type NetworkIngressReconciler struct {
	client.Client
	Log logr.Logger
}

// We generally want to ignore (not requeue) NotFound errors, since we’ll get a
// reconciliation request once the object exists, and requeuing in the meantime
// won’t help.
func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

// Source: https://www.socketloop.com/tutorials/golang-check-if-item-is-in-slice-array
func intInSlice(int int, list []int) bool {
	for _, v := range list {
		if v == int {
			return true
		}
	}
	return false
}

// TODO ajustar permisos
// +kubebuilder:rbac:groups=core,resources=configmap,verbs=get;list;create;update
// +kubebuilder:rbac:groups=core,resources=configmap/status,verbs=get

// +kubebuilder:rbac:groups=littleangryclouds.little-angry-clouds.k8s.io,resources=networkingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=littleangryclouds.little-angry-clouds.k8s.io,resources=networkingresses/status,verbs=get;update;patch

func (r *NetworkIngressReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("networkingress", req.NamespacedName)
	var testNetworkIngress littleangrycloudsv1.NetworkIngress
	if err := r.Get(ctx, req.NamespacedName, &testNetworkIngress); err != nil {
		log.Info(fmt.Sprintf("Deleting %s NetworkIngress", req.NamespacedName))
	} else {
		log.Info(fmt.Sprintf("Updating %s NetworkIngress", req.NamespacedName))
	}

	// create configmap if it doesn't exist
	// TODO deshardcodeadr nombre y namespace
	var ningressConfigMapName types.NamespacedName
	ningressConfigMapName.Namespace = req.NamespacedName.Namespace
	ningressConfigMapName.Name = "networkingress-configuration"
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "networkingress-configuration",
			Namespace: req.NamespacedName.Namespace,
		},
	}
	if err := r.Get(ctx, types.NamespacedName{Name: "networkingress-configuration", Namespace: req.NamespacedName.Namespace}, configMap); err != nil {
		if err := r.Create(ctx, configMap); err != nil {
			log.Error(err, fmt.Sprintf("Unable to create %s configmap", req.NamespacedName))
			return ctrl.Result{}, err
		}
	}


	// list all ningresses
	// TODO filtrar por algo
	var networkIngresses littleangrycloudsv1.NetworkIngressList
	if err := r.List(ctx, &networkIngresses); err != nil {
		log.Error(err, "Unable to list all NetworkIngress")
	}

	// get the content of all ningress to:
	// - update haproxy's configmap
	// - update haproxy's deployment ports
	// - update haproxy-s  service ports
	// TODO desharcodear nombre del deploy de haproxy
	emptyData := make(map[string]string)
	emptyData["haproxy.cfg"] = ""
	configMap.Data = emptyData
	configMap.Data["haproxy.cfg"] = "# healthz\nfrontend healthz\n  mode http\n  monitor-uri /healthz\n  bind *:80"
	var networkIngress littleangrycloudsv1.NetworkIngress
	var haproxyDeployment appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: "network-ingress-haproxy-backend", Namespace: "default"}, &haproxyDeployment); err != nil {
		log.Error(err, fmt.Sprintf("Unable to get %s deployment", haproxyDeployment.Name))
	}

	haproxyPorts := []corev1.ContainerPort{
		{
			Name:          "healthz",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 80,
		},
	}
	// TODO testNetworkIngress ya esta listando el ningress. hacemos algo con eso?
	var haproxyServicePorts []corev1.ServicePort
	var containerPort corev1.ContainerPort
	var servicePort corev1.ServicePort
	for _, networkIngressItem := range networkIngresses.Items {
		log.Info(networkIngressItem.Name)
		if err := r.Get(ctx, types.NamespacedName{Name: networkIngressItem.Name, Namespace: networkIngressItem.Namespace}, &networkIngress); err != nil {
			log.Info("unable to get NetworkIngress")
		}
		for _, rule := range networkIngressItem.Spec.Rules {
			id := fmt.Sprintf("%s:%d:%d", rule.Name, rule.Port, rule.TargetPort)
			configMap.Data["haproxy.cfg"] = configMap.Data["haproxy.cfg"] + fmt.Sprintf("\n\n# begining of %s\nfrontend %s\n  bind *:%d\n  mode tcp\n  use_backend %s\n\nbackend %s\n  mode tcp\n  server %s %d:%d\n# end of %s",
				id, id, rule.Port, id, id, rule.Host, rule.Port, rule.TargetPort, id)
			containerPort.Name = rule.Name
			containerPort.Protocol = corev1.ProtocolTCP
			containerPort.ContainerPort = int32(rule.Port)
			haproxyPorts = append(haproxyPorts, containerPort)

			// Los servicePort se usaran mas adelante
			servicePort.Name = rule.Name
			servicePort.Port = int32(rule.Port)
			servicePort.TargetPort = intstr.IntOrString{IntVal: int32(rule.TargetPort)}
			haproxyServicePorts = append(haproxyServicePorts, servicePort)
		}
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Update(ctx, configMap)
		return err
	})
	if retryErr != nil {
		log.Error(retryErr, "unable to update configmap")
		return ctrl.Result{}, ignoreNotFound(retryErr)
	} else {
		log.Info("configmap updated")
	}

	sort.Sort(ByName(haproxyPorts))
	haproxyDeployment.Spec.Template.Spec.Containers[0].Ports = haproxyPorts
	retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Update(ctx, &haproxyDeployment)
		return err
	})
	if retryErr != nil {
		log.Error(retryErr, "unable to update deployment")
		return ctrl.Result{}, ignoreNotFound(retryErr)
	} else {
		log.Info("deployment updated")
	}

	// build a servicelist struct
	var desiredHaproxyServiceList corev1.ServiceList
	var haproxyService corev1.Service
	haproxyService.Labels = map[string]string{"NetworkIngress": "true"}
	for _, ports := range haproxyServicePorts {
		haproxyService.Name = ports.Name
		haproxyService.Namespace = "default"
		haproxyService.Spec.Ports = []corev1.ServicePort{ports}
		desiredHaproxyServiceList.Items = append(desiredHaproxyServiceList.Items, haproxyService)
	}

	// list all services created by the controller
	var actualHaproxyServiceList corev1.ServiceList
	// TODO el controller tiene que apuntar al netowrkingress, no asi de forma generica
	if err := r.List(ctx, &actualHaproxyServiceList, client.InNamespace(req.Namespace), client.MatchingLabels{"NetworkIngress": "true"}); err != nil {
		log.Error(err, "unable to list services")
	}

	// var desiredPorts []corev1.ServicePort
	var service corev1.Service
	var updatedService []string
	for _, item := range desiredHaproxyServiceList.Items {
		updatedService = append(updatedService, item.Name)
		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			err := r.Get(ctx, types.NamespacedName{Name: item.Name, Namespace: item.Namespace}, &service)
			service.Spec.Ports = item.Spec.Ports
			err = r.Update(ctx, &service)
			return err
		})
		if retryErr != nil {
			retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				err := r.Create(ctx, &item)
				return err
			})
			if retryErr != nil {
				log.Info("unable to create service")
				return ctrl.Result{}, ignoreNotFound(retryErr)
			} else {
				log.Info("service created")
			}
		} else {
			log.Info("service updated")
		}
	}
	// TODO hacer que borre los servicios
	// implementarlo usando updatedService
	// hacer funcion que saque los servicios que no estan en updatedService
	var actualService []string
	for _, service := range actualHaproxyServiceList.Items {
		actualService = append(actualService, service.Name)
	}
	deletable := difference(actualService, updatedService)

	for _, service := range actualHaproxyServiceList.Items {
		for _, delete := range deletable {
			if service.Name == delete {
				if err := r.Delete(ctx, &service); err != nil {
					log.Error(err, "unable to delete services")
				} else {
					log.Info("service deleted")
				}
			}
		}
	}
	return ctrl.Result{}, nil
}

// Source: https://stackoverflow.com/questions/19374219/how-to-find-the-difference-between-two-slices-of-strings-in-golang
func difference(a, b []string) []string {
	mb := make(map[string]struct{}, len(b))
	for _, x := range b {
		mb[x] = struct{}{}
	}
	var diff []string
	for _, x := range a {
		if _, found := mb[x]; !found {
			diff = append(diff, x)
		}
	}
	return diff
}
func (r *NetworkIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&littleangrycloudsv1.NetworkIngress{}).
		Complete(r)
}
