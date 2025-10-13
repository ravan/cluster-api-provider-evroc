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

package evroc

import (
	"fmt"
	"net"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Error classification for proper retry behavior
const (
	// TransientRetryDelay is the delay for retrying transient errors
	TransientRetryDelay = 30 * time.Second

	// BootstrapDataRetryDelay is the delay for waiting on bootstrap data
	BootstrapDataRetryDelay = 5 * time.Second
)

// IsTransientError checks if an error is transient and should be retried
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors are transient
	if _, ok := err.(net.Error); ok {
		return true
	}

	// Timeout errors are transient
	if strings.Contains(err.Error(), "timeout") {
		return true
	}

	// Connection errors are transient
	if strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connection reset") {
		return true
	}

	// Rate limit errors are transient
	if strings.Contains(err.Error(), "rate limit") ||
		strings.Contains(err.Error(), "too many requests") {
		return true
	}

	// Kubernetes API errors that are transient
	if apierrors.IsServiceUnavailable(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) {
		return true
	}

	// Temporary unavailability
	if strings.Contains(err.Error(), "temporarily unavailable") {
		return true
	}

	return false
}

// IsTerminalError checks if an error is terminal and should not be retried
func IsTerminalError(err error) bool {
	if err == nil {
		return false
	}

	// Validation errors are terminal
	if apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) {
		return true
	}

	// Permission errors are terminal
	if apierrors.IsForbidden(err) ||
		apierrors.IsUnauthorized(err) {
		return true
	}

	// Not found can be terminal in certain contexts
	if apierrors.IsNotFound(err) {
		return true
	}

	// Conflict errors are terminal (resource version mismatch)
	if apierrors.IsConflict(err) {
		return true
	}

	return false
}

// IsNotFoundError checks if an error is a not found error
func IsNotFoundError(err error) bool {
	return apierrors.IsNotFound(err)
}

// HandleError classifies an error and returns appropriate result and error
func HandleError(err error, errMsg string) (ctrl.Result, error) {
	if err == nil {
		return ctrl.Result{}, nil
	}

	// Transient errors should be retried
	if IsTransientError(err) {
		return ctrl.Result{RequeueAfter: TransientRetryDelay}, nil
	}

	// Terminal errors should fail fast
	if IsTerminalError(err) {
		return ctrl.Result{}, fmt.Errorf("%s: %w", errMsg, err)
	}

	// Default: return error and let controller-runtime handle retry
	return ctrl.Result{}, fmt.Errorf("%s: %w", errMsg, err)
}
