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

package evroc

import (
	"context"
	"encoding/base64"
	"fmt"

	computev1 "github.com/ravan/cluster-api-provider-evroc/api/v1alpha1/compute"
	networkingv1 "github.com/ravan/cluster-api-provider-evroc/api/v1alpha1/networking"
	infrav1 "github.com/ravan/cluster-api-provider-evroc/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileMachine ensures the virtual machine and its dependencies (disk, public IP) exist.
// It creates the public IP (if requested), boot disk, and virtual machine in that order.
// Once the VM is running, it updates the EvrocMachine status with addresses and provider ID.
// For control plane machines, it also updates the cluster's control plane endpoint.
func (s *Service) ReconcileMachine(ctx context.Context, mgmtClient client.Client, evrocCluster *infrav1.EvrocCluster, evrocMachine *infrav1.EvrocMachine, machine *clusterv1.Machine, bootstrapData []byte) error {
	log := s.log.WithValues("EvrocMachine", evrocMachine.Name)
	log.Info("Reconciling machine")

	var publicIPName string

	// Reconcile Public IP if requested
	if evrocMachine.Spec.PublicIP {
		// Check if this is a control plane machine - if so, reuse the pre-allocated PublicIP
		isControlPlane := metav1.HasLabel(machine.ObjectMeta, clusterv1.MachineControlPlaneLabel)

		if isControlPlane && evrocCluster.Status.ControlPlanePublicIPName != "" {
			// Reuse the pre-allocated control plane PublicIP
			publicIPName = evrocCluster.Status.ControlPlanePublicIPName
			log.Info("Using pre-allocated control plane PublicIP", "name", publicIPName)
		} else {
			// For worker nodes or if control plane IP not yet allocated, create a new PublicIP
			publicIP := &networkingv1.PublicIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-publicip", evrocMachine.Name),
					Namespace: evrocCluster.Spec.Project,
				},
			}
			err := s.Get(ctx, client.ObjectKeyFromObject(publicIP), publicIP)
			if err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("PublicIP not found, creating it")
					if err := s.Create(ctx, publicIP); err != nil {
						return fmt.Errorf("failed to create PublicIP %s: %w", publicIP.Name, err)
					}
					log.Info("PublicIP created successfully")
				} else {
					return fmt.Errorf("failed to get PublicIP %s: %w", publicIP.Name, err)
				}
			}
			publicIPName = publicIP.Name
		}
	}

	// Reconcile Boot Disk
	disk := &computev1.Disk{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-bootdisk", evrocMachine.Name),
			Namespace: evrocCluster.Spec.Project,
		},
		Spec: computev1.DiskSpec{
			DiskImage: &computev1.DiskImageInfo{
				DiskImageRef: computev1.DiskImageRef{
					Name: evrocMachine.Spec.BootDisk.ImageName,
				},
			},
			DiskSize: &computev1.DiskSize{
				Amount: evrocMachine.Spec.BootDisk.SizeGB,
				Unit:   "GB",
			},
			DiskStorageClass: &computev1.DiskStorageClassInfo{
				Name: evrocMachine.Spec.BootDisk.StorageClass,
			},
		},
	}
	err := s.Get(ctx, client.ObjectKeyFromObject(disk), disk)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Disk not found, creating it")
			if err := s.Create(ctx, disk); err != nil {
				return fmt.Errorf("failed to create Disk %s: %w", disk.Name, err)
			}
			log.Info("Disk created successfully")
		} else {
			return fmt.Errorf("failed to get Disk %s: %w", disk.Name, err)
		}
	}

	// Reconcile Virtual Machine
	encodedBootstrapData := base64.StdEncoding.EncodeToString(bootstrapData)

	// Prepare SSH settings if SSH key is provided
	var sshSettings *computev1.VMSSHSettings
	if evrocMachine.Spec.SSHKey != nil && *evrocMachine.Spec.SSHKey != "" {
		sshSettings = &computev1.VMSSHSettings{
			AuthorizedKeys: []computev1.VMAuthorizedKey{
				{
					Value: *evrocMachine.Spec.SSHKey,
				},
			},
		}
	}

	vm := &computev1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evrocMachine.Name,
			Namespace: evrocCluster.Spec.Project,
		},
		Spec: computev1.VirtualMachineSpec{
			Running: true,
			VMVirtualResourcesRef: computev1.VMVirtualResourcesRef{
				VMVirtualResourcesRefName: evrocMachine.Spec.VirtualResourcesRef,
			},
			DiskRefs: []computev1.DiskRef{
				{
					Name:     disk.Name,
					BootFrom: true,
				},
			},
			OSSettings: &computev1.VMOSSettings{
				CloudInitUserData: encodedBootstrapData,
				SSH:               sshSettings,
			},
			Networking: &computev1.VMNetworkingSettings{
				PublicIPv4Address: &computev1.VMPublicIPv4AddressSettings{
					Static: &computev1.VMStaticPublicIPv4AddressSettings{
						PublicIPRef: publicIPName,
					},
				},
			},
		},
	}

	// Add security groups to the Networking settings if specified
	if len(evrocMachine.Spec.SecurityGroups) > 0 {
		securityGroupMemberships := make([]computev1.SecurityGroupMembershipRef, len(evrocMachine.Spec.SecurityGroups))
		for i, sg := range evrocMachine.Spec.SecurityGroups {
			securityGroupMemberships[i] = computev1.SecurityGroupMembershipRef{Name: sg}
		}
		vm.Spec.Networking.SecurityGroups = &computev1.SecurityGroupSettings{
			SecurityGroupMemberships: securityGroupMemberships,
		}
	}

	err = s.Get(ctx, client.ObjectKeyFromObject(vm), vm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("VirtualMachine not found, creating it")
			if err := s.Create(ctx, vm); err != nil {
				return fmt.Errorf("failed to create VirtualMachine %s: %w", vm.Name, err)
			}
			log.Info("VirtualMachine created successfully")
		} else {
			return fmt.Errorf("failed to get VirtualMachine %s: %w", vm.Name, err)
		}
	}

	// Check if the VM is running
	if vm.Status.VirtualMachineStatus != "Running" {
		log.Info("VM is not yet in Running state", "status", vm.Status.VirtualMachineStatus)
		return nil // Requeue and check again later
	}

	// Update EvrocMachine Status
	machinePatchHelper, err := patch.NewHelper(evrocMachine, mgmtClient)
	if err != nil {
		return err
	}
	providerID := fmt.Sprintf("evroc://%s/%s", evrocCluster.Spec.Project, vm.Name)
	evrocMachine.Spec.ProviderID = &providerID
	evrocMachine.Status.Ready = true
	evrocMachine.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: vm.Status.Networking.PrivateIPv4Address},
		{Type: corev1.NodeExternalIP, Address: vm.Status.Networking.PublicIPv4Address},
	}
	if err := machinePatchHelper.Patch(ctx, evrocMachine); err != nil {
		return err
	}

	// Note: Control plane endpoint is now managed by the EvrocCluster controller
	// using a pre-allocated PublicIP, so we don't need to update it here

	return nil
}

// DeleteMachine removes the virtual machine and its associated resources (disk, public IP).
// Resources are deleted in reverse order: VM, then disk, then public IP.
// NotFound errors are ignored as resources may have already been deleted.
func (s *Service) DeleteMachine(ctx context.Context, evrocCluster *infrav1.EvrocCluster, evrocMachine *infrav1.EvrocMachine) error {
	log := s.log.WithValues("EvrocMachine", evrocMachine.Name)
	log.Info("Deleting machine")

	// Delete Virtual Machine
	vm := &computev1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evrocMachine.Name,
			Namespace: evrocCluster.Spec.Project,
		},
	}
	if err := s.Delete(ctx, vm); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete VirtualMachine %s: %w", vm.Name, err)
	}

	// Delete Boot Disk
	disk := &computev1.Disk{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-bootdisk", evrocMachine.Name),
			Namespace: evrocCluster.Spec.Project,
		},
	}
	if err := s.Delete(ctx, disk); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete Disk %s: %w", disk.Name, err)
	}

	// Delete Public IP if it was requested
	if evrocMachine.Spec.PublicIP {
		publicIP := &networkingv1.PublicIP{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-publicip", evrocMachine.Name),
				Namespace: evrocCluster.Spec.Project,
			},
		}
		if err := s.Delete(ctx, publicIP); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete PublicIP %s: %w", publicIP.Name, err)
		}
	}

	return nil
}
