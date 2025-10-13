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
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "network error",
			err:      &net.DNSError{IsTimeout: true},
			expected: true,
		},
		{
			name:     "timeout error",
			err:      errors.New("connection timeout"),
			expected: true,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "connection reset",
			err:      errors.New("connection reset by peer"),
			expected: true,
		},
		{
			name:     "rate limit error",
			err:      errors.New("rate limit exceeded"),
			expected: true,
		},
		{
			name:     "too many requests",
			err:      errors.New("too many requests"),
			expected: true,
		},
		{
			name:     "service unavailable",
			err:      apierrors.NewServiceUnavailable("service unavailable"),
			expected: true,
		},
		{
			name:     "timeout k8s error",
			err:      apierrors.NewTimeoutError("timeout", 1),
			expected: true,
		},
		{
			name:     "too many requests k8s",
			err:      apierrors.NewTooManyRequests("too many", 1),
			expected: true,
		},
		{
			name:     "temporarily unavailable",
			err:      errors.New("resource temporarily unavailable"),
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTransientError(tt.err)
			if result != tt.expected {
				t.Errorf("IsTransientError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsTerminalError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "invalid error",
			err:      apierrors.NewInvalid(schema.GroupKind{}, "test", nil),
			expected: true,
		},
		{
			name:     "bad request error",
			err:      apierrors.NewBadRequest("bad request"),
			expected: true,
		},
		{
			name:     "forbidden error",
			err:      apierrors.NewForbidden(schema.GroupResource{}, "test", errors.New("forbidden")),
			expected: true,
		},
		{
			name:     "unauthorized error",
			err:      apierrors.NewUnauthorized("unauthorized"),
			expected: true,
		},
		{
			name:     "not found error",
			err:      apierrors.NewNotFound(schema.GroupResource{}, "test"),
			expected: true,
		},
		{
			name:     "conflict error",
			err:      apierrors.NewConflict(schema.GroupResource{}, "test", errors.New("conflict")),
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTerminalError(tt.err)
			if result != tt.expected {
				t.Errorf("IsTerminalError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "not found error",
			err:      apierrors.NewNotFound(schema.GroupResource{}, "test"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNotFoundError(tt.err)
			if result != tt.expected {
				t.Errorf("IsNotFoundError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestHandleError(t *testing.T) {
	tests := []struct {
		name               string
		err                error
		errMsg             string
		expectRequeue      bool
		expectRequeueAfter time.Duration
		expectError        bool
	}{
		{
			name:               "nil error",
			err:                nil,
			errMsg:             "test",
			expectRequeue:      false,
			expectRequeueAfter: 0,
			expectError:        false,
		},
		{
			name:               "transient error",
			err:                errors.New("connection timeout"),
			errMsg:             "test",
			expectRequeue:      false,
			expectRequeueAfter: TransientRetryDelay,
			expectError:        false,
		},
		{
			name:               "terminal error",
			err:                apierrors.NewForbidden(schema.GroupResource{}, "test", errors.New("forbidden")),
			errMsg:             "test",
			expectRequeue:      false,
			expectRequeueAfter: 0,
			expectError:        true,
		},
		{
			name:               "generic error",
			err:                errors.New("some error"),
			errMsg:             "test",
			expectRequeue:      false,
			expectRequeueAfter: 0,
			expectError:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := HandleError(tt.err, tt.errMsg)

			if tt.expectError && err == nil {
				t.Errorf("HandleError() expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("HandleError() expected no error but got %v", err)
			}

			if result.Requeue != tt.expectRequeue {
				t.Errorf("HandleError() result.Requeue = %v, want %v", result.Requeue, tt.expectRequeue)
			}

			if result.RequeueAfter != tt.expectRequeueAfter {
				t.Errorf("HandleError() result.RequeueAfter = %v, want %v", result.RequeueAfter, tt.expectRequeueAfter)
			}
		})
	}
}

func TestErrorConstants(t *testing.T) {
	if TransientRetryDelay != 30*time.Second {
		t.Errorf("TransientRetryDelay = %v, want 30s", TransientRetryDelay)
	}

	if BootstrapDataRetryDelay != 5*time.Second {
		t.Errorf("BootstrapDataRetryDelay = %v, want 5s", BootstrapDataRetryDelay)
	}
}

func TestHandleErrorReturnsCorrectResult(t *testing.T) {
	// Test that transient errors return correct result
	err := errors.New("timeout")
	result, resultErr := HandleError(err, "operation failed")

	if resultErr != nil {
		t.Errorf("Expected no error for transient error, got %v", resultErr)
	}

	if result != (ctrl.Result{RequeueAfter: TransientRetryDelay}) {
		t.Errorf("Expected requeue after %v, got %v", TransientRetryDelay, result.RequeueAfter)
	}
}

func TestHandleErrorWrapsMessage(t *testing.T) {
	originalErr := errors.New("original error")
	customMsg := "custom message"

	_, err := HandleError(originalErr, customMsg)

	if err == nil {
		t.Fatal("Expected error to be returned")
	}

	errorString := err.Error()
	if errorString != fmt.Sprintf("%s: %v", customMsg, originalErr) {
		t.Errorf("Expected error message to contain both custom message and original error, got %s", errorString)
	}
}
