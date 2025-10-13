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
	"context"
	"fmt"
	"strings"

	"github.com/ravan/cluster-api-provider-evroc/internal/cloud/evroc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/ravan/cluster-api-provider-evroc/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
)

const (
	evrocClusterFinalizer = "evroccluster.infrastructure.evroc.com"
)

// EvrocClusterReconciler reconciles a EvrocCluster object
type EvrocClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infrastructure.evroc.com,resources=evrocclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.evroc.com,resources=evrocclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.evroc.com,resources=evrocclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=infrastructure.evroc.com,resources=evrocmachines,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;patch;update

func (r *EvrocClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	logger := log.FromContext(ctx)

	// Fetch the EvrocCluster instance.
	evrocCluster := &infrav1.EvrocCluster{}
	if err := r.Get(ctx, req.NamespacedName, evrocCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster (optional - may not be set yet).
	// We proceed even if the OwnerRef is not set, as the infrastructure
	// can be reconciled independently. The Cluster controller will set
	// the OwnerRef eventually.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, evrocCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Return early if the object or Cluster is paused.
	// Only check if cluster is available
	if cluster != nil && annotations.IsPaused(cluster, evrocCluster) {
		logger.Info("EvrocCluster or linked Cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	// Initialize patch helper before any updates to the resource
	patchHelper, err := patch.NewHelper(evrocCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always patch the object when exiting this function
	defer func() {
		if err := patchHelper.Patch(
			ctx,
			evrocCluster,
			patch.WithOwnedConditions{Conditions: []clusterv1.ConditionType{
				clusterv1.ReadyCondition,
				infrav1.NetworkReadyCondition,
				infrav1.VPCReadyCondition,
				infrav1.SubnetsReadyCondition,
			}},
		); err != nil {
			logger.Error(err, "Failed to patch EvrocCluster")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Create the evroc client
	evrocClient, err := evroc.New(ctx, r.Client, evrocCluster, logger)
	if err != nil {
		// Client creation failure could be due to missing secrets or invalid config
		if evroc.IsNotFoundError(err) {
			// Secret not found - requeue and wait
			logger.Info("Identity secret not found, waiting", "secret", evrocCluster.Spec.IdentitySecretName)
			return ctrl.Result{RequeueAfter: evroc.BootstrapDataRetryDelay}, nil
		}
		// Other errors are likely terminal (invalid config, etc.)
		return ctrl.Result{}, fmt.Errorf("failed to create evroc client: %w", err)
	}

	// Handle deletion
	if !evrocCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, evrocClient, evrocCluster)
	}

	// Handle reconciliation
	return r.reconcileNormal(ctx, evrocClient, evrocCluster)
}

func (r *EvrocClusterReconciler) reconcileNormal(ctx context.Context, evrocClient *evroc.Service, evrocCluster *infrav1.EvrocCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling EvrocCluster")

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(evrocCluster, evrocClusterFinalizer) {
		controllerutil.AddFinalizer(evrocCluster, evrocClusterFinalizer)
		return ctrl.Result{}, nil
	}

	// Reconcile network
	if err := evrocClient.ReconcileNetwork(ctx, evrocCluster); err != nil {
		conditions.MarkFalse(
			evrocCluster,
			infrav1.NetworkReadyCondition,
			"NetworkReconciliationFailed",
			clusterv1.ConditionSeverityError,
			"Failed to reconcile network: %v", err,
		)
		conditions.MarkFalse(
			evrocCluster,
			clusterv1.ReadyCondition,
			"NetworkNotReady",
			clusterv1.ConditionSeverityError,
			"Network reconciliation failed",
		)
		return ctrl.Result{}, fmt.Errorf("failed to reconcile network: %w", err)
	}

	// Mark network as ready
	conditions.MarkTrue(evrocCluster, infrav1.NetworkReadyCondition)

	// Reconcile control plane PublicIP - this must happen before endpoint reconciliation
	publicIPName, ipAddress, err := evrocClient.ReconcileControlPlanePublicIP(ctx, evrocCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile control plane PublicIP: %w", err)
	}

	// Update the status with the PublicIP name
	evrocCluster.Status.ControlPlanePublicIPName = publicIPName

	// If IP address is not yet allocated, requeue and wait
	if ipAddress == "" {
		logger.Info("Control plane PublicIP not yet allocated, waiting")
		return ctrl.Result{RequeueAfter: evroc.BootstrapDataRetryDelay}, nil
	}

	// Reconcile control plane endpoint (only if Cluster is available)
	// Fetch the Cluster to update ControlPlaneEndpoint
	cluster, err := util.GetOwnerCluster(ctx, r.Client, evrocCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster != nil {
		// OwnerRef is set, we can update the control plane endpoint with the pre-allocated IP
		if err := r.reconcileControlPlaneEndpoint(ctx, evrocClient, evrocCluster, cluster, ipAddress); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile control plane endpoint: %w", err)
		}
	} else {
		// OwnerRef not set yet, skip control plane endpoint for now
		// It will be reconciled in the next iteration once the OwnerRef is set
		logger.Info("Cluster OwnerRef not set yet, skipping control plane endpoint reconciliation")
	}

	// Mark cluster as ready
	conditions.MarkTrue(evrocCluster, clusterv1.ReadyCondition)
	evrocCluster.Status.Ready = true

	logger.Info("Successfully reconciled EvrocCluster")
	return ctrl.Result{}, nil
}

func (r *EvrocClusterReconciler) reconcileControlPlaneEndpoint(ctx context.Context, evrocClient *evroc.Service, evrocCluster *infrav1.EvrocCluster, cluster *clusterv1.Cluster, publicIPAddress string) error {
	logger := log.FromContext(ctx)

	// Skip if ControlPlaneEndpoint is already set to the correct IP
	if cluster.Spec.ControlPlaneEndpoint.Host == publicIPAddress && cluster.Spec.ControlPlaneEndpoint.Port == 6443 {
		logger.Info("ControlPlaneEndpoint already set correctly", "host", publicIPAddress, "port", 6443)
		return nil
	}

	port := int32(6443)
	logger.Info("Setting ControlPlaneEndpoint to pre-allocated PublicIP", "host", publicIPAddress, "port", port)

	// Create a patch helper for the cluster
	patchHelper, err := patch.NewHelper(cluster, r.Client)
	if err != nil {
		return fmt.Errorf("failed to create patch helper for cluster: %w", err)
	}

	// Set the ControlPlaneEndpoint to the pre-allocated public IP
	cluster.Spec.ControlPlaneEndpoint.Host = publicIPAddress
	cluster.Spec.ControlPlaneEndpoint.Port = port

	// Patch the cluster
	if err := patchHelper.Patch(ctx, cluster); err != nil {
		return fmt.Errorf("failed to patch cluster with control plane endpoint: %w", err)
	}

	logger.Info("Successfully set ControlPlaneEndpoint to pre-allocated IP")
	return nil
}

func (r *EvrocClusterReconciler) reconcileDelete(ctx context.Context, evrocClient *evroc.Service, evrocCluster *infrav1.EvrocCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Deleting EvrocCluster")

	// Delete network
	if err := evrocClient.DeleteNetwork(ctx, evrocCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete network: %w", err)
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(evrocCluster, evrocClusterFinalizer)

	logger.Info("Successfully deleted EvrocCluster")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EvrocClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.EvrocCluster{}).
		Complete(r)
}

// containsString checks if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
