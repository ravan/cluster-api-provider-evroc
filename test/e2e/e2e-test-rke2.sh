#!/usr/bin/env bash

# End-to-End Test Script for Cluster API Provider Evroc with RKE2
# This script creates a kind management cluster, deploys the Evroc CAPI provider,
# and creates a test cluster on real Evroc infrastructure using RKE2.

set -o errexit
set -o nounset
set -o pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Default values
export KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-capi-evroc-e2e}"
export CLUSTER_NAME="${CLUSTER_NAME:-evroc-rke2-test-cluster}"
export RKE2_VERSION="${RKE2_VERSION:-v1.31.4+rke2r1}"
export CONTROL_PLANE_MACHINE_COUNT="${CONTROL_PLANE_MACHINE_COUNT:-1}"
export WORKER_MACHINE_COUNT="${WORKER_MACHINE_COUNT:-0}"
export PROVIDER_VERSION="${PROVIDER_VERSION:-v0.1.0}"
export EVROC_CONFIG="${EVROC_CONFIG:-${HOME}/.evroc/config.yaml}"

# Environment configuration is loaded from .env file
# Source it before running this script:
#   source test/e2e/.env

# Functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

check_prerequisites() {
    log_info "Checking prerequisites..."

    local missing_tools=()

    if ! command -v kind &> /dev/null; then
        missing_tools+=("kind")
    fi

    if ! command -v kubectl &> /dev/null; then
        missing_tools+=("kubectl")
    fi

    if ! command -v clusterctl &> /dev/null; then
        missing_tools+=("clusterctl")
    fi

    if ! command -v docker &> /dev/null; then
        missing_tools+=("docker")
    fi

    if ! command -v evroc &> /dev/null; then
        missing_tools+=("evroc")
    fi

    if [ ${#missing_tools[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        log_error "Please install them and try again"
        exit 1
    fi

    # Check if Evroc config exists
    if [ ! -f "${EVROC_CONFIG}" ]; then
        log_error "Evroc config not found at: ${EVROC_CONFIG}"
        log_error "Please run 'evroc login' or set EVROC_CONFIG"
        exit 1
    fi

    # Check SSH key configuration
    if [ -z "${EVROC_SSH_KEY:-}" ]; then
        log_warn "No SSH key configured (EVROC_SSH_KEY is not set)"
        log_warn "VMs will be created without SSH access for troubleshooting"
        log_warn "To enable SSH access:"
        log_warn "  1. Generate a key: ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519"
        log_warn "  2. Update test/e2e/.env to include the public key"
        log_warn "  3. Re-source: source test/e2e/.env"
    else
        log_info "SSH key configured for VM access"
        if [ -n "${EVROC_SSH_PRIVATE_KEY:-}" ] && [ -f "${EVROC_SSH_PRIVATE_KEY}" ]; then
            log_info "SSH private key: ${EVROC_SSH_PRIVATE_KEY}"
        fi
    fi

    log_info "All prerequisites satisfied"
}

create_kind_cluster() {
    log_info "Creating kind management cluster: ${KIND_CLUSTER_NAME}"

    if kind get clusters | grep -q "^${KIND_CLUSTER_NAME}$"; then
        log_warn "Kind cluster ${KIND_CLUSTER_NAME} already exists, skipping creation"
        return 0
    fi

    cat <<EOF | kind create cluster --name "${KIND_CLUSTER_NAME}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraMounts:
      - hostPath: ${EVROC_CONFIG}
        containerPath: /root/.evroc/config.yaml
        readOnly: true
EOF

    log_info "Waiting for kind cluster to be ready..."
    kubectl wait --for=condition=Ready nodes --all --timeout=2m
}

install_cert_manager() {
    log_info "Installing cert-manager..."

    if kubectl get namespace cert-manager &> /dev/null; then
        log_warn "cert-manager already installed, skipping"
        return 0
    fi

    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml

    log_info "Waiting for cert-manager to be ready..."
    kubectl wait --for=condition=Available --timeout=5m \
        -n cert-manager deployment/cert-manager \
        deployment/cert-manager-cainjector \
        deployment/cert-manager-webhook
}

install_provider_crds() {
    log_info "Installing provider CRDs..."

    cd "${REPO_ROOT}"

    # Install only CRDs (not the controller)
    # The controller will be run locally via 'make run'
    make install

    log_info "CRDs installed successfully"
}

initialize_cluster_api() {
    log_info "Installing Cluster API core components with clusterctl..."

    # Install CAPI core components and RKE2 providers
    clusterctl init \
        --core cluster-api \
        --bootstrap rke2 \
        --control-plane rke2

    log_info "Waiting for CAPI core controllers to be ready..."
    kubectl wait --for=condition=Available --timeout=5m \
        -n capi-system deployment/capi-controller-manager
    kubectl wait --for=condition=Available --timeout=5m \
        -n rke2-bootstrap-system deployment/rke2-bootstrap-controller-manager
    kubectl wait --for=condition=Available --timeout=5m \
        -n rke2-control-plane-system deployment/rke2-control-plane-controller-manager
}

configure_capi_rbac() {
    log_info "Configuring RBAC for CAPI to access Evroc resources..."

    cd "${REPO_ROOT}"

    # Apply RBAC from repository files (manually maintained, not generated)
    # These files grant the CAPI manager service accounts permissions to manage
    # Evroc infrastructure resources (evrocmachines, evrocclusters, etc.)
    # This includes: capi-manager, capi-kubeadm-control-plane-manager, and rke2-control-plane-manager
    kubectl apply -f config/rbac/capi_manager_role.yaml
    kubectl apply -f config/rbac/capi_manager_role_binding.yaml

    log_info "CAPI RBAC configured successfully (includes RKE2 control plane manager)"
}

configure_rke2_rbac() {
    log_info "Configuring additional RBAC for RKE2 control plane (optional, redundant with capi_manager RBAC)..."

    cd "${REPO_ROOT}"

    # Apply dedicated RBAC for RKE2 control plane controller
    # NOTE: This is now redundant since rke2-control-plane-manager is included in
    # capi_manager_role_binding.yaml, but we keep this for backwards compatibility
    # and as an alternative approach for environments that need separate RBAC.
    kubectl apply -f config/rbac/rke2_control_plane_role.yaml
    kubectl apply -f config/rbac/rke2_control_plane_role_binding.yaml

    log_info "RKE2 control plane RBAC configured (additional/redundant)"
}

build_and_load_provider_image() {
    log_info "Building provider image..."

    cd "${REPO_ROOT}"

    # Build the docker image
    make docker-build IMG="controller:${PROVIDER_VERSION}"

    log_info "Loading image into kind cluster..."
    kind load docker-image "controller:${PROVIDER_VERSION}" --name "${KIND_CLUSTER_NAME}"
}

deploy_evroc_provider() {
    log_info "Deploying Evroc CAPI provider using make deploy..."

    cd "${REPO_ROOT}"

    # Use make deploy to install our provider
    # This uses kustomize to build and apply CRDs + RBAC + Deployment
    make deploy IMG=controller:v0.1.0

    log_info "Waiting for Evroc provider to be ready..."
    kubectl wait --for=condition=Available --timeout=5m \
        -n cluster-api-provider-evroc-system deployment/cluster-api-provider-evroc-controller-manager || {
        log_warn "Provider deployment not ready yet, checking pods..."
        kubectl get pods -n cluster-api-provider-evroc-system
    }
}

create_evroc_credentials_secret() {
    log_info "Skipping separate secret creation - secret will be created by cluster template"
    # The cluster template includes the secret definition with the correct name
    # No need to create it separately here
}

generate_and_apply_cluster() {
    log_info "Generating RKE2 cluster manifest..."

    cd "${REPO_ROOT}"

    # Export variables for template substitution
    # Base64 encode the kubeconfig to avoid YAML indentation issues
    export EVROC_KUBECONFIG_B64=$(cat "${EVROC_CONFIG}" | base64 | tr -d '\n')
    export EVROC_VPC_NAME="${EVROC_VPC_NAME:-capi-test-vpc}"
    export EVROC_SUBNET_NAME="${EVROC_SUBNET_NAME:-capi-test-subnet}"
    export EVROC_SUBNET_CIDR="${EVROC_SUBNET_CIDR:-10.0.1.0/24}"
    export EVROC_CONTROL_PLANE_MACHINE_TYPE="${EVROC_CONTROL_PLANE_MACHINE_TYPE:-c1a.s}"
    export EVROC_WORKER_MACHINE_TYPE="${EVROC_WORKER_MACHINE_TYPE:-c1a.s}"
    export EVROC_IMAGE_NAME="${EVROC_IMAGE_NAME:-ubuntu-minimal.24-04.1}"
    export EVROC_DISK_SIZE="${EVROC_DISK_SIZE:-20}"
    export EVROC_SSH_KEY="${EVROC_SSH_KEY:-}"
    export POD_CIDR="${POD_CIDR:-192.168.0.0/16}"
    export SERVICE_CIDR="${SERVICE_CIDR:-10.96.0.0/12}"

    # Generate cluster manifest using clusterctl
    clusterctl generate cluster "${CLUSTER_NAME}" \
        --from templates/cluster-template-rke2.yaml \
        --target-namespace default \
        --kubernetes-version "${RKE2_VERSION}" \
        --control-plane-machine-count "${CONTROL_PLANE_MACHINE_COUNT}" \
        --worker-machine-count "${WORKER_MACHINE_COUNT}" \
        > "${SCRIPT_DIR}/generated-cluster-rke2.yaml"

    log_info "Applying RKE2 cluster manifest..."
    kubectl apply -f "${SCRIPT_DIR}/generated-cluster-rke2.yaml"
}

watch_cluster_creation() {
    log_info "Watching RKE2 cluster creation (this may take 10-15 minutes)..."
    log_info "You can press Ctrl+C to stop watching (cluster creation will continue)"

    # Watch cluster status
    timeout 1800 bash -c '
        while true; do
            STATUS=$(kubectl get cluster '"${CLUSTER_NAME}"' -o jsonpath="{.status.phase}" 2>/dev/null || echo "NotFound")
            echo "[$(date +%H:%M:%S)] Cluster phase: $STATUS"

            if [ "$STATUS" = "Provisioned" ]; then
                echo "Cluster is provisioned!"
                break
            fi

            # Show machine status
            echo "Machines:"
            kubectl get machines -l cluster.x-k8s.io/cluster-name='"${CLUSTER_NAME}"' 2>/dev/null || true

            sleep 30
        done
    ' || log_warn "Watch timed out or was interrupted"

    # Show final status
    log_info "Cluster status:"
    kubectl get cluster "${CLUSTER_NAME}" -o wide
    kubectl get evroccluster "${CLUSTER_NAME}" -o wide
    kubectl get machines -l "cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}"
}

get_workload_kubeconfig() {
    log_info "Retrieving workload cluster kubeconfig..."

    # Wait for kubeconfig secret
    kubectl wait --for=jsonpath='{.data.value}' \
        --timeout=10m \
        secret/"${CLUSTER_NAME}-kubeconfig" 2>/dev/null || {
        log_warn "Kubeconfig secret not ready yet"
        return 1
    }

    # Save kubeconfig
    kubectl get secret "${CLUSTER_NAME}-kubeconfig" \
        -o jsonpath='{.data.value}' | base64 -d \
        > "${SCRIPT_DIR}/${CLUSTER_NAME}-kubeconfig.yaml"

    log_info "Kubeconfig saved to: ${SCRIPT_DIR}/${CLUSTER_NAME}-kubeconfig.yaml"

    # Test access to workload cluster
    log_info "Testing access to workload cluster..."
    kubectl --kubeconfig="${SCRIPT_DIR}/${CLUSTER_NAME}-kubeconfig.yaml" get nodes || {
        log_warn "Cannot access workload cluster yet"
        return 1
    }
}

cleanup() {
    log_info "Cleaning up..."

    # Delete workload cluster
    if kubectl get cluster "${CLUSTER_NAME}" &> /dev/null; then
        log_info "Deleting workload cluster..."
        kubectl delete cluster "${CLUSTER_NAME}"

        # Wait for deletion
        kubectl wait --for=delete cluster/"${CLUSTER_NAME}" --timeout=10m || true
    fi

    # Delete kind cluster
    if kind get clusters | grep -q "^${KIND_CLUSTER_NAME}$"; then
        log_info "Deleting kind management cluster..."
        kind delete cluster --name "${KIND_CLUSTER_NAME}"
    fi

    # Clean up generated files
    rm -f "${SCRIPT_DIR}/generated-cluster-rke2.yaml"
    rm -f "${SCRIPT_DIR}"/*-kubeconfig.yaml

    log_info "Cleanup complete"
}

setup_local_dev() {
    log_info "Setting up environment for local development with RKE2"
    log_info "Management cluster: ${KIND_CLUSTER_NAME}"
    log_info ""

    # Set up trap for cleanup on error
    trap 'log_error "Setup failed!"; exit 1' ERR

    check_prerequisites
    create_kind_cluster
    install_cert_manager
    install_provider_crds
    initialize_cluster_api
    configure_capi_rbac
    configure_rke2_rbac
    create_evroc_credentials_secret

    log_info ""
    log_info "✅ Local development environment ready for RKE2!"
    log_info ""
    log_info "Next steps:"
    log_info "  1. Run the operator locally:"
    log_info "     make run"
    log_info ""
    log_info "  2. In another terminal, create a test cluster:"
    log_info "     source test/e2e/.env"
    log_info "     ${SCRIPT_DIR}/e2e-test-rke2.sh create-cluster"
    log_info ""
    log_info "  3. To clean up:"
    log_info "     ${SCRIPT_DIR}/e2e-test-rke2.sh cleanup"
}

create_cluster_only() {
    log_info "Creating RKE2 workload cluster: ${CLUSTER_NAME}"
    log_info ""

    # Verify kind cluster exists
    if ! kind get clusters | grep -q "^${KIND_CLUSTER_NAME}$"; then
        log_error "Kind cluster ${KIND_CLUSTER_NAME} not found"
        log_error "Run '${SCRIPT_DIR}/e2e-test-rke2.sh setup-local' first"
        exit 1
    fi

    generate_and_apply_cluster
    watch_cluster_creation
    get_workload_kubeconfig

    log_info ""
    log_info "✅ RKE2 cluster creation completed!"
    log_info ""
    log_info "To access the workload cluster:"
    log_info "  export KUBECONFIG=${SCRIPT_DIR}/${CLUSTER_NAME}-kubeconfig.yaml"
    log_info "  kubectl get nodes"
    log_info ""

    # Show SSH access instructions if SSH key was configured
    if [ -n "${EVROC_SSH_KEY:-}" ]; then
        log_info "To SSH into VMs for troubleshooting:"
        log_info "  source test/e2e/.env  # Load SSH key configuration"
        log_info "  test/e2e/ssh-to-vm.sh <machine-name>"
        log_info ""
        log_info "To list machines:"
        log_info "  kubectl get evrocmachines"
        log_info "  kubectl get machines"
    else
        log_warn "SSH access not configured - VMs cannot be accessed directly"
        log_warn "To enable SSH for future clusters, configure EVROC_SSH_KEY in test/e2e/.env"
    fi
}

main() {
    log_info "Starting Evroc CAPI E2E Test with RKE2 (Full Run)"
    log_info "Management cluster: ${KIND_CLUSTER_NAME}"
    log_info "Workload cluster: ${CLUSTER_NAME}"

    # Set up trap for cleanup on error
    trap 'log_error "Test failed!"; exit 1' ERR

    check_prerequisites
    create_kind_cluster
    install_cert_manager
    build_and_load_provider_image
    initialize_cluster_api
    configure_capi_rbac
    configure_rke2_rbac
    deploy_evroc_provider
    create_evroc_credentials_secret
    generate_and_apply_cluster
    watch_cluster_creation
    get_workload_kubeconfig

    log_info "E2E test with RKE2 completed successfully!"
    log_info "To access the workload cluster:"
    log_info "  export KUBECONFIG=${SCRIPT_DIR}/${CLUSTER_NAME}-kubeconfig.yaml"
    log_info "  kubectl get nodes"
    log_info ""

    # Show SSH access instructions if SSH key was configured
    if [ -n "${EVROC_SSH_KEY:-}" ]; then
        log_info "To SSH into VMs for troubleshooting:"
        log_info "  source test/e2e/.env  # Load SSH key configuration"
        log_info "  test/e2e/ssh-to-vm.sh <machine-name>"
        log_info ""
        log_info "To list machines:"
        log_info "  kubectl get evrocmachines"
        log_info ""
    fi

    log_info "To clean up:"
    log_info "  ${SCRIPT_DIR}/e2e-test-rke2.sh cleanup"
}

# Handle command line arguments
case "${1:-run}" in
    run)
        main
        ;;
    setup-local)
        setup_local_dev
        ;;
    create-cluster)
        create_cluster_only
        ;;
    cleanup)
        cleanup
        ;;
    *)
        echo "Usage: $0 [run|setup-local|create-cluster|cleanup]"
        echo ""
        echo "Commands:"
        echo "  run           - Full e2e test with in-cluster operator (CI mode)"
        echo "  setup-local   - Set up environment for local development with RKE2"
        echo "  create-cluster - Create RKE2 workload cluster (requires setup-local first)"
        echo "  cleanup       - Clean up all resources"
        echo ""
        echo "Development workflow:"
        echo "  1. source test/e2e/.env"
        echo "  2. test/e2e/e2e-test-rke2.sh setup-local"
        echo "  3. make run  (in a separate terminal)"
        echo "  4. test/e2e/e2e-test-rke2.sh create-cluster"
        exit 1
        ;;
esac
