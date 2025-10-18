#!/usr/bin/env bash

# logging.sh - Color-coded logging utilities
# Usage: Source this file and call log_info, log_warn, log_error, log_success

# Prevent double-sourcing
if [[ -n "${_LOGGING_SH_LOADED:-}" ]]; then
    return 0
fi
readonly _LOGGING_SH_LOADED=1

set -euo pipefail

# Color codes
readonly COLOR_RED='\033[0;31m'
readonly COLOR_GREEN='\033[0;32m'
readonly COLOR_YELLOW='\033[1;33m'
readonly COLOR_BLUE='\033[0;34m'
readonly COLOR_NC='\033[0m' # No Color

# Check if QUIET mode is enabled
is_quiet() {
    [[ "${QUIET:-false}" == "true" ]]
}

# Get timestamp
get_timestamp() {
    date '+%Y-%m-%d %H:%M:%S'
}

# Log info message
log_info() {
    if ! is_quiet; then
        echo -e "${COLOR_BLUE}[$(get_timestamp)] INFO:${COLOR_NC} $*" >&2
    fi
}

# Log warning message
log_warn() {
    if ! is_quiet; then
        echo -e "${COLOR_YELLOW}[$(get_timestamp)] WARN:${COLOR_NC} $*" >&2
    fi
}

# Log error message
log_error() {
    echo -e "${COLOR_RED}[$(get_timestamp)] ERROR:${COLOR_NC} $*" >&2
}

# Log success message
log_success() {
    if ! is_quiet; then
        echo -e "${COLOR_GREEN}[$(get_timestamp)] SUCCESS:${COLOR_NC} $*" >&2
    fi
}

# Log debug message (only if DEBUG=true)
log_debug() {
    if [[ "${DEBUG:-false}" == "true" ]] && ! is_quiet; then
        echo -e "[$(get_timestamp)] DEBUG: $*" >&2
    fi
}
