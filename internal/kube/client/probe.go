package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/version"
)

// ConnState is the outcome of a connectivity probe.
type ConnState int

const (
	// ConnUnknown means no probe has completed yet.
	ConnUnknown ConnState = iota
	// ConnOK means the API server answered.
	ConnOK
	// ConnUnreachable means the network could not deliver the request.
	ConnUnreachable
	// ConnUnauthorized means we reached the server but are not authenticated.
	ConnUnauthorized
	// ConnForbidden means we are authenticated but lack permission.
	ConnForbidden
	// ConnTimeout means the request exceeded its deadline.
	ConnTimeout
	// ConnTLSError means the server's certificate could not be verified.
	ConnTLSError
	// ConnConfigError means the kubeconfig entry itself is unusable.
	ConnConfigError
)

// String renders the state as a short label.
func (s ConnState) String() string {
	switch s {
	case ConnOK:
		return "connected"
	case ConnUnreachable:
		return "unreachable"
	case ConnUnauthorized:
		return "unauthorized"
	case ConnForbidden:
		return "forbidden"
	case ConnTimeout:
		return "timeout"
	case ConnTLSError:
		return "TLS error"
	case ConnConfigError:
		return "config error"
	default:
		return "unknown"
	}
}

// ClusterInfo is what a successful probe learned about a cluster.
type ClusterInfo struct {
	State ConnState
	// ServerVersion is the reported Kubernetes version ("" when unknown).
	ServerVersion string
	// Server is the API server URL the probe used.
	Server string
	// Latency is the round-trip time of the probe.
	Latency time.Duration
	// Err is the underlying failure, if any.
	Err error
	// Hint is a short, actionable next step for the user.
	Hint string
}

// Probe checks whether the API server for a context is reachable and returns
// its version. It is the cheapest possible call (/version needs no permissions)
// so it works even for tightly scoped service accounts.
func (f *Factory) Probe(ctx context.Context, contextName string) ClusterInfo {
	info := ClusterInfo{}
	restCfg, err := f.RESTConfig(contextName)
	if restCfg != nil {
		info.Server = restCfg.Host
	}
	if err != nil {
		info.State = ConnConfigError
		info.Err = err
		info.Hint = "Check the kubeconfig entry for this context."
		return info
	}
	cs, err := f.Clientset(contextName)
	if err != nil {
		info.State = ConnConfigError
		info.Err = err
		info.Hint = "Check the kubeconfig entry for this context."
		return info
	}

	start := time.Now()
	body, err := cs.Discovery().RESTClient().Get().AbsPath("/version").DoRaw(ctx)
	info.Latency = time.Since(start)
	if err != nil {
		info.State, info.Hint = classify(err)
		info.Err = err
		return info
	}

	var v version.Info
	if jsonErr := json.Unmarshal(body, &v); jsonErr == nil {
		info.ServerVersion = v.GitVersion
	}
	info.State = ConnOK
	return info
}

// classify turns a client-go error into a state plus a hint that tells the user
// what to do next. Kubernetes surfaces these as long wrapped strings; the point
// of this function is that kubeui never shows one raw.
func classify(err error) (ConnState, string) {
	switch {
	case err == nil:
		return ConnOK, ""
	case errors.Is(err, context.DeadlineExceeded), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return ConnTimeout, "The API server did not answer in time. Are you on the right network or VPN?"
	case errors.Is(err, context.Canceled):
		return ConnUnknown, ""
	case apierrors.IsUnauthorized(err):
		return ConnUnauthorized, "Credentials were rejected. Refresh your login for this context."
	case apierrors.IsForbidden(err):
		return ConnForbidden, "Authenticated, but this user may not perform the request."
	}

	msg := err.Error()
	switch {
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "x509"), strings.Contains(msg, "tls:"):
		return ConnTLSError, "The API server certificate could not be verified."
	case strings.Contains(msg, "no route to host"), strings.Contains(msg, "network is unreachable"):
		return ConnUnreachable, "No route to the API server. Are you on the right network or VPN?"
	case strings.Contains(msg, "connection refused"):
		return ConnUnreachable, "The API server refused the connection. Is the cluster running?"
	case strings.Contains(msg, "no such host"), isDNSError(err):
		return ConnUnreachable, "The API server hostname does not resolve."
	case isTimeoutError(err):
		return ConnTimeout, "The API server did not answer in time. Are you on the right network or VPN?"
	case isNetworkError(err):
		return ConnUnreachable, "The API server could not be reached."
	}
	return ConnUnknown, "See the error below for details."
}

func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

func isNetworkError(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// FriendlyError renders err as a single line suitable for a status bar.
func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// client-go wraps transport failures in several layers; the innermost
	// sentence is the one that means something to a human.
	if idx := strings.LastIndex(msg, ": "); idx > 0 && idx < len(msg)-2 {
		tail := msg[idx+2:]
		if len(tail) > 12 {
			msg = tail
		}
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Sprintf("%v", err)
	}
	return msg
}
