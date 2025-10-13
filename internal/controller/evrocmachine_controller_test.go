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

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1beta1 "github.com/ravan/cluster-api-provider-evroc/api/v1beta1"
)

var _ = Describe("EvrocMachine Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling without owner machine", func() {
		var (
			evrocMachine     *infrastructurev1beta1.EvrocMachine
			evrocMachineName types.NamespacedName
		)

		BeforeEach(func() {
			evrocMachineName = types.NamespacedName{
				Name:      "test-machine-no-owner",
				Namespace: "default",
			}

			evrocMachine = &infrastructurev1beta1.EvrocMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      evrocMachineName.Name,
					Namespace: evrocMachineName.Namespace,
				},
				Spec: infrastructurev1beta1.EvrocMachineSpec{
					VirtualResourcesRef: "c1a.s",
					BootDisk: infrastructurev1beta1.EvrocDiskSpec{
						ImageName:    "ubuntu-minimal.24-04.1",
						StorageClass: "persistent",
						SizeGB:       20,
					},
					SubnetName: "test-subnet",
				},
			}
		})

		AfterEach(func() {
			resource := &infrastructurev1beta1.EvrocMachine{}
			err := k8sClient.Get(ctx, evrocMachineName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should return early when no owner machine is set", func() {
			Expect(k8sClient.Create(ctx, evrocMachine)).To(Succeed())

			reconciler := &EvrocMachineReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: evrocMachineName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})
	})

	Context("When reconciling with complete owner hierarchy", func() {
		var (
			evrocMachine     *infrastructurev1beta1.EvrocMachine
			machine          *clusterv1.Machine
			evrocCluster     *infrastructurev1beta1.EvrocCluster
			cluster          *clusterv1.Cluster
			evrocMachineName types.NamespacedName
			machineName      types.NamespacedName
			evrocClusterName types.NamespacedName
			clusterName      types.NamespacedName
		)

		BeforeEach(func() {
			evrocMachineName = types.NamespacedName{
				Name:      "test-machine-with-owner",
				Namespace: "default",
			}
			machineName = types.NamespacedName{
				Name:      "test-capi-machine",
				Namespace: "default",
			}
			evrocClusterName = types.NamespacedName{
				Name:      "test-evroc-cluster",
				Namespace: "default",
			}
			clusterName = types.NamespacedName{
				Name:      "test-cluster",
				Namespace: "default",
			}

			// Create CAPI Cluster
			cluster = &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName.Name,
					Namespace: clusterName.Namespace,
				},
				Spec: clusterv1.ClusterSpec{
					InfrastructureRef: &corev1.ObjectReference{
						APIVersion: infrastructurev1beta1.GroupVersion.String(),
						Kind:       "EvrocCluster",
						Name:       evrocClusterName.Name,
						Namespace:  evrocClusterName.Namespace,
					},
				},
				Status: clusterv1.ClusterStatus{
					InfrastructureReady: true,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create EvrocCluster
			evrocCluster = &infrastructurev1beta1.EvrocCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      evrocClusterName.Name,
					Namespace: evrocClusterName.Namespace,
				},
				Spec: infrastructurev1beta1.EvrocClusterSpec{
					Region:             "region-1",
					Project:            "test-project",
					IdentitySecretName: "test-secret",
					Network: infrastructurev1beta1.EvrocNetworkSpec{
						VPC: infrastructurev1beta1.EvrocVPCSpec{
							Name: "test-vpc",
						},
						Subnets: []infrastructurev1beta1.EvrocSubnetSpec{
							{
								Name:      "test-subnet",
								CIDRBlock: "10.0.0.0/24",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, evrocCluster)).To(Succeed())

			// Create CAPI Machine with cluster label
			machine = &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      machineName.Name,
					Namespace: machineName.Namespace,
					Labels: map[string]string{
						clusterv1.ClusterNameLabel: cluster.Name,
					},
				},
				Spec: clusterv1.MachineSpec{
					ClusterName: cluster.Name,
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: infrastructurev1beta1.GroupVersion.String(),
						Kind:       "EvrocMachine",
						Name:       evrocMachineName.Name,
						Namespace:  evrocMachineName.Namespace,
					},
					Bootstrap: clusterv1.Bootstrap{
						DataSecretName: nil, // No bootstrap data yet
					},
				},
			}
			Expect(k8sClient.Create(ctx, machine)).To(Succeed())

			// Create EvrocMachine with owner reference
			evrocMachine = &infrastructurev1beta1.EvrocMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      evrocMachineName.Name,
					Namespace: evrocMachineName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: clusterv1.GroupVersion.String(),
							Kind:       "Machine",
							Name:       machine.Name,
							UID:        machine.UID,
						},
					},
				},
				Spec: infrastructurev1beta1.EvrocMachineSpec{
					VirtualResourcesRef: "c1a.s",
					BootDisk: infrastructurev1beta1.EvrocDiskSpec{
						ImageName:    "ubuntu-minimal.24-04.1",
						StorageClass: "persistent",
						SizeGB:       20,
					},
					SubnetName: "test-subnet",
				},
			}
		})

		AfterEach(func() {
			// Cleanup in reverse order
			resource := &infrastructurev1beta1.EvrocMachine{}
			err := k8sClient.Get(ctx, evrocMachineName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			capiMachine := &clusterv1.Machine{}
			err = k8sClient.Get(ctx, machineName, capiMachine)
			if err == nil {
				Expect(k8sClient.Delete(ctx, capiMachine)).To(Succeed())
			}

			evrocClusterResource := &infrastructurev1beta1.EvrocCluster{}
			err = k8sClient.Get(ctx, evrocClusterName, evrocClusterResource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, evrocClusterResource)).To(Succeed())
			}

			capiCluster := &clusterv1.Cluster{}
			err = k8sClient.Get(ctx, clusterName, capiCluster)
			if err == nil {
				Expect(k8sClient.Delete(ctx, capiCluster)).To(Succeed())
			}
		})

		It("should add finalizer on first reconciliation", func() {
			Expect(k8sClient.Create(ctx, evrocMachine)).To(Succeed())

			reconciler := &EvrocMachineReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile should add finalizer
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: evrocMachineName,
			})
			// We expect requeue after 5 seconds because no bootstrap data
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			// Verify finalizer was added
			Eventually(func() bool {
				updated := &infrastructurev1beta1.EvrocMachine{}
				if err := k8sClient.Get(ctx, evrocMachineName, updated); err != nil {
					return false
				}
				return len(updated.Finalizers) > 0
			}, timeout, interval).Should(BeTrue())
		})

		It("should wait for bootstrap data when not available", func() {
			Expect(k8sClient.Create(ctx, evrocMachine)).To(Succeed())

			reconciler := &EvrocMachineReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Reconcile without bootstrap data
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: evrocMachineName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Should requeue after 5 seconds waiting for bootstrap data
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))
		})

		It("should handle paused cluster", func() {
			// Mark cluster as paused
			cluster.Spec.Paused = true
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			Expect(k8sClient.Create(ctx, evrocMachine)).To(Succeed())

			reconciler := &EvrocMachineReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: evrocMachineName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})
	})
})
