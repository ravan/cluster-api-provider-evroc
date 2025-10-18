#!/usr/bin/env bash

# End-to-End Test Script for Rancher Turtles + EVROC CAPI Provider Integration
#
# Prerequisites:
#   1. Run ./test/e2e/setup-rancher-turtles.sh to setup Rancher management cluster
#   2. Run 'make install' to install EVROC CRDs
#   3. Run 'make run' in separate terminal to start EVROC provider locally
#
# Usage:
#   source test/e2e/.env
#   ./test/e2e/e2e-test-rancher.sh [run|create-cluster|monitor|cleanup]

set -o errexit
set -o nounset
set -o pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Configuration from environment
export KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-rancher-mgmt}"
export CLUSTER_NAME="${CLUSTER_NAME:-rancher-test-$(date +%s)}"
export RKE2_VERSION="${RKE2_VERSION:-v1.31.4+rke2r1}"
export CONTROL_PLANE_MACHINE_COUNT="${CONTROL_PLANE_MACHINE_COUNT:-1}"
export WORKER_MACHINE_COUNT="${WORKER_MACHINE_COUNT:-0}"
export EVROC_CONFIG="${EVROC_CONFIG:-${HOME}/.evroc/config.yaml}"

# Generated files
CLUSTER_MANIFEST="/tmp/rancher-cluster-${CLUSTER_NAME}.yaml"
KUBECONFIG_FILE="${SCRIPT_DIR}/${CLUSTER_NAME}-kubeconfig.yaml"
PLAN_FILE="${REPO_ROOT}/plan_rancher.md"

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

# Get current timestamp
timestamp() {
    date +"%Y-%m-%d %H:%M:%S"
}

#######################################
# Prerequisites Check
#######################################

check_prerequisites() {
    log_step "Checking prerequisites..."

    local all_ok=true

    # Check required tools
    local required_tools=("kubectl" "evroc" "kind")
    for tool in "${required_tools[@]}"; do
        if ! command -v "$tool" &> /dev/null; then
            log_error "Required tool not found: $tool"
            all_ok=false
        fi
    done

    # Check KIND cluster exists
    if ! kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
        log_error "KIND cluster '${KIND_CLUSTER_NAME}' not found"
        log_error "Please run: ./test/e2e/setup-rancher-turtles.sh"
        all_ok=false
    else
        log_info "KIND cluster found: ${KIND_CLUSTER_NAME}"
        # Set kubeconfig context
        kind export kubeconfig --name "${KIND_CLUSTER_NAME}"
    fi

    # Check Rancher components
    if kubectl get namespace cattle-system &> /dev/null; then
        log_info "Rancher Manager: ✓"
    else
        log_error "Rancher Manager not found (cattle-system namespace missing)"
        all_ok=false
    fi

    if kubectl get namespace rancher-turtles-system &> /dev/null; then
        log_info "Rancher Turtles: ✓"
    else
        log_error "Rancher Turtles not found (rancher-turtles-system namespace missing)"
        all_ok=false
    fi

    if kubectl get namespace capi-system &> /dev/null; then
        log_info "Cluster API: ✓"
    else
        log_error "Cluster API not found (capi-system namespace missing)"
        all_ok=false
    fi

    if kubectl get namespace rke2-control-plane-system &> /dev/null; then
        log_info "RKE2 Control Plane Provider: ✓"
    else
        log_error "RKE2 Control Plane Provider not found"
        all_ok=false
    fi

    # Check EVROC CRDs are installed
    if kubectl get crd evrocclusters.infrastructure.evroc.com &> /dev/null; then
        log_info "EVROC CRDs: ✓"
    else
        log_error "EVROC CRDs not installed"
        log_error "Please run: make install"
        all_ok=false
    fi

    # Check EVROC credentials
    if [ ! -f "${EVROC_CONFIG}" ]; then
        log_error "EVROC credentials not found at: ${EVROC_CONFIG}"
        log_error "Please run: evroc login"
        all_ok=false
    else
        log_info "EVROC credentials: ✓"
    fi

    # Check SSH key configuration
    if [ -z "${EVROC_SSH_KEY:-}" ]; then
        log_warn "No SSH key configured (EVROC_SSH_KEY is not set)"
        log_warn "VMs will be created without SSH access for troubleshooting"
        log_warn "To enable: configure EVROC_SSH_KEY in test/e2e/.env"
    else
        log_info "SSH key configured: ✓"
    fi

    if [ "$all_ok" = false ]; then
        log_error "Prerequisites check failed"
        exit 1
    fi

    log_success "All prerequisites satisfied"
}

#######################################
# RBAC Configuration
#######################################

apply_rbac() {
    log_step "Applying RBAC configuration for EVROC resources..."

    cd "${REPO_ROOT}"

    if kubectl apply -k config/rancher/; then
        log_success "RBAC configuration applied"
    else
        log_error "Failed to apply RBAC configuration"
        return 1
    fi

    # Verify key service accounts have permissions
    log_info "Verifying RBAC permissions..."

    local sa_list=(
        "system:serviceaccount:cattle-system:rancher"
        "system:serviceaccount:rancher-turtles-system:rancher-turtles-manager"
        "system:serviceaccount:capi-system:capi-controller-manager"
        "system:serviceaccount:capi-system:capi-manager"
        "system:serviceaccount:rke2-control-plane-system:rke2-control-plane-controller-manager"
        "system:serviceaccount:rke2-control-plane-system:rke2-control-plane-manager"
    )

    for sa in "${sa_list[@]}"; do
        if kubectl auth can-i get evrocclusters --as="${sa}" &> /dev/null; then
            log_info "  ${sa}: ✓"
        else
            log_warn "  ${sa}: permissions may not be set yet (controllers may not exist)"
        fi
    done
}

#######################################
# Cluster Manifest Generation
#######################################

generate_cluster_manifest() {
    log_step "Generating cluster manifest from template..."

    cd "${REPO_ROOT}"

    # Load environment variables
    if [ -f "${SCRIPT_DIR}/.env" ]; then
        source "${SCRIPT_DIR}/.env"
    fi

    # Base64 encode the kubeconfig
    export EVROC_KUBECONFIG_B64=$(cat "${EVROC_CONFIG}" | base64 | tr -d '\n')

    # Set defaults for variables (bash-style defaults need manual handling)
    export EVROC_VPC_NAME="${EVROC_VPC_NAME:-capi-test-vpc}"
    export EVROC_SUBNET_NAME="${EVROC_SUBNET_NAME:-capi-test-subnet}"
    export EVROC_SUBNET_CIDR="${EVROC_SUBNET_CIDR:-10.0.1.0/24}"
    export EVROC_CONTROL_PLANE_MACHINE_TYPE="${EVROC_CONTROL_PLANE_MACHINE_TYPE:-c1a.s}"
    export EVROC_WORKER_MACHINE_TYPE="${EVROC_WORKER_MACHINE_TYPE:-c1a.s}"
    export EVROC_IMAGE_NAME="${EVROC_IMAGE_NAME:-ubuntu-24.04}"
    export EVROC_DISK_SIZE="${EVROC_DISK_SIZE:-20}"
    export EVROC_SSH_KEY="${EVROC_SSH_KEY:-}"
    export POD_CIDR="${POD_CIDR:-10.244.0.0/16}"
    export SERVICE_CIDR="${SERVICE_CIDR:-10.96.0.0/12}"
    export EVROC_REGION="${EVROC_REGION:-eu-central-1}"
    export EVROC_PROJECT="${EVROC_PROJECT:-}"

    log_info "Cluster configuration:"
    log_info "  Name: ${CLUSTER_NAME}"
    log_info "  Region: ${EVROC_REGION}"
    log_info "  VPC: ${EVROC_VPC_NAME}"
    log_info "  Subnet: ${EVROC_SUBNET_NAME}"
    log_info "  Control Plane: ${CONTROL_PLANE_MACHINE_COUNT} x ${EVROC_CONTROL_PLANE_MACHINE_TYPE}"
    log_info "  Workers: ${WORKER_MACHINE_COUNT} x ${EVROC_WORKER_MACHINE_TYPE}"
    log_info "  RKE2 Version: ${RKE2_VERSION}"

    # Use envsubst with explicit variable list to avoid expanding cloud-init variables
    # This preserves variables like $instance_id, $short_id which are for cloud-init

    # Export all variables
    export CLUSTER_NAME EVROC_KUBECONFIG_B64 EVROC_REGION EVROC_PROJECT
    export EVROC_VPC_NAME EVROC_SUBNET_NAME EVROC_SUBNET_CIDR
    export EVROC_CONTROL_PLANE_MACHINE_TYPE EVROC_WORKER_MACHINE_TYPE
    export EVROC_IMAGE_NAME EVROC_DISK_SIZE EVROC_SSH_KEY
    export POD_CIDR SERVICE_CIDR
    export CONTROL_PLANE_MACHINE_COUNT WORKER_MACHINE_COUNT RKE2_VERSION

    # Use envsubst with explicit variable list (only substitute these, leave others alone)
    # Temporarily disable nounset to avoid errors with cloud-init variables like $instance_id
    set +u
    envsubst '${CLUSTER_NAME} ${EVROC_KUBECONFIG_B64} ${EVROC_REGION} ${EVROC_PROJECT} ${EVROC_VPC_NAME} ${EVROC_SUBNET_NAME} ${EVROC_SUBNET_CIDR} ${EVROC_CONTROL_PLANE_MACHINE_TYPE} ${EVROC_WORKER_MACHINE_TYPE} ${EVROC_IMAGE_NAME} ${EVROC_DISK_SIZE} ${EVROC_SSH_KEY} ${POD_CIDR} ${SERVICE_CIDR} ${CONTROL_PLANE_MACHINE_COUNT} ${WORKER_MACHINE_COUNT} ${RKE2_VERSION}' \
        < templates/cluster-template-rancher.yaml \
        > "${CLUSTER_MANIFEST}"
    set -u

    log_success "Cluster manifest generated: ${CLUSTER_MANIFEST}"
}

apply_cluster_manifest() {
    log_step "Applying cluster manifest..."

    if [ ! -f "${CLUSTER_MANIFEST}" ]; then
        log_error "Cluster manifest not found: ${CLUSTER_MANIFEST}"
        return 1
    fi

    log_info "Applying to management cluster..."
    kubectl apply -f "${CLUSTER_MANIFEST}"

    log_success "Cluster manifest applied"
}

#######################################
# Cluster Monitoring
#######################################

wait_for_evroccluster_ready() {
    log_step "Waiting for EvrocCluster to become ready..."

    local timeout=300
    local elapsed=0
    local interval=5

    while [ $elapsed -lt $timeout ]; do
        local ready=$(kubectl get evroccluster "${CLUSTER_NAME}" -o jsonpath='{.status.ready}' 2>/dev/null || echo "false")

        if [ "$ready" = "true" ]; then
            log_success "EvrocCluster is ready"
            kubectl get evroccluster "${CLUSTER_NAME}" -o wide
            return 0
        fi

        log_info "Waiting for EvrocCluster... ($elapsed/$timeout seconds)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done

    log_error "Timeout waiting for EvrocCluster to become ready"
    kubectl describe evroccluster "${CLUSTER_NAME}"
    return 1
}

wait_for_machine_creation() {
    log_step "Waiting for Machine and EvrocMachine creation..."

    local timeout=600
    local elapsed=0
    local interval=10

    while [ $elapsed -lt $timeout ]; do
        local machine_count=$(kubectl get machines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}" --no-headers 2>/dev/null | wc -l)

        if [ "$machine_count" -gt 0 ]; then
            log_success "Machine(s) created"
            kubectl get machines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}"
            kubectl get evrocmachines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}"
            return 0
        fi

        log_info "Waiting for Machine creation... ($elapsed/$timeout seconds)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done

    log_error "Timeout waiting for Machine creation"
    return 1
}

get_machine_external_ip() {
    local machine_name=$1
    kubectl get evrocmachine "${machine_name}" -o jsonpath='{.status.addresses[?(@.type=="ExternalIP")].address}' 2>/dev/null
}

wait_for_vm_running() {
    log_step "Waiting for VMs to be running in EVROC cloud..."

    local timeout=180
    local elapsed=0
    local interval=10

    while [ $elapsed -lt $timeout ]; do
        local evrocmachines=$(kubectl get evrocmachines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)

        if [ -z "$evrocmachines" ]; then
            log_info "No EvrocMachines found yet... ($elapsed/$timeout seconds)"
            sleep $interval
            elapsed=$((elapsed + interval))
            continue
        fi

        local all_running=true
        for machine in $evrocmachines; do
            local ready=$(kubectl get evrocmachine "$machine" -o jsonpath='{.status.ready}' 2>/dev/null || echo "false")
            if [ "$ready" != "true" ]; then
                all_running=false
                break
            fi
        done

        if [ "$all_running" = true ]; then
            log_success "All VMs are running"
            kubectl get evrocmachines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}" -o wide
            return 0
        fi

        log_info "Waiting for VMs to be ready... ($elapsed/$timeout seconds)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done

    log_error "Timeout waiting for VMs to be ready"
    return 1
}

#######################################
# RKE2 Auto-Fix
#######################################

check_rke2() {
    log_step "Checking RKE2 installation on VMs..."

    # Get control plane machines
    # Note: The control-plane label might have an empty value, so we check for the label's existence
    local machines=$(kubectl get evrocmachines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}",cluster.x-k8s.io/control-plane -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)

    if [ -z "$machines" ]; then
        log_warn "No control plane machines found, trying all machines in cluster..."
        machines=$(kubectl get evrocmachines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)

        if [ -z "$machines" ]; then
            log_warn "No machines found at all"
            return 1
        fi
    fi

    for machine in $machines; do
        log_info "Checking RKE2 on machine: $machine"

        local external_ip=$(get_machine_external_ip "$machine")
        if [ -z "$external_ip" ]; then
            log_warn "No external IP found for machine $machine"
            continue
        fi

        log_info "  External IP: $external_ip"

        # Wait a bit for VM to fully boot
        log_info "  Waiting 60 seconds for VM to boot..."
        sleep 60

        # Check if SSH is accessible
        if [ -z "${EVROC_SSH_PRIVATE_KEY:-}" ] || [ ! -f "${EVROC_SSH_PRIVATE_KEY}" ]; then
            log_warn "  SSH key not configured, cannot check RKE2 status"
            log_warn "  Set EVROC_SSH_PRIVATE_KEY in test/e2e/.env"
            continue
        fi

        # Try to check if RKE2 is running
        log_info "  Checking if RKE2 is running..."
        if ssh -i "${EVROC_SSH_PRIVATE_KEY}" -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
            evroc-user@"${external_ip}" "sudo systemctl is-active rke2-server" &> /dev/null; then
            log_success "  RKE2 is already running on $machine"
            continue
        fi

        log_warn "  RKE2 is not running, checking for cloud-init error..."

        # Check for cloud-init error
        if ssh -i "${EVROC_SSH_PRIVATE_KEY}" -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
            evroc-user@"${external_ip}" "sudo grep -q 'Syntax error' /var/log/cloud-init-output.log 2>/dev/null" &> /dev/null; then
            log_error "  Cloud-init syntax error detected!"
        else
            log_warn "  RKE2 not running but no cloud-init error detected"
            log_warn "  It may still be installing, please check manually"
        fi
    done

    log_success "RKE2 check completed"
}

wait_for_api_server() {
    log_step "Waiting for Kubernetes API server to be ready..."

    # Get control plane endpoint
    local endpoint=$(kubectl get cluster "${CLUSTER_NAME}" -o jsonpath='{.spec.controlPlaneEndpoint.host}' 2>/dev/null)
    local port=$(kubectl get cluster "${CLUSTER_NAME}" -o jsonpath='{.spec.controlPlaneEndpoint.port}' 2>/dev/null || echo "6443")

    if [ -z "$endpoint" ]; then
        log_error "Control plane endpoint not set"
        return 1
    fi

    log_info "Control plane endpoint: https://${endpoint}:${port}"

    local timeout=600
    local elapsed=0
    local interval=10

    while [ $elapsed -lt $timeout ]; do
        if curl -k -s --connect-timeout 5 "https://${endpoint}:${port}/livez" &> /dev/null; then
            log_success "API server is responding"
            return 0
        fi

        log_info "Waiting for API server... ($elapsed/$timeout seconds)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done

    log_error "Timeout waiting for API server"
    return 1
}

#######################################
# Cluster Verification
#######################################

retrieve_kubeconfig() {
    log_step "Retrieving workload cluster kubeconfig..."

    # Wait for kubeconfig secret
    local timeout=300
    local elapsed=0
    local interval=10

    while [ $elapsed -lt $timeout ]; do
        if kubectl get secret "${CLUSTER_NAME}-kubeconfig" &> /dev/null; then
            kubectl get secret "${CLUSTER_NAME}-kubeconfig" \
                -o jsonpath='{.data.value}' | base64 -d \
                > "${KUBECONFIG_FILE}"

            log_success "Kubeconfig saved to: ${KUBECONFIG_FILE}"
            return 0
        fi

        log_info "Waiting for kubeconfig secret... ($elapsed/$timeout seconds)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done

    log_error "Timeout waiting for kubeconfig secret"
    return 1
}

verify_cluster_ready() {
    log_step "Verifying workload cluster is ready..."

    if [ ! -f "${KUBECONFIG_FILE}" ]; then
        log_error "Kubeconfig file not found: ${KUBECONFIG_FILE}"
        return 1
    fi

    log_info "Testing access to workload cluster..."

    local timeout=300
    local elapsed=0
    local interval=10

    while [ $elapsed -lt $timeout ]; do
        if kubectl --kubeconfig="${KUBECONFIG_FILE}" get nodes &> /dev/null; then
            log_success "Successfully accessed workload cluster"
            echo ""
            kubectl --kubeconfig="${KUBECONFIG_FILE}" get nodes -o wide
            echo ""
            return 0
        fi

        log_info "Waiting for nodes to be ready... ($elapsed/$timeout seconds)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done

    log_error "Timeout waiting for nodes to be ready"
    return 1
}

#######################################
# Iteration Logging
#######################################

log_iteration() {
    local iteration_num=$1
    local title="$2"
    local status="$3"
    local details="$4"

    cat >> "${PLAN_FILE}" << EOF

---

## Iteration ${iteration_num}: ${title}

### Date: $(date +"%Y-%m-%d")
### Time: $(timestamp)
### Status: ${status}

${details}

EOF

    log_info "Logged iteration to: ${PLAN_FILE}"
}

#######################################
# Cleanup
#######################################

cleanup() {
    local cluster_name="${1:-${CLUSTER_NAME}}"

    log_info "=== Cleaning up cluster: ${cluster_name} ==="
    echo ""

    # Set kubeconfig context
    kind export kubeconfig --name "${KIND_CLUSTER_NAME}" &> /dev/null || true

    # Check if cluster exists
    if ! kubectl get cluster "${cluster_name}" &> /dev/null; then
        log_warn "Cluster '${cluster_name}' not found in management cluster"
    else
        log_info "Deleting CAPI cluster '${cluster_name}'..."

        # List all resources that will be deleted
        log_info "Resources to be deleted:"
        kubectl get cluster,evroccluster,machines,evrocmachines,rke2controlplane,machinedeployment \
            -l cluster.x-k8s.io/cluster-name="${cluster_name}" 2>/dev/null || true
        echo ""

        # Delete the cluster (this will cascade to all related resources)
        log_info "Deleting cluster object (this will cascade delete all resources)..."
        kubectl delete cluster "${cluster_name}" --wait=false

        # Wait for cluster deletion with timeout
        log_info "Waiting for cluster deletion (timeout: 10 minutes)..."
        kubectl wait --for=delete cluster/"${cluster_name}" --timeout=10m 2>/dev/null || {
            log_warn "Timeout waiting for cluster deletion, checking remaining resources..."

            # Show what's still there
            log_info "Remaining resources:"
            kubectl get cluster,evroccluster,machines,evrocmachines \
                -l cluster.x-k8s.io/cluster-name="${cluster_name}" 2>/dev/null || true

            # Force delete stuck resources if needed
            log_warn "Some resources may be stuck. Checking for finalizers..."
            for resource in evrocmachines machines evrocclusters; do
                local items=$(kubectl get $resource -l cluster.x-k8s.io/cluster-name="${cluster_name}" -o name 2>/dev/null || true)
                if [ -n "$items" ]; then
                    log_info "Removing finalizers from $resource..."
                    for item in $items; do
                        kubectl patch $item -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
                    done
                fi
            done
        }

        log_success "Cluster deleted from management cluster"
    fi

    # Clean up VMs in EVROC cloud (in case they weren't deleted by the controller)
    log_info "Checking for orphaned VMs in EVROC cloud..."
    local vms=$(evroc compute vm list | grep "${cluster_name}" | awk '{print $1}' || true)
    if [ -n "$vms" ]; then
        log_warn "Found VMs in EVROC cloud matching cluster name:"
        echo "$vms"

        for vm in $vms; do
            log_info "Deleting VM: $vm"
            evroc compute vm delete "$vm" --force-yes 2>&1 || log_warn "Failed to delete VM $vm"
        done
    else
        log_info "No orphaned VMs found in EVROC cloud"
    fi

    # Clean up generated files
    log_info "Cleaning up generated files..."
    rm -f "/tmp/rancher-cluster-${cluster_name}.yaml"
    rm -f "${SCRIPT_DIR}/${cluster_name}-kubeconfig.yaml"
    rm -f "/tmp/${cluster_name}-kubeconfig.yaml"

    log_success "=== Cleanup complete for cluster: ${cluster_name} ==="
    echo ""
}

cleanup_all() {
    log_info "=== Cleaning up ALL test clusters ==="
    echo ""

    # Set kubeconfig context
    kind export kubeconfig --name "${KIND_CLUSTER_NAME}" &> /dev/null || true

    # Find all EVROC test clusters
    local clusters=$(kubectl get clusters -o jsonpath='{.items[*].metadata.name}' 2>/dev/null | tr ' ' '\n' | grep -E "^(evroc-test-|rancher-test-)" || true)

    if [ -z "$clusters" ]; then
        log_info "No test clusters found"
    else
        log_info "Found test clusters:"
        echo "$clusters"
        echo ""

        for cluster in $clusters; do
            cleanup "$cluster"
        done
    fi

    # Clean up all VMs matching test patterns
    log_info "Checking for all test VMs in EVROC cloud..."
    local all_vms=$(evroc compute vm list | grep -E "(evroc-test-|rancher-test-)" | awk '{print $1}' || true)
    if [ -n "$all_vms" ]; then
        log_warn "Found test VMs in EVROC cloud:"
        echo "$all_vms"

        for vm in $all_vms; do
            log_info "Deleting VM: $vm"
            evroc compute vm delete "$vm" --force-yes 2>&1 || log_warn "Failed to delete VM $vm"
        done
    fi

    # Clean up all generated files
    log_info "Cleaning up all generated files..."
    rm -f /tmp/rancher-cluster-*.yaml
    rm -f /tmp/evroc-test-*-kubeconfig.yaml
    rm -f /tmp/rancher-test-*-kubeconfig.yaml
    rm -f "${SCRIPT_DIR}"/evroc-test-*-kubeconfig.yaml
    rm -f "${SCRIPT_DIR}"/rancher-test-*-kubeconfig.yaml

    log_success "=== All test clusters cleaned up ==="
    echo ""
}

#######################################
# Main Workflows
#######################################

create_cluster() {
    log_info "=== Creating Rancher Turtles + EVROC Cluster ==="
    log_info "Cluster name: ${CLUSTER_NAME}"
    echo ""

    local start_time=$(date +%s)

    check_prerequisites
    apply_rbac
    generate_cluster_manifest
    apply_cluster_manifest
    wait_for_evroccluster_ready
    wait_for_machine_creation
    wait_for_vm_running
    check_rke2
    wait_for_api_server
    retrieve_kubeconfig
    verify_cluster_ready

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    log_success "=== Cluster creation completed in ${duration} seconds ==="
    echo ""
    log_info "To access the workload cluster:"
    log_info "  export KUBECONFIG=${KUBECONFIG_FILE}"
    log_info "  kubectl get nodes"
    echo ""
    log_info "To clean up:"
    log_info "  ${SCRIPT_DIR}/e2e-test-rancher.sh cleanup"
    echo ""

    # Log iteration
    log_iteration "AUTO" "Automated E2E Test" "SUCCESS" \
"### Cluster Details
- **Cluster Name**: ${CLUSTER_NAME}
- **Duration**: ${duration} seconds
- **RKE2 Version**: ${RKE2_VERSION}
- **Control Plane Nodes**: ${CONTROL_PLANE_MACHINE_COUNT}
- **Worker Nodes**: ${WORKER_MACHINE_COUNT}

### Status
✅ Cluster created and verified successfully
"
}

monitor_cluster() {
    log_info "=== Monitoring cluster: ${CLUSTER_NAME} ==="
    echo ""

    check_prerequisites

    log_info "Cluster status:"
    kubectl get cluster "${CLUSTER_NAME}" -o wide || log_warn "Cluster not found"
    echo ""

    log_info "EvrocCluster status:"
    kubectl get evroccluster "${CLUSTER_NAME}" -o wide || log_warn "EvrocCluster not found"
    echo ""

    log_info "Machines:"
    kubectl get machines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}" || log_warn "No machines found"
    echo ""

    log_info "EvrocMachines:"
    kubectl get evrocmachines -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}" -o wide || log_warn "No EvrocMachines found"
    echo ""

    log_info "RKE2 Control Plane:"
    kubectl get rke2controlplane -l cluster.x-k8s.io/cluster-name="${CLUSTER_NAME}" || log_warn "No RKE2 control plane found"
    echo ""
}

#######################################
# Main Entry Point
#######################################

main() {
    case "${1:-run}" in
        run|create-cluster)
            create_cluster
            ;;
        monitor)
            monitor_cluster
            ;;
        cleanup)
            # Support optional cluster name argument
            if [ -n "${2:-}" ]; then
                cleanup "$2"
            else
                cleanup
            fi
            ;;
        cleanup-all)
            cleanup_all
            ;;
        *)
            echo "Usage: $0 [run|create-cluster|monitor|cleanup|cleanup-all]"
            echo ""
            echo "Commands:"
            echo "  run            - Create and test cluster (default)"
            echo "  create-cluster - Same as 'run'"
            echo "  monitor        - Show status of existing cluster"
            echo "  cleanup [NAME] - Delete cluster and clean up (optionally specify cluster name)"
            echo "  cleanup-all    - Delete ALL test clusters and clean up everything"
            echo ""
            echo "Prerequisites:"
            echo "  1. Run: ./test/e2e/setup-rancher-turtles.sh"
            echo "  2. Run: make install"
            echo "  3. Run: make run (in separate terminal)"
            echo ""
            echo "Usage Examples:"
            echo "  # Create a cluster"
            echo "  export KIND_CLUSTER_NAME=\"rancher-mgmt\""
            echo "  source test/e2e/.env"
            echo "  ./test/e2e/e2e-test-rancher.sh run"
            echo ""
            echo "  # Clean up current cluster"
            echo "  ./test/e2e/e2e-test-rancher.sh cleanup"
            echo ""
            echo "  # Clean up specific cluster"
            echo "  ./test/e2e/e2e-test-rancher.sh cleanup evroc-test-1760779483"
            echo ""
            echo "  # Clean up ALL test clusters"
            echo "  ./test/e2e/e2e-test-rancher.sh cleanup-all"
            exit 1
            ;;
    esac
}

# Run main
main "$@"
