package client

import (
	"errors"
	"net/http"
	"strings"
)

// ErrReadOnly is what a request refused by a read-only context fails with. The
// UI never offers such a request in the first place; this is the second lock,
// for the code path somebody adds next year and forgets to guard.
var ErrReadOnly = errors.New("refused by Correlux: this context is read-only")

// readOnlyTransport refuses, before it leaves the machine, every request that
// could change a cluster or run something inside it.
//
// It sits beneath every client the factory builds — typed, dynamic, exec and
// port-forward alike — because a read-only mode that only hides keys is a
// promise about the keys, not about the cluster.
type readOnlyTransport struct {
	next http.RoundTripper
}

func (t readOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !readOnlyAllows(req.Method, req.URL.Path) {
		// The body is the caller's to close when the request is not sent.
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, ErrReadOnly
	}
	return t.next.RoundTrip(req)
}

// readOnlyAllows reports whether a request only reads.
//
// Reading means the safe HTTP methods, except where Kubernetes uses one of
// them to open a session inside a pod: a websocket exec is a GET, and what it
// runs is anything at all. The one POST allowed is asking the API server what
// the current account may do, which is itself a read (`correlux doctor`).
func readOnlyAllows(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return !opensSession(path)
	case http.MethodPost:
		return asksPermission(path)
	default:
		return false
	}
}

// opensSession reports the pod subresources that run something or connect to
// something inside the cluster rather than describing it.
func opensSession(path string) bool {
	for _, sub := range []string{"/exec", "/attach", "/portforward"} {
		if strings.HasSuffix(path, sub) {
			return true
		}
	}
	return false
}

func asksPermission(path string) bool {
	for _, review := range []string{
		"/apis/authorization.k8s.io/v1/selfsubjectaccessreviews",
		"/apis/authorization.k8s.io/v1/selfsubjectrulesreviews",
		"/apis/authentication.k8s.io/v1/selfsubjectreviews",
	} {
		if strings.HasSuffix(path, review) {
			return true
		}
	}
	return false
}
