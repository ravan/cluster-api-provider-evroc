#!/usr/bin/env bash

# SSH Helper Script for Evroc CAPI VMs
# This script simplifies SSH connections to VMs created by Cluster API Provider Evroc

set -o errexit
set -o nounset
set -o pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
SSH_USER="${SSH_USER:-evroc-user}"
# Use EVROC_SSH_PRIVATE_KEY from .env if set, otherwise fall back to defaults
SSH_KEY="${SSH_KEY:-${EVROC_SSH_PRIVATE_KEY:-${HOME}/.ssh/id_rsa}}"
NAMESPACE="${NAMESPACE:-default}"

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

usage() {
    cat <<EOF
Usage: $0 [OPTIONS] <vm-name-or-machine-name>

Connect to an Evroc VM via SSH using Kubernetes to discover the IP address.

Arguments:
  vm-name-or-machine-name    Name of the EvrocMachine or VirtualMachine to connect to

Options:
  -k, --key PATH            Path to SSH private key (default: ~/.ssh/id_rsa)
  -u, --user USERNAME       SSH username (default: evroc-user)
  -n, --namespace NAMESPACE Kubernetes namespace (default: default)
  -p, --project PROJECT     Evroc project namespace (overrides namespace for VM lookup)
  -c, --command COMMAND     Execute command via SSH and exit
  -h, --help                Show this help message

Environment Variables:
  SSH_KEY                   Default SSH key path (overrides EVROC_SSH_PRIVATE_KEY)
  EVROC_SSH_PRIVATE_KEY     SSH private key path from .env (auto-detected)
  SSH_USER                  Default SSH username
  NAMESPACE                 Default Kubernetes namespace
  EVROC_PROJECT             Default Evroc project namespace

Note: Source test/e2e/.env to automatically configure SSH keys

Examples:
  # Connect to control plane machine
  $0 my-cluster-control-plane-abcd

  # Connect using specific SSH key
  $0 -k ~/.ssh/my-key my-cluster-worker-xyz

  # Execute command on VM
  $0 -c "sudo systemctl status kubelet" my-cluster-control-plane-abcd

  # Connect to VM in specific project
  $0 -p production my-cluster-control-plane-abcd

EOF
}

get_machine_ip() {
    local machine_name="$1"
    local namespace="$2"

    log_info "Looking up IP address for EvrocMachine: ${machine_name}"

    # Try to get the machine from Kubernetes
    local machine_json
    if ! machine_json=$(kubectl get evrocmachine "${machine_name}" -n "${namespace}" -o json 2>/dev/null); then
        log_error "EvrocMachine '${machine_name}' not found in namespace '${namespace}'"
        log_info "Available EvrocMachines:"
        kubectl get evrocmachines -n "${namespace}" 2>/dev/null || echo "  (none found)"
        return 1
    fi

    # Extract public IP from status.addresses
    local public_ip
    public_ip=$(echo "${machine_json}" | jq -r '.status.addresses[] | select(.type=="ExternalIP") | .address' | head -n1)

    if [ -z "${public_ip}" ] || [ "${public_ip}" == "null" ]; then
        log_error "No public IP found for EvrocMachine '${machine_name}'"
        log_info "Machine status:"
        echo "${machine_json}" | jq '.status'
        return 1
    fi

    echo "${public_ip}"
}

get_vm_ip() {
    local vm_name="$1"
    local project="$2"

    log_info "Looking up IP address for VirtualMachine: ${vm_name} in project: ${project}"

    # Try to get the VM from Kubernetes (in the Evroc project namespace)
    local vm_json
    if ! vm_json=$(kubectl get virtualmachine "${vm_name}" -n "${project}" -o json 2>/dev/null); then
        log_error "VirtualMachine '${vm_name}' not found in project '${project}'"
        log_info "Available VirtualMachines:"
        kubectl get virtualmachines -n "${project}" 2>/dev/null || echo "  (none found)"
        return 1
    fi

    # Extract public IP from status
    local public_ip
    public_ip=$(echo "${vm_json}" | jq -r '.status.networking.publicIPv4Address' 2>/dev/null)

    if [ -z "${public_ip}" ] || [ "${public_ip}" == "null" ]; then
        log_warn "No public IP found for VirtualMachine '${vm_name}', trying private IP"
        public_ip=$(echo "${vm_json}" | jq -r '.status.networking.privateIPv4Address' 2>/dev/null)
    fi

    if [ -z "${public_ip}" ] || [ "${public_ip}" == "null" ]; then
        log_error "No IP address found for VirtualMachine '${vm_name}'"
        log_info "VM status:"
        echo "${vm_json}" | jq '.status'
        return 1
    fi

    echo "${public_ip}"
}

ssh_to_vm() {
    local name="$1"
    local namespace="$2"
    local project="${3:-${namespace}}"
    local ssh_key="$4"
    local ssh_user="$5"
    local ssh_command="${6:-}"

    # Check if SSH key exists
    if [ ! -f "${ssh_key}" ]; then
        log_error "SSH key not found at: ${ssh_key}"
        log_info "Please specify a valid SSH key with -k option or set SSH_KEY environment variable"
        return 1
    fi

    # Try to get IP from EvrocMachine first
    local ip_address
    if ip_address=$(get_machine_ip "${name}" "${namespace}" 2>/dev/null); then
        log_info "Found IP address: ${ip_address}"
    else
        # If that fails, try VirtualMachine in the project namespace
        log_warn "Not found as EvrocMachine, trying VirtualMachine in project '${project}'"
        if ! ip_address=$(get_vm_ip "${name}" "${project}"); then
            log_error "Failed to find VM or machine: ${name}"
            return 1
        fi
        log_info "Found IP address: ${ip_address}"
    fi

    # Connect via SSH
    if [ -n "${ssh_command}" ]; then
        log_info "Executing command on ${ssh_user}@${ip_address}: ${ssh_command}"
        ssh -i "${ssh_key}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
            "${ssh_user}@${ip_address}" "${ssh_command}"
    else
        log_info "Connecting to ${ssh_user}@${ip_address}"
        log_info "Press Ctrl+D or type 'exit' to disconnect"
        echo ""
        ssh -i "${ssh_key}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
            "${ssh_user}@${ip_address}"
    fi
}

# Main script
main() {
    local name=""
    local project="${EVROC_PROJECT:-}"
    local ssh_command=""

    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -k|--key)
                SSH_KEY="$2"
                shift 2
                ;;
            -u|--user)
                SSH_USER="$2"
                shift 2
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -p|--project)
                project="$2"
                shift 2
                ;;
            -c|--command)
                ssh_command="$2"
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            -*)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
            *)
                name="$1"
                shift
                ;;
        esac
    done

    # Validate arguments
    if [ -z "${name}" ]; then
        log_error "VM or machine name is required"
        echo ""
        usage
        exit 1
    fi

    # Use namespace as project if project is not specified
    if [ -z "${project}" ]; then
        project="${NAMESPACE}"
    fi

    # Check if kubectl is available
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi

    # Check if jq is available
    if ! command -v jq &> /dev/null; then
        log_error "jq is not installed or not in PATH"
        log_info "Install jq: brew install jq (macOS) or apt-get install jq (Linux)"
        exit 1
    fi

    # Connect to the VM
    ssh_to_vm "${name}" "${NAMESPACE}" "${project}" "${SSH_KEY}" "${SSH_USER}" "${ssh_command}"
}

# Run main function
main "$@"
