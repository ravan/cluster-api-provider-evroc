#!/usr/bin/env bash

# setup-rancher-turtles.sh - Setup Rancher Manager + Turtles + EVROC Provider
#
# This script automates the deployment of a complete Rancher + CAPI + EVROC development environment
# on a local KIND cluster. It includes:
#   - KIND cluster creation with proper port mappings
#   - cert-manager installation
#   - Rancher Manager installation
#   - Rancher Turtles (CAPI extension) installation
#   - EVROC CAPI provider registration
#
# Usage:
#   ./hack/setup-rancher-turtles.sh [OPTIONS]
#
# Options:
#   -h, --help              Show this help message
#   -v, --version           Show component versions
#   --dry-run               Show what would be installed without making changes
#   --cleanup               Delete KIND cluster and clean up
#   --skip-prerequisites    Skip prerequisite checks (not recommended)
#   --evroc-credentials     Path to EVROC credentials (default: ~/.evroc/config.yaml)
#   --external-hostname     External hostname for Rancher (e.g., ngrok URL)
#
# Exit codes:
#   0 - Success
#   1 - Prerequisites failure
#   2 - Installation failure

set -euo pipefail

# Determine script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Source library functions
# shellcheck source=./lib/logging.sh
source "${SCRIPT_DIR}/lib/logging.sh"
# shellcheck source=./lib/prerequisites.sh
source "${SCRIPT_DIR}/lib/prerequisites.sh"
# shellcheck source=./lib/k8s-helpers.sh
source "${SCRIPT_DIR}/lib/k8s-helpers.sh"

#######################################
# Configuration Variables
#######################################

# Component versions
readonly RANCHER_VERSION="${RANCHER_VERSION:-2.12.1}"
readonly TURTLES_VERSION="${TURTLES_VERSION:-0.24.0}"
readonly CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-1.16.3}"
readonly KUBERNETES_VERSION="${KUBERNETES_VERSION:-1.31.4}"

# Cluster configuration
readonly KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-rancher-mgmt}"
readonly RANCHER_HOSTNAME="${RANCHER_HOSTNAME:-localhost}"
readonly RANCHER_BOOTSTRAP_PASSWORD="${RANCHER_BOOTSTRAP_PASSWORD:-admin}"
readonly EXTERNAL_HOSTNAME="${EXTERNAL_HOSTNAME:-rancher2-rnaidoo.ngrok.io}"  # Optional external hostname (e.g., ngrok URL)

# EVROC configuration
readonly EVROC_NAMESPACE="${EVROC_NAMESPACE:-evroc-capi-system}"
readonly EVROC_CREDENTIALS_PATH="${EVROC_CREDENTIALS_PATH:-${HOME}/.evroc/config.yaml}"

# Timeouts (in seconds)
readonly CERT_MANAGER_TIMEOUT=300
readonly INGRESS_NGINX_TIMEOUT=180
readonly RANCHER_TIMEOUT=600
readonly TURTLES_TIMEOUT=180
readonly EVROC_PROVIDER_TIMEOUT=300

# Options
DRY_RUN="${DRY_RUN:-false}"
SKIP_PREREQUISITES="${SKIP_PREREQUISITES:-false}"

#######################################
# Helper Functions
#######################################

# Print usage information
usage() {
    head -n 30 "$0" | grep "^#" | sed 's/^# \?//'
    exit 0
}

# Print component versions
show_versions() {
    cat <<EOF
Component Versions:
  Rancher Manager: v${RANCHER_VERSION}
  Rancher Turtles: v${TURTLES_VERSION}
  cert-manager:    v${CERT_MANAGER_VERSION}
  Kubernetes:      v${KUBERNETES_VERSION}
EOF
    exit 0
}

# Cleanup function - called on error or explicit cleanup
cleanup() {
    local exit_code=$?

    if [[ "${exit_code}" -ne 0 ]]; then
        log_error "Setup failed with exit code ${exit_code}"
        log_info "To clean up, run: $0 --cleanup"
    fi

    return "${exit_code}"
}

# Trap errors
trap cleanup EXIT

# Delete KIND cluster and clean up
cleanup_environment() {
    log_info "Cleaning up Rancher Turtles environment..."

    if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
        log_info "Deleting KIND cluster: ${KIND_CLUSTER_NAME}"
        kind delete cluster --name "${KIND_CLUSTER_NAME}"
        log_success "KIND cluster deleted"
    else
        log_info "KIND cluster ${KIND_CLUSTER_NAME} not found"
    fi

    log_success "Cleanup complete"
    exit 0
}

#######################################
# Installation Steps
#######################################

# Step 1: Check prerequisites
check_prerequisites() {
    log_info "Step 1/8: Checking prerequisites..."

    if [[ "${SKIP_PREREQUISITES}" == "true" ]]; then
        log_warn "Skipping prerequisite checks (not recommended)"
        return 0
    fi

    if ! check_all_prerequisites "${EVROC_CREDENTIALS_PATH}"; then
        log_error "Prerequisites check failed"
        exit 1
    fi

    log_success "Prerequisites check passed"
}

# Step 2: Create KIND cluster
create_kind_cluster() {
    log_info "Step 2/8: Creating KIND cluster..."

    # Check if cluster already exists
    if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
        log_warn "KIND cluster ${KIND_CLUSTER_NAME} already exists, skipping creation"
        kind export kubeconfig --name "${KIND_CLUSTER_NAME}"
        return 0
    fi

    local kind_config="${REPO_ROOT}/e2e/kind-cluster-config.yaml"

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would create KIND cluster with config: ${kind_config}"
        return 0
    fi

    log_info "Creating KIND cluster: ${KIND_CLUSTER_NAME}"
    kind create cluster --name "${KIND_CLUSTER_NAME}" --config "${kind_config}"

    log_info "Waiting for cluster to be ready..."
    sleep 5
    kubectl cluster-info

    log_success "KIND cluster created successfully"
}

# Step 3: Install cert-manager
install_cert_manager() {
    log_info "Step 3/8: Installing cert-manager v${CERT_MANAGER_VERSION}..."

    # Check if cert-manager is already installed
    if kubectl get namespace cert-manager >/dev/null 2>&1; then
        log_warn "cert-manager namespace already exists, skipping installation"
        return 0
    fi

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would install cert-manager v${CERT_MANAGER_VERSION}"
        return 0
    fi

    local cert_manager_url="https://github.com/cert-manager/cert-manager/releases/download/v${CERT_MANAGER_VERSION}/cert-manager.yaml"

    log_info "Applying cert-manager manifest from ${cert_manager_url}"
    kubectl apply -f "${cert_manager_url}"

    log_info "Waiting for cert-manager to be ready..."
    wait_for_namespace "cert-manager" 60
    wait_for_deployment_ready "cert-manager" "cert-manager" "${CERT_MANAGER_TIMEOUT}"
    wait_for_deployment_ready "cert-manager" "cert-manager-webhook" "${CERT_MANAGER_TIMEOUT}"
    wait_for_deployment_ready "cert-manager" "cert-manager-cainjector" "${CERT_MANAGER_TIMEOUT}"

    log_success "cert-manager installed successfully"
}

# Step 4: Install ingress-nginx controller
install_ingress_nginx() {
    log_info "Step 4/9: Installing ingress-nginx controller for KIND..."

    # Check if ingress-nginx is already installed
    if kubectl get namespace ingress-nginx >/dev/null 2>&1; then
        log_warn "ingress-nginx namespace already exists, skipping installation"
        return 0
    fi

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would install ingress-nginx for KIND"
        return 0
    fi

    local ingress_nginx_url="https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml"

    log_info "Applying ingress-nginx manifest from ${ingress_nginx_url}"
    kubectl apply -f "${ingress_nginx_url}"

    log_info "Waiting for ingress-nginx to be ready..."
    wait_for_namespace "ingress-nginx" 60
    kubectl wait --namespace ingress-nginx \
        --for=condition=ready pod \
        --selector=app.kubernetes.io/component=controller \
        --timeout="${INGRESS_NGINX_TIMEOUT}s"

    log_success "ingress-nginx installed successfully"
}

# Step 5: Install Rancher Manager
install_rancher() {
    log_info "Step 5/9: Installing Rancher Manager v${RANCHER_VERSION}..."

    # Check if Rancher is already installed
    if kubectl get namespace cattle-system >/dev/null 2>&1; then
        log_warn "cattle-system namespace already exists, skipping Rancher installation"
        return 0
    fi

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would install Rancher Manager v${RANCHER_VERSION}"
        return 0
    fi

    log_info "Adding Rancher Helm repository..."
    helm repo add rancher-stable https://releases.rancher.com/server-charts/stable 2>/dev/null || true
    helm repo update

    local values_file="${REPO_ROOT}/config/rancher/values-dev.yaml"

    log_info "Installing Rancher Manager..."
    helm install rancher rancher-stable/rancher \
        --namespace cattle-system \
        --create-namespace \
        --version "${RANCHER_VERSION}" \
        --set hostname="${RANCHER_HOSTNAME}" \
        --set bootstrapPassword="${RANCHER_BOOTSTRAP_PASSWORD}" \
        --set replicas=1 \
        --set ingress.ingressClassName=nginx \
        --wait \
        --timeout "${RANCHER_TIMEOUT}s"

    log_info "Waiting for Rancher deployments to be ready..."
    wait_for_deployment_ready "cattle-system" "rancher" "${RANCHER_TIMEOUT}"
    wait_for_deployment_ready "cattle-system" "rancher-webhook" "${RANCHER_TIMEOUT}"

    log_success "Rancher Manager installed successfully"
}

# Step 6: Patch Rancher ingress for external hostname
patch_rancher_ingress() {
    # Skip if no external hostname is set
    if [[ -z "${EXTERNAL_HOSTNAME}" ]]; then
        log_info "Step 6/9: Skipping ingress patch (no external hostname set)"
        return 0
    fi

    log_info "Step 6/9: Patching Rancher ingress for external hostname: ${EXTERNAL_HOSTNAME}..."

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would patch ingress to accept ${EXTERNAL_HOSTNAME}"
        return 0
    fi

    log_info "Adding ${EXTERNAL_HOSTNAME} to Rancher ingress..."

    # Add the external hostname to the ingress rules and TLS configuration
    kubectl patch ingress rancher -n cattle-system --type='json' -p="[
      {
        \"op\": \"add\",
        \"path\": \"/spec/rules/-\",
        \"value\": {
          \"host\": \"${EXTERNAL_HOSTNAME}\",
          \"http\": {
            \"paths\": [
              {
                \"backend\": {
                  \"service\": {
                    \"name\": \"rancher\",
                    \"port\": {
                      \"number\": 80
                    }
                  }
                },
                \"path\": \"/\",
                \"pathType\": \"ImplementationSpecific\"
              }
            ]
          }
        }
      },
      {
        \"op\": \"add\",
        \"path\": \"/spec/tls/0/hosts/-\",
        \"value\": \"${EXTERNAL_HOSTNAME}\"
      }
    ]"

    log_success "Ingress patched to accept both ${RANCHER_HOSTNAME} and ${EXTERNAL_HOSTNAME}"
}

# Step 7: Install Rancher Turtles
install_turtles() {
    log_info "Step 7/9: Installing Rancher Turtles v${TURTLES_VERSION}..."

    # Check if Turtles is already installed
    if kubectl get namespace rancher-turtles-system >/dev/null 2>&1; then
        log_warn "rancher-turtles-system namespace already exists, skipping Turtles installation"
        return 0
    fi

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would install Rancher Turtles v${TURTLES_VERSION}"
        return 0
    fi

    log_info "Adding Turtles Helm repository..."
    helm repo add turtles https://rancher.github.io/turtles 2>/dev/null || true
    helm repo update

    log_info "Installing Rancher Turtles..."
    helm install rancher-turtles turtles/rancher-turtles \
        --version "${TURTLES_VERSION}" \
        -n rancher-turtles-system \
        --create-namespace \
        --dependency-update \
        --wait \
        --timeout "${TURTLES_TIMEOUT}s"

    log_info "Waiting for Turtles components to be ready..."
    wait_for_deployment_ready "rancher-turtles-system" "rancher-turtles-controller-manager" "${TURTLES_TIMEOUT}"

    # Wait for CAPI components
    log_info "Waiting for CAPI core components..."
    sleep 10  # Give CAPI operator time to create resources
    wait_for_namespace "capi-system" 60
    wait_for_deployment_ready "capi-system" "capi-controller-manager" "${TURTLES_TIMEOUT}"

    log_success "Rancher Turtles installed successfully"
}

# Step 8: Register EVROC provider
register_evroc_provider() {
    log_info "Step 8/9: Registering EVROC CAPI provider..."

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would register EVROC provider in namespace ${EVROC_NAMESPACE}"
        return 0
    fi

    # Create namespace
    if ! kubectl get namespace "${EVROC_NAMESPACE}" >/dev/null 2>&1; then
        log_info "Creating namespace: ${EVROC_NAMESPACE}"
        kubectl create namespace "${EVROC_NAMESPACE}"
    fi

    # Create credentials secret
    log_info "Creating EVROC credentials secret..."
    if kubectl get secret evroc-credentials -n "${EVROC_NAMESPACE}" >/dev/null 2>&1; then
        log_warn "Credentials secret already exists, skipping creation"
    else
        kubectl create secret generic evroc-credentials \
            --from-file=kubeconfig="${EVROC_CREDENTIALS_PATH}" \
            -n "${EVROC_NAMESPACE}"
        log_success "Credentials secret created"
    fi

    # Apply CAPIProvider resource
    local capiprovider_file="${REPO_ROOT}/specs/002-rancher-turtles-integration/contracts/capiprovider-evroc.yaml"

    if [[ -f "${capiprovider_file}" ]]; then
        log_info "Applying CAPIProvider resource..."
        kubectl apply -f "${capiprovider_file}"

        log_info "Waiting for EVROC provider to be ready..."
        sleep 10
        wait_for_namespace "${EVROC_NAMESPACE}" 60

        # Note: The actual deployment name will depend on the CAPIProvider resource
        # Usually it's in the format: cape-<provider>-controller-manager
        log_info "Checking EVROC provider deployment..."
        local timeout=60
        local interval=5
        local elapsed=0

        while [[ ${elapsed} -lt ${timeout} ]]; do
            if kubectl get deployment -n "${EVROC_NAMESPACE}" | grep -q "controller-manager"; then
                log_success "EVROC provider deployment found"
                break
            fi
            log_debug "Waiting for EVROC provider deployment... (${elapsed}/${timeout}s)"
            sleep ${interval}
            elapsed=$((elapsed + interval))
        done

        log_success "EVROC provider registered successfully"
    else
        log_error "CAPIProvider file not found: ${capiprovider_file}"
        return 1
    fi
}

# Step 9: Verify installation and print output
verify_and_output() {
    log_info "Step 9/9: Verifying installation..."

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY RUN] Would verify all components"
        show_completion_message
        return 0
    fi

    log_info "Checking component status..."

    local all_ok=true

    # Check Rancher
    if kubectl get deployment rancher -n cattle-system >/dev/null 2>&1; then
        log_success "Rancher Manager: OK"
    else
        log_error "Rancher Manager: NOT FOUND"
        all_ok=false
    fi

    # Check Turtles
    if kubectl get deployment rancher-turtles-controller-manager -n rancher-turtles-system >/dev/null 2>&1; then
        log_success "Rancher Turtles: OK"
    else
        log_error "Rancher Turtles: NOT FOUND"
        all_ok=false
    fi

    # Check CAPI
    if kubectl get deployment capi-controller-manager -n capi-system >/dev/null 2>&1; then
        log_success "Cluster API: OK"
    else
        log_error "Cluster API: NOT FOUND"
        all_ok=false
    fi

    # Check EVROC provider
    # if kubectl get namespace "${EVROC_NAMESPACE}" >/dev/null 2>&1; then
    #     log_success "EVROC Provider: OK"
    # else
    #     log_error "EVROC Provider: NOT FOUND"
    #     all_ok=false
    # fi

    echo ""

    if [[ "${all_ok}" == "true" ]]; then
        log_success "All components installed successfully!"
        show_completion_message
        return 0
    else
        log_error "Some components are missing or failed to install"
        return 1
    fi
}

# Show completion message with next steps
show_completion_message() {
    cat <<EOF

╔═══════════════════════════════════════════════════════════════════════════════╗
║                                                                               ║
║  Rancher Manager + Turtles + EVROC Provider Setup Complete!                  ║
║                                                                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝

📋 Access Information:

  Rancher UI:  https://${RANCHER_HOSTNAME}
  Username:    admin
  Password:    ${RANCHER_BOOTSTRAP_PASSWORD}

  Note: Accept the self-signed certificate warning in your browser

🔍 Verification Commands:

  # Check all deployments
  kubectl get deployments -A | grep -E "(rancher|turtles|capi|evroc)"

  # Check CAPI providers
  kubectl get capiproviders -A

  # Check EVROC resources
  kubectl get evrocclusters,evrocmachines -A

📚 Next Steps:

  1. Open https://${RANCHER_HOSTNAME} in your browser
  2. Login with the credentials above
  3. Set the server URL to https://${RANCHER_HOSTNAME}
  4. (Optional) Install CAPI UI Extension from Rancher Extensions page
  5. Navigate to Cluster Management → CAPI to see providers
  6. Create your first EVROC cluster!

📖 Documentation:

  - Quickstart: ${REPO_ROOT}/specs/002-rancher-turtles-integration/quickstart.md
  - Full docs:  ${REPO_ROOT}/docs/rancher-integration.md

🧹 Cleanup:

  To remove everything: $0 --cleanup

EOF
}

#######################################
# Main Function
#######################################

main() {
    local start_time="${SECONDS}"

    log_info "=== Rancher Turtles Setup Script ==="
    log_info "This will install Rancher Manager, Rancher Turtles, and EVROC CAPI Provider"
    log_info "Estimated time: 10-15 minutes"
    echo ""

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)
                usage
                ;;
            -v|--version)
                show_versions
                ;;
            --dry-run)
                DRY_RUN=true
                log_info "Running in DRY RUN mode - no changes will be made"
                ;;
            --cleanup)
                cleanup_environment
                ;;
            --skip-prerequisites)
                SKIP_PREREQUISITES=true
                ;;
            --evroc-credentials)
                shift
                EVROC_CREDENTIALS_PATH="$1"
                ;;
            --external-hostname)
                shift
                EXTERNAL_HOSTNAME="$1"
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                ;;
        esac
        shift
    done

    # Execute installation steps
    check_prerequisites
    create_kind_cluster
    install_cert_manager
    install_ingress_nginx
    install_rancher
    patch_rancher_ingress
    install_turtles
    # register_evroc_provider
    verify_and_output

    local elapsed=$((SECONDS - start_time))
    log_success "Setup completed in ${elapsed} seconds!"

    # Disable error trap on successful completion
    trap - EXIT
    return 0
}

# Run main function
main "$@"
