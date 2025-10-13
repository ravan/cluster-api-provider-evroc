/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// Cluster condition types
const (
	// NetworkReadyCondition indicates the cluster network infrastructure (VPC, subnets) is ready
	NetworkReadyCondition clusterv1.ConditionType = "NetworkReady"

	// VPCReadyCondition indicates the VPC has been provisioned
	VPCReadyCondition clusterv1.ConditionType = "VPCReady"

	// SubnetsReadyCondition indicates all subnets have been provisioned
	SubnetsReadyCondition clusterv1.ConditionType = "SubnetsReady"
)

// EvrocClusterSpec defines the desired state of EvrocCluster
type EvrocClusterSpec struct {
	// The evroc region where the cluster will be deployed.
	// +kubebuilder:validation:Required
	Region string `json:"region"`

	// The evroc project (ResourceGroup) to deploy the cluster in.
	// +kubebuilder:validation:Required
	Project string `json:"project"`

	// The name of the Kubernetes secret containing the OIDC-authenticated
	// kubeconfig for accessing the evroc API.
	// +kubebuilder:validation:Required
	IdentitySecretName string `json:"identitySecretName"`

	// The endpoint for the Kubernetes API server.
	// This is managed by the provider and set in the status.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// Defines the networking configuration for the cluster.
	// +kubebuilder:validation:Required
	Network EvrocNetworkSpec `json:"network"`
}

// EvrocNetworkSpec defines the networking configuration for the cluster.
type EvrocNetworkSpec struct {
	// The Virtual Private Cloud configuration.
	// +kubebuilder:validation:Required
	VPC EvrocVPCSpec `json:"vpc"`

	// A list of subnets to create within the VPC. At least one is required.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Subnets []EvrocSubnetSpec `json:"subnets"`
}

// EvrocVPCSpec defines the Virtual Private Cloud configuration.
type EvrocVPCSpec struct {
	// The name of the VirtualPrivateCloud resource to be created.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// EvrocSubnetSpec defines a subnet to create within the VPC.
type EvrocSubnetSpec struct {
	// The name of the Subnet resource.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// The IPv4 CIDR block for the subnet (e.g., "10.0.1.0/24").
	// +kubebuilder:validation:Required
	CIDRBlock string `json:"cidrBlock"`
}

// EvrocClusterStatus defines the observed state of EvrocCluster
type EvrocClusterStatus struct {
	// Ready indicates whether the cluster infrastructure is ready.
	// +optional
	Ready bool `json:"ready"`

	// Network is the status of the provisioned networking resources.
	// +optional
	Network EvrocNetworkStatus `json:"network,omitempty"`

	// ControlPlanePublicIPName is the name of the PublicIP resource allocated for the control plane.
	// This is pre-allocated during cluster reconciliation to provide a stable endpoint.
	// +optional
	ControlPlanePublicIPName string `json:"controlPlanePublicIPName,omitempty"`

	// FailureReason will be set in case of a terminal problem
	// and will contain a short value suitable for machine interpretation.
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage will be set in case of a terminal problem
	// and will contain a long user-readable message.
	// +optional
	FailureMessage string `json:"failureMessage,omitempty"`

	// Conditions defines current service state of the EvrocCluster.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

// EvrocNetworkStatus describes the status of the provisioned network.
type EvrocNetworkStatus struct {
	// The status of the VPC.
	// +optional
	VPC EvrocVPCStatus `json:"vpc,omitempty"`

	// The status of the subnets.
	// +optional
	Subnets []EvrocSubnetStatus `json:"subnets,omitempty"`
}

// EvrocVPCStatus describes the status of a VPC.
type EvrocVPCStatus struct {
	// The name of the provisioned VPC.
	Name string `json:"name"`

	// True if the VPC is ready.
	Ready bool `json:"ready"`
}

// EvrocSubnetStatus describes the status of a Subnet.
type EvrocSubnetStatus struct {
	// The name of the provisioned Subnet.
	Name string `json:"name"`
	// The unique ID of the subnet.
	ID string `json:"id"`
	// The CIDR block of the subnet.
	CIDRBlock string `json:"cidrBlock"`
	// True if the Subnet is ready.
	Ready bool `json:"ready"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=evrocclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this EvrocCluster belongs"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Cluster infrastructure is ready"
// +kubebuilder:printcolumn:name="VPC",type="string",JSONPath=".status.network.vpc.name",description="VPC name"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.controlPlaneEndpoint.host",description="API Endpoint",priority=1

// EvrocCluster is the Schema for the evrocclusters API
type EvrocCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EvrocClusterSpec   `json:"spec,omitempty"`
	Status EvrocClusterStatus `json:"status,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (c *EvrocCluster) GetConditions() clusterv1.Conditions {
	return c.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (c *EvrocCluster) SetConditions(conditions clusterv1.Conditions) {
	c.Status.Conditions = conditions
}

//+kubebuilder:object:root=true

// EvrocClusterList contains a list of EvrocCluster
type EvrocClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EvrocCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EvrocCluster{}, &EvrocClusterList{})
}
