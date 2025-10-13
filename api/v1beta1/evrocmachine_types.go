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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// Machine condition types
const (
	// VMReadyCondition indicates the virtual machine has been provisioned and is running
	VMReadyCondition clusterv1.ConditionType = "VMReady"

	// BootstrapDataReadyCondition indicates the bootstrap data secret is available
	BootstrapDataReadyCondition clusterv1.ConditionType = "BootstrapDataReady"

	// DiskReadyCondition indicates the boot disk has been provisioned
	DiskReadyCondition clusterv1.ConditionType = "DiskReady"

	// PublicIPReadyCondition indicates the public IP has been allocated (if requested)
	PublicIPReadyCondition clusterv1.ConditionType = "PublicIPReady"
)

// EvrocMachineSpec defines the desired state of EvrocMachine
type EvrocMachineSpec struct {
	// ProviderID is the unique identifier for the instance in the evroc cloud.
	// This is typically set by the controller.
	// +optional
	ProviderID *string `json:"providerID,omitempty"`

	// The machine type and size (e.g., `c1a.s`, `m1a.l`).
	// This maps to a VMVirtualResources resource in the evroc API.
	// +kubebuilder:validation:Required
	VirtualResourcesRef string `json:"virtualResourcesRef"`

	// Defines the properties of the boot disk for the virtual machine.
	// +kubebuilder:validation:Required
	BootDisk EvrocDiskSpec `json:"bootDisk"`

	// The SSH public key that will be added to the `evroc-user` for remote access.
	// +optional
	SSHKey *string `json:"sshKey,omitempty"`

	// The name of the subnet to which this machine's primary network interface will be attached.
	// +kubebuilder:validation:Required
	SubnetName string `json:"subnetName"`

	// Security groups to attach to this machine for firewall rules.
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// If true, a static public IP will be allocated and associated with this machine. Defaults to false.
	// +optional
	PublicIP bool `json:"publicIP,omitempty"`
}

// EvrocDiskSpec defines the properties of a boot disk for a virtual machine.
type EvrocDiskSpec struct {
	// The name of the OS disk image to use (e.g., `ubuntu-minimal.24-04.1`).
	// This maps to a DiskImage resource in evroc.
	// +kubebuilder:validation:Required
	ImageName string `json:"imageName"`

	// The storage class for the disk. Must be `persistent`.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=persistent
	StorageClass string `json:"storageClass"`

	// The size of the disk in Gigabytes.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	SizeGB int `json:"sizeGB"`
}

// EvrocMachineStatus defines the observed state of EvrocMachine
type EvrocMachineStatus struct {
	// Ready indicates whether the machine is ready and has joined the cluster.
	// +optional
	Ready bool `json:"ready"`

	// Addresses is a list of addresses assigned to the machine.
	// +optional
	Addresses []corev1.NodeAddress `json:"addresses,omitempty"`

	// InstanceState is the current state of the evroc virtual machine.
	// (e.g., `Running`, `Stopped`, `Creating`).
	// +optional
	InstanceState *string `json:"instanceState,omitempty"`

	// FailureReason will be set in case of a terminal problem
	// and will contain a short value suitable for machine interpretation.
	// +optional
	FailureReason *string `json:"failureReason,omitempty"`

	// FailureMessage will be set in case of a terminal problem
	// and will contain a long user-readable message.
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`

	// Conditions defines current service state of the EvrocMachine.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=evrocmachines,scope=Namespaced,categories=cluster-api
//+kubebuilder:storageversion
//+kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this EvrocMachine belongs"
//+kubebuilder:printcolumn:name="Machine",type="string",JSONPath=".metadata.ownerReferences[?(@.kind==\"Machine\")].name",description="Machine object which owns this EvrocMachine"
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Machine is ready"
//+kubebuilder:printcolumn:name="InstanceState",type="string",JSONPath=".status.instanceState",description="VM instance state"
//+kubebuilder:printcolumn:name="ProviderID",type="string",JSONPath=".spec.providerID",description="Provider ID"

// EvrocMachine is the Schema for the evrocmachines API
type EvrocMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EvrocMachineSpec   `json:"spec,omitempty"`
	Status EvrocMachineStatus `json:"status,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (m *EvrocMachine) GetConditions() clusterv1.Conditions {
	return m.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (m *EvrocMachine) SetConditions(conditions clusterv1.Conditions) {
	m.Status.Conditions = conditions
}

//+kubebuilder:object:root=true

// EvrocMachineList contains a list of EvrocMachine
type EvrocMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EvrocMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EvrocMachine{}, &EvrocMachineList{})
}
