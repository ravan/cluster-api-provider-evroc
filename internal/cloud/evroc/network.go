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
	"fmt"

	networkingv1 "github.com/ravan/cluster-api-provider-evroc/api/v1alpha1/networking"
	infrav1 "github.com/ravan/cluster-api-provider-evroc/api/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileNetwork ensures the VPC and subnets defined in the EvrocCluster spec exist.
// It creates the VPC if it doesn't exist, then creates all specified subnets.
// The cluster status is updated with the current state of the network resources.
func (s *Service) ReconcileNetwork(ctx context.Context, evrocCluster *infrav1.EvrocCluster) error {
	log := s.log.WithValues("EvrocCluster", evrocCluster.Name)
	log.Info("Reconciling network")

	// Reconcile VPC
	vpcName := evrocCluster.Spec.Network.VPC.Name
	if vpcName == "" {
		vpcName = evrocCluster.Name
	}

	vpc := &networkingv1.VirtualPrivateCloud{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vpcName,
			Namespace: evrocCluster.Spec.Project,
		},
	}

	err := s.Get(ctx, client.ObjectKeyFromObject(vpc), vpc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("VPC not found, creating it")
			if err := s.Create(ctx, vpc); err != nil {
				return fmt.Errorf("failed to create VPC %s: %w", vpc.Name, err)
			}
			log.Info("VPC created successfully")
		} else {
			return fmt.Errorf("failed to get VPC %s: %w", vpc.Name, err)
		}
	}

	// Update VPC status
	evrocCluster.Status.Network.VPC.Name = vpc.Name
	evrocCluster.Status.Network.VPC.Ready = true

	// Reconcile all subnets from spec
	var subnetStatuses []infrav1.EvrocSubnetStatus

	for _, subnetSpec := range evrocCluster.Spec.Network.Subnets {
		subnet := &networkingv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subnetSpec.Name,
				Namespace: evrocCluster.Spec.Project,
			},
			Spec: networkingv1.SubnetSpec{
				VpcRef: networkingv1.VpcRef{
					Name: vpc.Name,
				},
				Ipv4CidrBlock: networkingv1.Ipv4CidrBlock{
					Block: subnetSpec.CIDRBlock,
				},
			},
		}

		err = s.Get(ctx, client.ObjectKeyFromObject(subnet), subnet)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Subnet not found, creating it", "subnet", subnetSpec.Name)
				if err := s.Create(ctx, subnet); err != nil {
					return fmt.Errorf("failed to create Subnet %s: %w", subnet.Name, err)
				}
				log.Info("Subnet created successfully", "subnet", subnetSpec.Name)
			} else {
				return fmt.Errorf("failed to get Subnet %s: %w", subnet.Name, err)
			}
		}

		// Add to status
		subnetStatuses = append(subnetStatuses, infrav1.EvrocSubnetStatus{
			Name:      subnet.Name,
			ID:        subnet.Name,
			CIDRBlock: subnetSpec.CIDRBlock,
			Ready:     true,
		})
	}

	evrocCluster.Status.Network.Subnets = subnetStatuses

	return nil
}

// ReconcileControlPlanePublicIP ensures a PublicIP resource exists for the control plane.
// This PublicIP is pre-allocated before any machines are created, providing a stable
// endpoint that can be used in the bootstrap data. Returns the PublicIP name and address.
func (s *Service) ReconcileControlPlanePublicIP(ctx context.Context, evrocCluster *infrav1.EvrocCluster) (string, string, error) {
	log := s.log.WithValues("EvrocCluster", evrocCluster.Name)
	log.Info("Reconciling control plane PublicIP")

	// Use a deterministic name for the control plane PublicIP
	publicIPName := fmt.Sprintf("%s-cp-publicip", evrocCluster.Name)

	publicIP := &networkingv1.PublicIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      publicIPName,
			Namespace: evrocCluster.Spec.Project,
		},
	}

	err := s.Get(ctx, client.ObjectKeyFromObject(publicIP), publicIP)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Control plane PublicIP not found, creating it")
			if err := s.Create(ctx, publicIP); err != nil {
				return "", "", fmt.Errorf("failed to create PublicIP %s: %w", publicIP.Name, err)
			}
			log.Info("Control plane PublicIP created successfully", "name", publicIPName)

			// After creation, fetch again to get the assigned IP address
			if err := s.Get(ctx, client.ObjectKeyFromObject(publicIP), publicIP); err != nil {
				return "", "", fmt.Errorf("failed to get PublicIP after creation %s: %w", publicIP.Name, err)
			}
		} else {
			return "", "", fmt.Errorf("failed to get PublicIP %s: %w", publicIP.Name, err)
		}
	}

	// Extract the IP address from the PublicIP status
	ipAddress := publicIP.Status.PublicIPv4Address
	if ipAddress == "" {
		log.Info("PublicIP not yet allocated, waiting", "name", publicIPName)
		return publicIPName, "", nil
	}

	log.Info("Control plane PublicIP ready", "name", publicIPName, "address", ipAddress)
	return publicIPName, ipAddress, nil
}

// DeleteNetwork removes all network resources (subnets and VPC) associated with the cluster.
// Subnets are deleted first, followed by the VPC.
// NotFound and Forbidden errors are ignored - NotFound means already deleted, Forbidden means
// it's a shared/pre-existing resource that we shouldn't (and can't) delete.
func (s *Service) DeleteNetwork(ctx context.Context, evrocCluster *infrav1.EvrocCluster) error {
	log := s.log.WithValues("EvrocCluster", evrocCluster.Name)
	log.Info("Deleting network")

	// Delete all subnets
	for _, subnetSpec := range evrocCluster.Spec.Network.Subnets {
		subnet := &networkingv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subnetSpec.Name,
				Namespace: evrocCluster.Spec.Project,
			},
		}
		if err := s.Delete(ctx, subnet); err != nil {
			if apierrors.IsNotFound(err) {
				// Subnet already deleted, that's fine
				log.Info("Subnet already deleted or not found", "subnet", subnetSpec.Name)
			} else if apierrors.IsForbidden(err) {
				// Forbidden means it's a shared/pre-existing resource we can't delete
				log.Info("Skipping deletion of shared/pre-existing subnet (read-only)", "subnet", subnetSpec.Name)
			} else {
				return fmt.Errorf("failed to delete Subnet %s: %w", subnet.Name, err)
			}
		} else {
			log.Info("Deleted subnet", "subnet", subnetSpec.Name)
		}
	}

	// Delete control plane PublicIP using deterministic name
	// This ensures cleanup works even if the status field wasn't populated
	publicIPName := fmt.Sprintf("%s-cp-publicip", evrocCluster.Name)
	publicIP := &networkingv1.PublicIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      publicIPName,
			Namespace: evrocCluster.Spec.Project,
		},
	}
	if err := s.Delete(ctx, publicIP); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete control plane PublicIP %s: %w", publicIP.Name, err)
	}
	log.Info("Deleted control plane PublicIP", "name", publicIPName)

	// Delete VPC
	vpcName := evrocCluster.Spec.Network.VPC.Name
	if vpcName == "" {
		vpcName = evrocCluster.Name
	}

	vpc := &networkingv1.VirtualPrivateCloud{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vpcName,
			Namespace: evrocCluster.Spec.Project,
		},
	}
	if err := s.Delete(ctx, vpc); err != nil {
		if apierrors.IsNotFound(err) {
			// VPC already deleted, that's fine
			log.Info("VPC already deleted or not found", "vpc", vpcName)
		} else if apierrors.IsForbidden(err) {
			// Forbidden means it's a shared/pre-existing VPC we can't delete
			log.Info("Skipping deletion of shared/pre-existing VPC (read-only)", "vpc", vpcName)
		} else {
			return fmt.Errorf("failed to delete VPC %s: %w", vpc.Name, err)
		}
	} else {
		log.Info("Deleted VPC", "vpc", vpcName)
	}

	return nil
}
