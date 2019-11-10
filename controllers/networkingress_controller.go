/*
Copyright 2019 alexppg.
*/

package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	networkingressv1 "github.com/little-angry-clouds/haproxy-network-ingress/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
)

var ctx context.Context = context.Background()

// Se usa para hacer un sort de los puertos por nombre
type ByName []corev1.ContainerPort

func (a ByName) Len() int           { return len(a) }
func (a ByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool { return a[i].Name < a[j].Name }

// NetworkIngressReconciler reconciles a NetworkIngress object
type NetworkIngressReconciler struct {
	client.Client
	Log                   logr.Logger
	ConfigmapName         string
	BackendDeploymentName string
	NetworkIngressClass   string
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

type NetworkIngressOperationRequest struct {
	ApiClient *NetworkIngressReconciler
	Request   ctrl.Request
}

// Create configmap idempotently
func createConfigmap(op NetworkIngressOperationRequest, configmapName types.NamespacedName) (error, *corev1.ConfigMap) {
	var err error
	var configmap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configmapName.Name,
			Namespace: configmapName.Namespace,
		},
	}
	if err = op.ApiClient.Get(ctx, configmapName, configmap); err != nil {
		if err = op.ApiClient.Create(ctx, configmap); err != nil {
			return err, nil
		}
	}
	return err, configmap
}

// Update haproxy's configmap
func updateConfigmap(op NetworkIngressOperationRequest, configMapName types.NamespacedName) error {
	// var result NetworkIngressOperationResult
	var networkIngresses networkingressv1.NetworkIngressList
	var emptyData = make(map[string]string)
	var itemLogger logr.Logger

	log := op.ApiClient.Log.WithValues("request", op.Request.NamespacedName).WithValues("function", "updateConfigmap")

	// Ensure configmap existes
	err, configmap := createConfigmap(op, configMapName)
	if err != nil {
		return err
	}

	emptyData["haproxy.cfg"] = ""
	configmap.Data = emptyData

	// Haproxy's deployment healthcheck
	configmap.Data["haproxy.cfg"] = "defaults\n  # never fail on address resolution\n  default-server init-addr none\n\n# healthz\nfrontend healthz\n  mode http\n  monitor-uri /healthz\n  bind *:80\n  timeout connect 5000ms\n  timeout client 50000ms\n  timeout server 50000ms"

	if err := op.ApiClient.List(ctx, &networkIngresses, client.InNamespace(op.Request.Namespace)); err != nil {
		return err
	}

	var configmapPart string
	for _, item := range networkIngresses.Items {
		itemLogger = log.WithValues("network-ingress", item)
		for _, rule := range item.Spec.Rules {
			id := fmt.Sprintf("%s:%d:%d", rule.Name, rule.Port, rule.TargetPort)
			configmapPart = configmap.Data["haproxy.cfg"] + fmt.Sprintf("\n\n# begining of %s\nfrontend %s\n  bind *:%d\n  mode tcp\n  use_backend %s\n  timeout connect 5000ms\n  timeout client 50000ms\n  timeout server 50000ms\n\nbackend %s\n  mode tcp\n  server %s %s:%d\n  timeout connect 5000ms\n  timeout client 50000ms\n  timeout server 50000ms\n# end of %s", id, id, rule.Port, id, id, rule.Host, rule.Host, rule.TargetPort, id)
			configmap.Data["haproxy.cfg"] = configmapPart
			itemLogger.V(2).Info("network-ingress configuration", "value", configmapPart)
		}
	}

	log.V(1).Info("configmap value", "value", configmap.Data["haproxy.cfg"])

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := op.ApiClient.Update(ctx, configmap)
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

// Update service ports
// Return error and list of modified services
func updateServicePorts(op NetworkIngressOperationRequest, backendServicePorts []corev1.ServicePort, networkIngressClass string, backenDeploymentName string) (error, []string) {
	var desiredBackendServiceList corev1.ServiceList
	var actualBackendServiceList corev1.ServiceList
	var backendService corev1.Service
	var service corev1.Service
	var modifiedServices []string

	log := op.ApiClient.Log.WithValues("request", op.Request.NamespacedName).WithValues("function", "updateServicePorts")

	// create service's list of ports
	for _, ports := range backendServicePorts {
		backendService.Name = ports.Name
		backendService.Namespace = op.Request.Namespace
		backendService.Spec.Ports = []corev1.ServicePort{ports}
		desiredBackendServiceList.Items = append(desiredBackendServiceList.Items, backendService)
		log.V(2).Info("desired backend service", "service", backendService)
	}

	// list all services created by the controller
	if err := op.ApiClient.List(ctx, &actualBackendServiceList, client.InNamespace(op.Request.Namespace), client.MatchingLabels{"kubernetes.io/network-ingress.class": networkIngressClass}); err != nil {
		return err, modifiedServices
	}

	// update every service with its ports (or create them if they don't exist)
	for _, item := range desiredBackendServiceList.Items {
		modifiedServices = append(modifiedServices, item.Namespace+"/"+item.Name)
		labels := make(map[string]string)
		labels["kubernetes.io/network-ingress.name"] = op.Request.Name
		labels["kubernetes.io/network-ingress.class"] = networkIngressClass
		item.Labels = labels
		// set selector
		selector := make(map[string]string)
		selector["app"] = backenDeploymentName
		item.Spec.Selector = selector
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			err := op.ApiClient.Get(ctx, types.NamespacedName{Name: item.Name, Namespace: item.Namespace}, &service)
			if err != nil {
				return err
			}
			service.Spec.Ports = item.Spec.Ports
			err = op.ApiClient.Update(ctx, &service)
			if err != nil {
				return err
			}
			return nil
		})

		if err != nil {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				err := op.ApiClient.Create(ctx, &item)
				return err
			})
			if err != nil {
				log.Info("unable to create service")
				return err, modifiedServices
			} else {
				log.V(1).Info("service created", "service", item)
				log.Info("service created")
			}
		} else {
			log.V(1).Info("service updated", "service", item)
			log.Info("service updated")
		}
	}
	log.V(1).Info("modified services", "modified-services", modifiedServices)
	return nil, modifiedServices
}

// Update service ports
// Return error and list of modified services
func updateBackendPorts(op NetworkIngressOperationRequest, backendDeploymentPorts []corev1.ContainerPort, backendDeploymentName types.NamespacedName) error {
	var backendDeployment appsv1.Deployment

	log := op.ApiClient.Log.WithValues("networkingress", op.Request.NamespacedName)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get backend's deployment
		if err := op.ApiClient.Get(ctx, backendDeploymentName, &backendDeployment); err != nil {
			log.Error(err, fmt.Sprintf("Unable to get %s deployment", backendDeployment.Name))
		}
		// Sort deployment ports by name to not force a re-deploy when updating
		sort.Sort(ByName(backendDeploymentPorts))
		backendDeployment.Spec.Template.Spec.Containers[0].Ports = backendDeploymentPorts
		err := op.ApiClient.Update(ctx, &backendDeployment)
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

// Update service and deployment ports
func updatePorts(op NetworkIngressOperationRequest, backendDeploymentName types.NamespacedName) (error, []string) {
	var networkIngresses networkingressv1.NetworkIngressList
	var containerPort corev1.ContainerPort
	var servicePort corev1.ServicePort
	var backendServicePorts []corev1.ServicePort
	var itemLogger logr.Logger
	var rulesLogger logr.Logger
	var backendDeploymentPorts = []corev1.ContainerPort{
		{
			Name:          "healthz",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 80,
		},
	}
	var networkIngressClass = op.ApiClient.NetworkIngressClass

	log := op.ApiClient.Log.WithValues("request", op.Request.NamespacedName).WithValues("function", "updatePorts")

	if err := op.ApiClient.List(ctx, &networkIngresses, client.InNamespace(op.Request.Namespace)); err != nil {
		log.Error(err, "Unable to list all NetworkIngress")
	}

	for _, item := range networkIngresses.Items {
		itemLogger = log.WithValues("network-ingress", item.Name)
		for _, rule := range item.Spec.Rules {
			rulesLogger = itemLogger.WithValues("rule", rule)
			containerPort.Name = rule.Name
			containerPort.Protocol = corev1.ProtocolTCP
			containerPort.ContainerPort = int32(rule.Port)
			backendDeploymentPorts = append(backendDeploymentPorts, containerPort)
			rulesLogger.V(2).Info("", "container-port", containerPort)

			servicePort.Name = rule.Name
			servicePort.Port = int32(rule.Port)
			servicePort.TargetPort = intstr.IntOrString{IntVal: int32(rule.TargetPort)}
			rulesLogger.V(2).Info("", "service-port", servicePort)
			backendServicePorts = append(backendServicePorts, servicePort)
		}
	}

	err, modifiedServices := updateServicePorts(op, backendServicePorts, networkIngressClass, backendDeploymentName.Name)
	if err != nil {
		log.Error(err, "there was an error updating the service ports")
	} else {
		log.Info("services updated correctly")
	}

	err = updateBackendPorts(op, backendDeploymentPorts, backendDeploymentName)
	if err != nil {
		log.Error(err, "there was an error updating the backend ports")
	} else {
		log.Info("deployment updated correctly")
	}

	return nil, modifiedServices
}

// delete unused services
// compare previously modified services with the existent services
// if there's a service which wasn't modified in a previous step, it means it
// doesn't have an associated NetworkIngress
func deleteUnusedServices(op NetworkIngressOperationRequest, modifiedServices []string) error {
	var existentServicesNames []string
	var existentServicesList corev1.ServiceList
	var networkIngressClass = op.ApiClient.NetworkIngressClass

	log := op.ApiClient.Log.WithValues("request", op.Request.NamespacedName).WithValues("function", "deleteUnusedServices")

	if err := op.ApiClient.List(ctx, &existentServicesList, client.InNamespace(op.Request.Namespace), client.MatchingLabels{"kubernetes.io/network-ingress.class": networkIngressClass}); err != nil {
		return err
	}

	for _, service := range existentServicesList.Items {
		existentServicesNames = append(existentServicesNames, service.Namespace+"/"+service.Name)
	}
	log.V(1).Info("modified service names", "service-name", modifiedServices)
	log.V(1).Info("existent service names", "service-name", existentServicesNames)
	deletableServices := difference(existentServicesNames, modifiedServices)
	log.V(1).Info("service names to delete", "service-name", deletableServices)

	for _, existingService := range existentServicesList.Items {
		for _, deletableService := range deletableServices {
			if existingService.Namespace+"/"+existingService.Name == deletableService {
				if err := op.ApiClient.Delete(ctx, &existingService); err != nil {
					return err
				} else {
					log.V(1).Info("service deleted", "service-name", existingService.Namespace+"/"+existingService.Name)
				}
			}
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;update;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;watch;delete
// +kubebuilder:rbac:groups=networkingress.little-angry-clouds.k8s.io,resources=networkingresses,verbs=get;list;watch

func (r *NetworkIngressReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	// Initial setup
	var op NetworkIngressOperationRequest
	var testNetworkIngress networkingressv1.NetworkIngress
	op.ApiClient = r
	op.Request = req
	log := op.ApiClient.Log.WithValues("request", op.Request.NamespacedName).WithValues("function", "Reconciler")

	// Assume that all happens in the event request namespace
	namespace := op.Request.Namespace
	configmapName := types.NamespacedName{Name: op.ApiClient.ConfigmapName, Namespace: namespace}
	backendDeploymentName := types.NamespacedName{Name: op.ApiClient.BackendDeploymentName, Namespace: namespace}

	// See if updating or deleting NetworkIngress for logging purposes
		log.Info("deleting network-ingress")
	if err := op.ApiClient.Get(ctx, req.NamespacedName, &testNetworkIngress); err != nil {
	} else {
		log.Info("updating NetworkIngress")
	}

	// Update configmap
	err := updateConfigmap(op, configmapName)
	if err != nil {
		log.Error(err, "there was an error updating the configmap")
		return ctrl.Result{}, err
	} else {
		log.Info("configmap updated")
	}

	// update services's and backend's ports
	err, modifiedServices := updatePorts(op, backendDeploymentName)
	if err != nil {
		log.Error(err, "there was an error updating the deployment and services ports")
		return ctrl.Result{}, err

	} else {
		log.Info("ports updated")
	}

	err = deleteUnusedServices(op, modifiedServices)
	if err != nil {
		log.Error(err, "there was an error deleting unused services")
		return ctrl.Result{}, err
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
		For(&networkingressv1.NetworkIngress{}).
		Complete(r)
}
