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

package compute

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VirtualMachineSpec defines the desired state of VirtualMachine
type VirtualMachineSpec struct {
	Running               bool                  `json:"running,omitempty"`
	VMVirtualResourcesRef VMVirtualResourcesRef `json:"vmVirtualResourcesRef"`
	DiskRefs              []DiskRef             `json:"diskRefs"`
	OSSettings            *VMOSSettings         `json:"osSettings,omitempty"`
	Networking            *VMNetworkingSettings `json:"networking,omitempty"`
}

type VMVirtualResourcesRef struct {
	VMVirtualResourcesRefName string `json:"vmVirtualResourcesRefName"`
}

type DiskRef struct {
	Name     string `json:"name"`
	BootFrom bool   `json:"bootFrom"`
}

type VMOSSettings struct {
	CloudInitUserData string         `json:"cloudInitUserData,omitempty"`
	SSH               *VMSSHSettings `json:"ssh,omitempty"`
}

type VMSSHSettings struct {
	AuthorizedKeys []VMAuthorizedKey `json:"authorizedKeys,omitempty"`
}

type VMAuthorizedKey struct {
	Value string `json:"value"`
}

type VMNetworkingSettings struct {
	PublicIPv4Address *VMPublicIPv4AddressSettings `json:"publicIPv4Address,omitempty"`
	SecurityGroups    *SecurityGroupSettings       `json:"securityGroups,omitempty"`
}

type SecurityGroupSettings struct {
	SecurityGroupMemberships []SecurityGroupMembershipRef `json:"securityGroupMemberships,omitempty"`
}

type SecurityGroupMembershipRef struct {
	Name string `json:"name"`
}

type VMPublicIPv4AddressSettings struct {
	Static *VMStaticPublicIPv4AddressSettings `json:"static,omitempty"`
}

type VMStaticPublicIPv4AddressSettings struct {
	PublicIPRef string `json:"publicIPRef"`
}

// VirtualMachineStatus defines the observed state of VirtualMachine
type VirtualMachineStatus struct {
	// The status of the VM (e.g., "Running", "Stopped", "Creating")
	VirtualMachineStatus string `json:"virtualMachineStatus,omitempty"`

	// The current status of the networking set up on the VM
	Networking VMNetworkStatus `json:"networking,omitempty"`
}

// VMNetworkStatus is the current state of networking on the VM
type VMNetworkStatus struct {
	// The assigned private IPv4 address of the VM
	PrivateIPv4Address string `json:"privateIPv4Address,omitempty"`

	// The assigned public IPv4 address of the VM
	PublicIPv4Address string `json:"publicIPv4Address,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VirtualMachine is the Schema for the virtualmachines API
type VirtualMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VirtualMachineSpec   `json:"spec,omitempty"`
	Status VirtualMachineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VirtualMachineList contains a list of VirtualMachine
type VirtualMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachine `json:"items"`
}

// DiskSpec defines the desired state of Disk
type DiskSpec struct {
	DiskSize         *DiskSize             `json:"diskSize,omitempty"`
	DiskImage        *DiskImageInfo        `json:"diskImage"`
	DiskStorageClass *DiskStorageClassInfo `json:"diskStorageClass"`
}

type DiskSize struct {
	Amount int    `json:"amount"`
	Unit   string `json:"unit"`
}

type DiskImageInfo struct {
	DiskImageRef DiskImageRef `json:"diskImageRef"`
}

type DiskImageRef struct {
	Name string `json:"name"`
}

type DiskStorageClassInfo struct {
	Name string `json:"name"`
}

// DiskStatus defines the observed state of Disk
type DiskStatus struct{}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Disk is the Schema for the disks API
type Disk struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DiskSpec   `json:"spec,omitempty"`
	Status DiskStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DiskList contains a list of Disk
type DiskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Disk `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachine{}, &VirtualMachineList{}, &Disk{}, &DiskList{})
}
