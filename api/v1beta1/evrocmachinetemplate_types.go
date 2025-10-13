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
)

// EvrocMachineTemplateSpec defines the desired state of EvrocMachineTemplate
type EvrocMachineTemplateSpec struct {
	// Template is the template for creating EvrocMachine resources.
	Template EvrocMachineTemplateResource `json:"template"`
}

// EvrocMachineTemplateResource defines the template for creating EvrocMachine resources.
type EvrocMachineTemplateResource struct {
	// Spec is the specification for the EvrocMachines to be created from this template.
	Spec EvrocMachineSpec `json:"spec"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:path=evrocmachinetemplates,scope=Namespaced,categories=cluster-api
//+kubebuilder:storageversion

// EvrocMachineTemplate is the Schema for the evrocmachinetemplates API
type EvrocMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec EvrocMachineTemplateSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// EvrocMachineTemplateList contains a list of EvrocMachineTemplate
type EvrocMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EvrocMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EvrocMachineTemplate{}, &EvrocMachineTemplateList{})
}
