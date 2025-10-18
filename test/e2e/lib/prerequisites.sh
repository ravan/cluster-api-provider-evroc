#!/usr/bin/env bash

# prerequisites.sh - Prerequisites validation library
# Usage: Source this file and call validation functions

set -euo pipefail

# Source logging utilities
_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./logging.sh
source "${_LIB_DIR}/logging.sh"

# Version comparison helper
# Returns 0 if version1 >= version2
version_gte() {
    local version1="$1"
    local version2="$2"

    # Remove 'v' prefix if present
    version1="${version1#v}"
    version2="${version2#v}"

    # Use sort -V for version comparison
    # If version2 sorts first or equal, version1 >= version2
    if printf '%s\n%s\n' "${version2}" "${version1}" | sort -V -C; then
        return 0
    else
        return 1
    fi
}

# Check if command exists and meets minimum version
# Args: command_name, min_version (optional)
check_command_exists() {
    local command_name="${1:?command name required}"
    local min_version="${2:-}"

    if ! command -v "${command_name}" >/dev/null 2>&1; then
        log_error "${command_name} is not installed"

        case "${command_name}" in
            kubectl)
                echo "Install kubectl: https://kubernetes.io/docs/tasks/tools/install-kubectl/" >&2
                ;;
            kind)
                echo "Install kind: https://kind.sigs.k8s.io/docs/user/quick-start/#installation" >&2
                ;;
            helm)
                echo "Install helm: https://helm.sh/docs/intro/install/" >&2
                ;;
            docker)
                echo "Install docker: https://docs.docker.com/engine/install/" >&2
                ;;
            *)
                echo "Install ${command_name} and ensure it is in your PATH" >&2
                ;;
        esac

        return 1
    fi

    log_success "${command_name} is installed"

    # Check version if min_version provided
    if [[ -n "${min_version}" ]]; then
        local current_version

        case "${command_name}" in
            kubectl)
                current_version=$(kubectl version --client -o json 2>/dev/null | grep -o '"gitVersion":"v[^"]*"' | cut -d'"' -f4 || echo "unknown")
                ;;
            kind)
                current_version=$(kind version 2>/dev/null | grep -o 'v[0-9.]*' | head -1 || echo "unknown")
                ;;
            helm)
                current_version=$(helm version --short 2>/dev/null | grep -o 'v[0-9.]*' | head -1 || echo "unknown")
                ;;
            docker)
                current_version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
                ;;
            *)
                current_version="unknown"
                ;;
        esac

        if [[ "${current_version}" == "unknown" ]]; then
            log_warn "Could not determine ${command_name} version"
            return 0
        fi

        if ! version_gte "${current_version}" "${min_version}"; then
            log_error "${command_name} version ${current_version} is below minimum ${min_version}"
            echo "Please upgrade ${command_name} to version ${min_version} or later" >&2
            return 1
        fi

        log_info "${command_name} version ${current_version} meets minimum ${min_version}"
    fi

    return 0
}

# Check if Docker daemon is running
check_docker_running() {
    log_info "Checking if Docker daemon is running..."

    if ! docker info >/dev/null 2>&1; then
        log_error "Docker daemon is not running"
        echo "Please start Docker and try again" >&2
        echo "On macOS: Open Docker Desktop application" >&2
        echo "On Linux: sudo systemctl start docker" >&2
        return 1
    fi

    log_success "Docker daemon is running"
    return 0
}

# Check system resources
# Args: min_ram_gb, min_cpu_cores, min_disk_gb
check_system_resources() {
    local min_ram_gb="${1:-8}"
    local min_cpu_cores="${2:-4}"
    local min_disk_gb="${3:-20}"

    log_info "Checking system resources..."

    # Get OS type
    local os_type
    os_type=$(uname -s)

    # Check RAM
    local ram_gb=0
    case "${os_type}" in
        Darwin)
            ram_gb=$(sysctl -n hw.memsize 2>/dev/null | awk '{print int($1/1024/1024/1024)}' || echo "0")
            ;;
        Linux)
            ram_gb=$(free -g 2>/dev/null | awk '/^Mem:/{print $2}' || echo "0")
            ;;
        *)
            log_warn "Cannot determine RAM on ${os_type}, skipping RAM check"
            ;;
    esac

    if [[ "${ram_gb}" -gt 0 ]]; then
        if [[ "${ram_gb}" -lt "${min_ram_gb}" ]]; then
            log_error "Insufficient RAM: ${ram_gb}GB available, ${min_ram_gb}GB required"
            return 1
        fi
        log_success "RAM: ${ram_gb}GB (meets minimum ${min_ram_gb}GB)"
    fi

    # Check CPU cores
    local cpu_cores=0
    case "${os_type}" in
        Darwin)
            cpu_cores=$(sysctl -n hw.ncpu 2>/dev/null || echo "0")
            ;;
        Linux)
            cpu_cores=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo "0")
            ;;
        *)
            log_warn "Cannot determine CPU cores on ${os_type}, skipping CPU check"
            ;;
    esac

    if [[ "${cpu_cores}" -gt 0 ]]; then
        if [[ "${cpu_cores}" -lt "${min_cpu_cores}" ]]; then
            log_error "Insufficient CPU cores: ${cpu_cores} available, ${min_cpu_cores} required"
            return 1
        fi
        log_success "CPU: ${cpu_cores} cores (meets minimum ${min_cpu_cores})"
    fi

    # Check disk space
    local disk_gb=0
    disk_gb=$(df -Pk . 2>/dev/null | awk 'NR==2 {print int($4/1024/1024)}' || echo "0")

    if [[ "${disk_gb}" -gt 0 ]]; then
        if [[ "${disk_gb}" -lt "${min_disk_gb}" ]]; then
            log_error "Insufficient disk space: ${disk_gb}GB available, ${min_disk_gb}GB required"
            return 1
        fi
        log_success "Disk: ${disk_gb}GB available (meets minimum ${min_disk_gb}GB)"
    else
        log_warn "Cannot determine disk space, skipping disk check"
    fi

    return 0
}

# Validate EVROC credentials
# Args: credentials_path
validate_evroc_credentials() {
    local credentials_path="${1:?credentials path required}"

    log_info "Validating EVROC credentials at ${credentials_path}..."

    if [[ ! -f "${credentials_path}" ]]; then
        log_error "EVROC credentials file not found: ${credentials_path}"
        echo "Please ensure your EVROC kubeconfig exists at ${credentials_path}" >&2
        echo "Contact your EVROC administrator for credentials" >&2
        return 1
    fi

    # Basic validation - check if it's a valid kubeconfig
    if ! grep -q "kind: Config" "${credentials_path}" 2>/dev/null; then
        log_error "File at ${credentials_path} does not appear to be a valid kubeconfig"
        return 1
    fi

    log_success "EVROC credentials file exists and appears valid"
    return 0
}

# Run all prerequisite checks
# Args: credentials_path (optional, defaults to ~/.evroc/config.yaml)
check_all_prerequisites() {
    local credentials_path="${1:-${HOME}/.evroc/config.yaml}"
    local all_passed=true

    log_info "Running all prerequisite checks..."
    echo ""

    # Check commands
    check_command_exists "kubectl" "1.29.0" || all_passed=false
    check_command_exists "kind" "0.20.0" || all_passed=false
    check_command_exists "helm" "3.17.0" || all_passed=false
    check_command_exists "docker" || all_passed=false

    echo ""

    # Check Docker is running
    check_docker_running || all_passed=false

    echo ""

    # Check system resources
    check_system_resources 8 4 20 || all_passed=false

    echo ""

    # Check EVROC credentials
    validate_evroc_credentials "${credentials_path}" || all_passed=false

    echo ""

    if [[ "${all_passed}" == "true" ]]; then
        log_success "All prerequisite checks passed!"
        return 0
    else
        log_error "Some prerequisite checks failed"
        return 1
    fi
}
