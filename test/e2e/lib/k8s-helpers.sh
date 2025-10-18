#!/usr/bin/env bash

# k8s-helpers.sh - Kubernetes utility functions
# Usage: Source this file and call wait_for_deployment_ready, wait_for_pod_ready, check_resource_exists

set -euo pipefail

# Source logging utilities
_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./logging.sh
source "${_LIB_DIR}/logging.sh"

# Wait for deployment to be ready
# Args: namespace, deployment_name, timeout_seconds
wait_for_deployment_ready() {
    local namespace="${1:?namespace required}"
    local deployment="${2:?deployment name required}"
    local timeout="${3:-300}"

    log_info "Waiting for deployment ${namespace}/${deployment} to be ready (timeout: ${timeout}s)..."

    local end_time=$((SECONDS + timeout))
    local retry_interval=5

    while [[ ${SECONDS} -lt ${end_time} ]]; do
        if kubectl rollout status deployment/"${deployment}" -n "${namespace}" --timeout="${retry_interval}s" >/dev/null 2>&1; then
            log_success "Deployment ${namespace}/${deployment} is ready"
            return 0
        fi

        log_debug "Deployment ${namespace}/${deployment} not ready yet, retrying in ${retry_interval}s..."
        sleep "${retry_interval}"
    done

    log_error "Timeout waiting for deployment ${namespace}/${deployment} to be ready"
    return 1
}

# Wait for pod to be ready
# Args: namespace, label_selector, timeout_seconds
wait_for_pod_ready() {
    local namespace="${1:?namespace required}"
    local selector="${2:?label selector required}"
    local timeout="${3:-300}"

    log_info "Waiting for pods matching '${selector}' in namespace ${namespace} to be ready (timeout: ${timeout}s)..."

    local end_time=$((SECONDS + timeout))
    local retry_interval=5

    while [[ ${SECONDS} -lt ${end_time} ]]; do
        local ready_count
        ready_count=$(kubectl get pods -n "${namespace}" -l "${selector}" -o jsonpath='{.items[*].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null | tr ' ' '\n' | grep -c "True" || echo "0")
        local total_count
        total_count=$(kubectl get pods -n "${namespace}" -l "${selector}" --no-headers 2>/dev/null | wc -l | tr -d '[:space:]')

        if [[ "${ready_count}" -gt 0 ]] && [[ "${ready_count}" -eq "${total_count}" ]]; then
            log_success "All ${ready_count} pod(s) matching '${selector}' in namespace ${namespace} are ready"
            return 0
        fi

        log_debug "Pods matching '${selector}' in ${namespace}: ${ready_count}/${total_count} ready, retrying in ${retry_interval}s..."
        sleep "${retry_interval}"
    done

    log_error "Timeout waiting for pods matching '${selector}' in namespace ${namespace} to be ready"
    return 1
}

# Check if resource exists
# Args: resource_type, name, namespace (optional)
check_resource_exists() {
    local resource_type="${1:?resource type required}"
    local name="${2:?resource name required}"
    local namespace="${3:-}"

    local ns_flag=""
    if [[ -n "${namespace}" ]]; then
        ns_flag="-n ${namespace}"
    fi

    if kubectl get "${resource_type}" "${name}" ${ns_flag} >/dev/null 2>&1; then
        log_debug "Resource ${resource_type}/${name} exists"
        return 0
    else
        log_debug "Resource ${resource_type}/${name} does not exist"
        return 1
    fi
}

# Wait for CRD to be established
# Args: crd_name, timeout_seconds
wait_for_crd() {
    local crd_name="${1:?CRD name required}"
    local timeout="${2:-120}"

    log_info "Waiting for CRD ${crd_name} to be established (timeout: ${timeout}s)..."

    local end_time=$((SECONDS + timeout))
    local retry_interval=5

    while [[ ${SECONDS} -lt ${end_time} ]]; do
        local established
        established=$(kubectl get crd "${crd_name}" -o jsonpath='{.status.conditions[?(@.type=="Established")].status}' 2>/dev/null || echo "")

        if [[ "${established}" == "True" ]]; then
            log_success "CRD ${crd_name} is established"
            return 0
        fi

        log_debug "CRD ${crd_name} not established yet, retrying in ${retry_interval}s..."
        sleep "${retry_interval}"
    done

    log_error "Timeout waiting for CRD ${crd_name} to be established"
    return 1
}

# Wait for namespace to exist
# Args: namespace, timeout_seconds
wait_for_namespace() {
    local namespace="${1:?namespace required}"
    local timeout="${2:-60}"

    log_info "Waiting for namespace ${namespace} to exist (timeout: ${timeout}s)..."

    local end_time=$((SECONDS + timeout))
    local retry_interval=2

    while [[ ${SECONDS} -lt ${end_time} ]]; do
        if kubectl get namespace "${namespace}" >/dev/null 2>&1; then
            log_success "Namespace ${namespace} exists"
            return 0
        fi

        log_debug "Namespace ${namespace} does not exist yet, retrying in ${retry_interval}s..."
        sleep "${retry_interval}"
    done

    log_error "Timeout waiting for namespace ${namespace} to exist"
    return 1
}
