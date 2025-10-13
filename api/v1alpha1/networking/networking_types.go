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

package networking

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VirtualPrivateCloudSpec defines the desired state of VirtualPrivateCloud
type VirtualPrivateCloudSpec struct{}

// VirtualPrivateCloudStatus defines the observed state of VirtualPrivateCloud
type VirtualPrivateCloudStatus struct{}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VirtualPrivateCloud is the Schema for the virtualprivateclouds API
type VirtualPrivateCloud struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VirtualPrivateCloudSpec   `json:"spec,omitempty"`
	Status VirtualPrivateCloudStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VirtualPrivateCloudList contains a list of VirtualPrivateCloud
type VirtualPrivateCloudList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualPrivateCloud `json:"items"`
}

// SubnetSpec defines the desired state of Subnet
type SubnetSpec struct {
	VpcRef        VpcRef        `json:"vpcRef"`
	Ipv4CidrBlock Ipv4CidrBlock `json:"ipv4CidrBlock"`
}

type VpcRef struct {
	Name string `json:"name"`
}

type Ipv4CidrBlock struct {
	Block string `json:"block"`
}

// SubnetStatus defines the observed state of Subnet
type SubnetStatus struct{}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Subnet is the Schema for the subnets API
type Subnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubnetSpec   `json:"spec,omitempty"`
	Status SubnetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SubnetList contains a list of Subnet
type SubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subnet `json:"items"`
}

// PublicIPSpec defines the desired state of PublicIP
type PublicIPSpec struct{}

// PublicIPStatus defines the observed state of PublicIP
type PublicIPStatus struct {
	PublicIPv4Address string `json:"publicIPv4Address,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PublicIP is the Schema for the publicips API
type PublicIP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PublicIPSpec   `json:"spec,omitempty"`
	Status PublicIPStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PublicIPList contains a list of PublicIP
type PublicIPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PublicIP `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualPrivateCloud{}, &VirtualPrivateCloudList{}, &Subnet{}, &SubnetList{}, &PublicIP{}, &PublicIPList{})
}
