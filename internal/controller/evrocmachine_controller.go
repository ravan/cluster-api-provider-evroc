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

	"github.com/ravan/cluster-api-provider-evroc/internal/cloud/evroc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/ravan/cluster-api-provider-evroc/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

const (
	evrocMachineFinalizer = "evrocmachine.infrastructure.evroc.com"
)

// EvrocMachineReconciler reconciles a EvrocMachine object
type EvrocMachineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infrastructure.evroc.com,resources=evrocmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.evroc.com,resources=evrocmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.evroc.com,resources=evrocmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *EvrocMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	logger := log.FromContext(ctx)

	// Fetch the EvrocMachine instance.
	evrocMachine := &infrav1.EvrocMachine{}
	if err := r.Get(ctx, req.NamespacedName, evrocMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Machine and Cluster.
	machine, err := util.GetOwnerMachine(ctx, r.Client, evrocMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		logger.Info("Machine Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		logger.Info("Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, nil
	}

	// Fetch the EvrocCluster.
	evrocCluster := &infrav1.EvrocCluster{}
	evrocClusterName := client.ObjectKey{
		Namespace: evrocMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, evrocClusterName, evrocCluster); err != nil {
		logger.Info("EvrocCluster is not available yet")
		return ctrl.Result{}, nil
	}

	// Return early if the object or Cluster is paused.
	if annotations.IsPaused(cluster, evrocMachine) {
		logger.Info("EvrocMachine or linked Cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	// Initialize patch helper before any updates to the resource
	patchHelper, err := patch.NewHelper(evrocMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always patch the object when exiting this function
	defer func() {
		if err := patchHelper.Patch(
			ctx,
			evrocMachine,
			patch.WithOwnedConditions{Conditions: []clusterv1.ConditionType{
				clusterv1.ReadyCondition,
				infrav1.VMReadyCondition,
				infrav1.BootstrapDataReadyCondition,
				infrav1.DiskReadyCondition,
				infrav1.PublicIPReadyCondition,
			}},
		); err != nil {
			logger.Error(err, "Failed to patch EvrocMachine")
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
	if !evrocMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, evrocClient, evrocCluster, evrocMachine)
	}

	// Handle reconciliation
	return r.reconcileNormal(ctx, evrocClient, cluster, machine, evrocCluster, evrocMachine)
}

func (r *EvrocMachineReconciler) reconcileNormal(ctx context.Context, evrocClient *evroc.Service, cluster *clusterv1.Cluster, machine *clusterv1.Machine, evrocCluster *infrav1.EvrocCluster, evrocMachine *infrav1.EvrocMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling EvrocMachine")

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(evrocMachine, evrocMachineFinalizer) {
		controllerutil.AddFinalizer(evrocMachine, evrocMachineFinalizer)
		return ctrl.Result{}, nil
	}

	// Check if cluster infrastructure is ready
	if !cluster.Status.InfrastructureReady {
		logger.Info("Waiting for cluster infrastructure to be ready")
		conditions.MarkFalse(
			evrocMachine,
			clusterv1.ReadyCondition,
			"WaitingForClusterInfrastructure",
			clusterv1.ConditionSeverityInfo,
			"Waiting for cluster infrastructure to be ready",
		)
		return ctrl.Result{RequeueAfter: evroc.BootstrapDataRetryDelay}, nil
	}

	// Check if bootstrap data secret is set
	if machine.Spec.Bootstrap.DataSecretName == nil {
		// For worker nodes, wait for control plane to be initialized
		if !util.IsControlPlaneMachine(machine) && !conditions.IsTrue(cluster, clusterv1.ControlPlaneInitializedCondition) {
			logger.Info("Waiting for the control plane to be initialized")
			conditions.MarkFalse(
				evrocMachine,
				clusterv1.ReadyCondition,
				"WaitingForControlPlane",
				clusterv1.ConditionSeverityInfo,
				"Waiting for control plane to be initialized",
			)
			return ctrl.Result{RequeueAfter: evroc.BootstrapDataRetryDelay}, nil
		}

		logger.Info("Waiting for the Bootstrap provider controller to set bootstrap data")
		conditions.MarkFalse(
			evrocMachine,
			infrav1.BootstrapDataReadyCondition,
			"WaitingForBootstrapData",
			clusterv1.ConditionSeverityInfo,
			"Waiting for bootstrap data secret to be set",
		)
		return ctrl.Result{RequeueAfter: evroc.BootstrapDataRetryDelay}, nil
	}

	// Get bootstrap data
	bootstrapData, err := r.getBootstrapData(ctx, machine)
	if err != nil {
		// If bootstrap data secret is not found, wait for it
		if evroc.IsNotFoundError(err) {
			logger.Info("Bootstrap data secret not found yet, waiting")
			conditions.MarkFalse(
				evrocMachine,
				infrav1.BootstrapDataReadyCondition,
				"BootstrapDataSecretNotFound",
				clusterv1.ConditionSeverityInfo,
				"Bootstrap data secret not found yet",
			)
			return ctrl.Result{RequeueAfter: evroc.BootstrapDataRetryDelay}, nil
		}

		// Other errors are more serious
		conditions.MarkFalse(
			evrocMachine,
			infrav1.BootstrapDataReadyCondition,
			"BootstrapDataUnavailable",
			clusterv1.ConditionSeverityError,
			"Failed to get bootstrap data: %v", err,
		)
		conditions.MarkFalse(
			evrocMachine,
			clusterv1.ReadyCondition,
			"BootstrapDataNotReady",
			clusterv1.ConditionSeverityError,
			"Bootstrap data is not available",
		)
		return ctrl.Result{}, err
	}

	// Mark bootstrap data as ready
	conditions.MarkTrue(evrocMachine, infrav1.BootstrapDataReadyCondition)

	// Reconcile machine
	if err := evrocClient.ReconcileMachine(ctx, r.Client, evrocCluster, evrocMachine, machine, bootstrapData); err != nil {
		conditions.MarkFalse(
			evrocMachine,
			infrav1.VMReadyCondition,
			"VMReconciliationFailed",
			clusterv1.ConditionSeverityError,
			"Failed to reconcile machine: %v", err,
		)
		conditions.MarkFalse(
			evrocMachine,
			clusterv1.ReadyCondition,
			"VMNotReady",
			clusterv1.ConditionSeverityError,
			"Machine reconciliation failed",
		)
		return ctrl.Result{}, fmt.Errorf("failed to reconcile machine: %w", err)
	}

	// Mark VM as ready
	conditions.MarkTrue(evrocMachine, infrav1.VMReadyCondition)

	// Mark machine as ready
	conditions.MarkTrue(evrocMachine, clusterv1.ReadyCondition)
	evrocMachine.Status.Ready = true

	logger.Info("Successfully reconciled EvrocMachine")
	return ctrl.Result{}, nil
}

func (r *EvrocMachineReconciler) reconcileDelete(ctx context.Context, evrocClient *evroc.Service, evrocCluster *infrav1.EvrocCluster, evrocMachine *infrav1.EvrocMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Deleting EvrocMachine")

	// Delete machine
	if err := evrocClient.DeleteMachine(ctx, evrocCluster, evrocMachine); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete machine: %w", err)
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(evrocMachine, evrocMachineFinalizer)

	logger.Info("Successfully deleted EvrocMachine")
	return ctrl.Result{}, nil
}

func (r *EvrocMachineReconciler) getBootstrapData(ctx context.Context, machine *clusterv1.Machine) ([]byte, error) {
	if machine.Spec.Bootstrap.DataSecretName == nil {
		return nil, fmt.Errorf("bootstrap data secret is not set")
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: machine.Namespace,
		Name:      *machine.Spec.Bootstrap.DataSecretName,
	}
	if err := r.Client.Get(ctx, key, secret); err != nil {
		return nil, fmt.Errorf("failed to get bootstrap data secret: %w", err)
	}

	data, ok := secret.Data["value"]
	if !ok {
		return nil, fmt.Errorf("bootstrap data secret does not contain 'value' key")
	}

	return data, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EvrocMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.EvrocMachine{}).
		Complete(r)
}
