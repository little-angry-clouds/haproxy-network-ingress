/*
Copyright 2019 Little Angry Clouds Inc.
*/

package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	networkingressv1 "github.com/little-angry-clouds/haproxy-network-ingress/api/v1"
	helpers "github.com/little-angry-clouds/haproxy-network-ingress/controllers/helpers"
	"hash/fnv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"math/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
	"strconv"
	"time"
)

var ctx context.Context = context.Background()

// NetworkIngressReconciler stores the arguments passed to the program, main logger and main api client.
type NetworkIngressReconciler struct {
	client.Client
	Log                   logr.Logger
	ConfigmapName         string
	BackendDeploymentName string
	NetworkIngressClass   string
}

// NetworkIngressOperationRequest is the object passed to all functions. It contains the NetworkIngressReconciler and also the event request.
type NetworkIngressOperationRequest struct {
	ApiClient *NetworkIngressReconciler
	Request   ctrl.Request
}

func createConfigmap(op NetworkIngressOperationRequest, configmapName types.NamespacedName) (*corev1.ConfigMap, error) {
	var err error
	var configmap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configmapName.Name,
			Namespace: configmapName.Namespace,
		},
	}
	if err = op.ApiClient.Get(ctx, configmapName, configmap); err != nil {
		if err = op.ApiClient.Create(ctx, configmap); err != nil {
			return nil, err
		}
	}
	return configmap, err
}

func updateConfigmap(op NetworkIngressOperationRequest, configMapName types.NamespacedName) error {
	var networkIngresses networkingressv1.NetworkIngressList
	var emptyData = make(map[string]string)
	var itemLogger logr.Logger
	log := op.ApiClient.Log.WithValues("request", op.Request.NamespacedName).WithValues("function", "updateConfigmap")
	// Ensure configmap exists
	configmap, err := createConfigmap(op, configMapName)
	if err != nil {
		return err
	}
	emptyData["haproxy.cfg"] = ""
	configmap.Data = emptyData
	// Haproxy's deployment healthcheck and sane defaults
	configmap.Data["haproxy.cfg"] = "global\n # Log to stdout\n log stdout format raw local0 info\ndefaults\n # never fail on address resolution\n default-server init-addr last,libc,none\n# healthz\n frontend healthz\n mode http\n monitor-uri /healthz\n bind *:80\n timeout client 50000ms"
	if err := op.ApiClient.List(ctx, &networkIngresses, client.InNamespace(op.Request.Namespace)); err != nil {
		return err
	}
	var configmapPart string
	for _, item := range networkIngresses.Items {
		// Modify only the assigned network ingress class
		if item.Labels["kubernetes.io/network-ingress.class"] == op.ApiClient.NetworkIngressClass {
			itemLogger = log.WithValues("network-ingress", item)
			for _, rule := range item.Spec.Rules {
				s := strconv.Itoa(rule.TargetPort)
				rule.Port = hash(rule.Name + rule.Host + s)
				id := fmt.Sprintf("%s:%d:%d", rule.Name, rule.Port, rule.TargetPort)
				configmapPart = configmap.Data["haproxy.cfg"] + fmt.Sprintf("\n\n# beginning of %s\nfrontend %s\n  bind *:%d\n  option tcplog\n  log stdout format raw local0 info\n  mode tcp\n  use_backend %s\n  timeout client 50000ms\n\nbackend %s\n  mode tcp\n  server %s %s:%d\n  timeout connect 5000ms\n  timeout server 50000ms\n# end of %s", id, id, rule.Port, id, id, rule.Name, rule.Host, rule.TargetPort, id)
				configmap.Data["haproxy.cfg"] = configmapPart
				itemLogger.V(2).Info("network-ingress configuration", "value", configmapPart)
			}
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

func updateServicePorts(op NetworkIngressOperationRequest, backendServicePorts []corev1.ServicePort, networkIngressClass string, backenDeploymentName string) ([]string, error) {
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
		return modifiedServices, err
	}
	// update every service with its ports (or create them if they don't exist)
	for _, item := range desiredBackendServiceList.Items {
		modifiedServices = append(modifiedServices, item.Namespace+"/"+item.Name)
		labels := make(map[string]string)
		// TODO this label should be set with it's ningress, not the one that makes the request
		// labels["kubernetes.io/network-ingress.name"] = op.Request.Name
		labels["kubernetes.io/network-ingress.class"] = networkIngressClass
		item.Labels = labels
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
				return modifiedServices, err
			}
			log.V(1).Info("service created", "service", item)
			log.Info("service created")
		} else {
			log.V(1).Info("service updated", "service", item)
			log.Info("service updated")
		}
	}
	log.V(1).Info("modified services", "modified-services", modifiedServices)
	return modifiedServices, nil
}

func updateBackendPorts(op NetworkIngressOperationRequest, backendDeploymentPorts []corev1.ContainerPort, backendDeploymentName types.NamespacedName) error {
	var backendDeployment appsv1.Deployment
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get backend's deployment
		if err := op.ApiClient.Get(ctx, backendDeploymentName, &backendDeployment); err != nil {
			return err
		}
		// Sort deployment ports by name to not force a re-deploy when updating
		sort.Sort(helpers.ByName(backendDeploymentPorts))
		backendDeployment.Spec.Template.Spec.Containers[0].Ports = backendDeploymentPorts
		err := op.ApiClient.Update(ctx, &backendDeployment)
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

func hash(seed string) int {
	var dest int
	var min = 3000
	var max = 49151
	h := fnv.New32a()
	h.Write([]byte(seed))
	rand.Seed(int64(h.Sum32()))
	dest = rand.Intn(max-min) + min
	return dest
}

func updatePorts(op NetworkIngressOperationRequest, backendDeploymentName types.NamespacedName) ([]string, error) {
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
	if err := op.ApiClient.List(ctx, &networkIngresses, client.InNamespace(op.Request.Namespace), client.MatchingLabels{"kubernetes.io/network-ingress.class": op.ApiClient.NetworkIngressClass}); err != nil {
		return nil, err
	}
	for _, item := range networkIngresses.Items {
		itemLogger = log.WithValues("network-ingress", item.Name)
		for _, rule := range item.Spec.Rules {
			s := strconv.Itoa(rule.TargetPort)
			randomPort := hash(rule.Name + rule.Host + s)
			rulesLogger = itemLogger.WithValues("rule", rule)
			containerPort.Name = rule.Name
			containerPort.Protocol = corev1.ProtocolTCP
			containerPort.ContainerPort = int32(randomPort)
			backendDeploymentPorts = append(backendDeploymentPorts, containerPort)
			rulesLogger.V(2).Info("", "container-port", containerPort)
			servicePort.Name = rule.Name
			servicePort.Port = int32(rule.Port)
			servicePort.TargetPort = intstr.IntOrString{IntVal: int32(randomPort)}
			rulesLogger.V(2).Info("", "service-port", servicePort)
			backendServicePorts = append(backendServicePorts, servicePort)
		}
	}
	modifiedServices, err := updateServicePorts(op, backendServicePorts, networkIngressClass, backendDeploymentName.Name)
	if err != nil {
		return nil, err
	}
	log.Info("services updated correctly")
	err = updateBackendPorts(op, backendDeploymentPorts, backendDeploymentName)
	if err != nil {
		return nil, err
	}
	log.Info("deployment updated correctly")
	return modifiedServices, nil
}

func updateServices(op NetworkIngressOperationRequest, modifiedServices []string) error {
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
	deletableServices := helpers.GetMapDifferences(existentServicesNames, modifiedServices)
	log.V(1).Info("service names to delete", "service-name", deletableServices)
	for _, existingService := range existentServicesList.Items {
		for _, deletableService := range deletableServices {
			if existingService.Namespace+"/"+existingService.Name == deletableService {
				if err := op.ApiClient.Delete(ctx, &existingService); err != nil {
					return err
				}
				log.V(1).Info("service deleted", "service-name", existingService.Namespace+"/"+existingService.Name)
			}
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;update;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;watch;delete
// +kubebuilder:rbac:groups=little-angry-clouds.k8s.io,resources=networkingresses,verbs=get;list;watch;update

// Reconcile reconciles the event requests.
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
	var action string
	// See if updating or deleting NetworkIngress for logging purposes
	if err := op.ApiClient.Get(ctx, req.NamespacedName, &testNetworkIngress); err != nil {
		action = "delete"
	} else {
		action = "update"
		if testNetworkIngress.Labels["kubernetes.io/network-ingress.class"] == "" {
			testNetworkIngress.Labels = make(map[string]string)
			testNetworkIngress.Labels["kubernetes.io/network-ingress.class"] = "haproxy"
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				err := op.ApiClient.Update(ctx, &testNetworkIngress)
				return err
			})
			if err != nil {
				log.Error(err, "there was an error updating the network ingress")
				return ctrl.Result{}, err
			}
		}
		if op.ApiClient.NetworkIngressClass == testNetworkIngress.Labels["kubernetes.io/network-ingress.class"] {
			log.V(1).Info("the network-ingress class does match")
		} else {
			log.V(1).Info("the network-ingress class doesn't match", "network-ingress-class",
				testNetworkIngress.Labels["kubernetes.io/network-ingress.class"],
				"argument-class", op.ApiClient.NetworkIngressClass)
			return ctrl.Result{}, nil
		}
	}
	if action == "delete" {
		log.Info("deleting network-ingress")
	} else if action == "update" {
		log.Info("updating NetworkIngress")
	}
	err := updateConfigmap(op, configmapName)
	if err != nil {
		log.Error(err, "there was an error updating the configmap")
		return ctrl.Result{}, err
	}
	log.Info("configmap updated")
	modifiedServices, err := updatePorts(op, backendDeploymentName)
	if err != nil {
		log.Error(err, "there was an error updating the deployment and services ports")
		return ctrl.Result{}, err
	}
	log.Info("ports updated")
	err = updateServices(op, modifiedServices)
	if err != nil {
		log.Error(err, "there was an error deleting unused services")
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Second * 300}, nil
}

// SetupWithManager calls NetworkIngressReconciler
func (r *NetworkIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingressv1.NetworkIngress{}).
		Complete(r)
}
