package client

import (
	"context"
	"errors"
	"net"
	"net/url"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// IsTransient reports whether an error represents a transient condition that
// may succeed on retry. Cloud operators use this to decide whether to retry a
// failed operation or report it as a permanent failure.
//
// Transient errors include:
// - Network timeouts and context deadlines exceeded
// - Server-side temporary errors (503, 429)
// - Connection refused / unreachable
// - Client context cancellations are NOT transient
func IsTransient(err error) bool {
	if err == nil {
		return false
	}

	switch {
	// Kubernetes API server temporary states
	case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return true
	case apierrors.IsTooManyRequests(err):
		return true
	case apierrors.IsServiceUnavailable(err), apierrors.IsInternalError(err):
		return true

	// Context deadline exceeded is transient (can retry after timeout resets)
	case errors.Is(err, context.DeadlineExceeded):
		return true

	// Context cancellation is intentional, not transient
	case errors.Is(err, context.Canceled):
		return false

	// Network-level transient errors
	case isNetworkTransient(err):
		return true

	// Connection-level transient errors
	case isConnectionTransient(err):
		return true
	}

	return false
}

// isNetworkTransient detects network-level errors that are typically transient.
func isNetworkTransient(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		switch opErr.Op {
		case "dial", "read", "write":
			// Connection issues are often transient; permanent errors
			// (like "no such host") are distinguished by Temporary().
			if opErr.Temporary() {
				return true
			}
		}
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Check the underlying error
		return IsTransient(urlErr.Err)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		// DNS server errors and timeouts are transient; "no such host"
		// (IsNotFound) is permanent.
		return !dnsErr.IsNotFound && (dnsErr.Timeout() || dnsErr.Temporary())
	}

	return false
}

// isConnectionTransient detects connection-level errors that indicate a
// transient condition (as opposed to misconfiguration).
func isConnectionTransient(err error) bool {
	// Temporary connection states
	switch {
	case errors.Is(err, net.ErrClosed):
		return true
	}

	return false
}

// IsAuthentication reports whether err represents an auth failure that won't
// be resolved by retrying (e.g., invalid token, missing credentials).
func IsAuthentication(err error) bool {
	return apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err)
}

// IsNotFound reports whether err represents a resource that does not exist.
// This is not transient: retrying will not create the resource.
func IsNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

// IsInvalid reports whether err represents invalid input or configuration,
// typically a 400 or 422 response. Retrying the same operation will fail again.
func IsInvalid(err error) bool {
	if apierrors.IsBadRequest(err) {
		return true
	}
	if apierrors.IsInvalid(err) {
		return true
	}
	// 422 from JSON patch test failure
	if apierrors.IsConflict(err) {
		return true
	}
	return false
}

// RetryBackoff computes an exponential backoff duration for retry attempt n
// (0-indexed). Used by callers to space out retry attempts. Cloud operators
// typically need backoff to avoid overwhelming a struggling API server.
func RetryBackoff(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	if n > 5 {
		n = 5
	}
	// 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s
	return time.Duration(100*(1<<uint(n))) * time.Millisecond
}
