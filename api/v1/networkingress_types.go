/*
Copyright 2019 alexppg.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type NetworkIngressSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	Rules []Rule `json:"rules"`
}

// TODO añadir descripción
type Rule struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort"`
}

// NetworkIngressStatus defines the observed state of NetworkIngress
type NetworkIngressStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true

// NetworkIngress is the Schema for the networkingresses API
type NetworkIngress struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkIngressSpec   `json:"spec,omitempty"`
	Status NetworkIngressStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NetworkIngressList contains a list of NetworkIngress
type NetworkIngressList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkIngress `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkIngress{}, &NetworkIngressList{})
}
