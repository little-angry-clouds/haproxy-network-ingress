/*
Copyright 2019 alexppg.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NetworkIngressSpec struct {
	// A list of hosts and its associated ports used to configure the Network
	// Ingress.
	Rules []Rule `json:"rules"`
}

// A rule is the core of a Network Ingress . It defines name, host, port and target port of a rule
type Rule struct {
	// Name of the rule. This will be used as ID
	Name       string `json:"name"`
	// Host of the rule. This is the destination machine that Haproxy will conecct to.
	Host       string `json:"host"`
	// Port of the rule. This is the port that will be configured in the service.
	Port       int    `json:"port"`
	// Target port of the rule. This is the port that will be configured in the Haproxy' s configuration
	TargetPort int    `json:"targetPort"`
}

// Defines the observed state of NetworkIngress
type NetworkIngressStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=networkingresses,shortName=ningress;ning
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.spec.rules[].name`
// +kubebuilder:printcolumn:name="Port",type=integer,JSONPath=`.spec.rules[].port`
// +kubebuilder:printcolumn:name="TargetPort",type=integer,JSONPath=`.spec.rules[].targetPort`

// NetworkIngress is the Schema for the Network Ingress API
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
