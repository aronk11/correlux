package client

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ConnState
	}{
		{"nil", nil, ConnOK},
		{"deadline", context.DeadlineExceeded, ConnTimeout},
		{"cancelled", context.Canceled, ConnUnknown},
		{"unauthorized", apierrors.NewUnauthorized("bad token"), ConnUnauthorized},
		{
			"forbidden",
			apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "x", errors.New("no")),
			ConnForbidden,
		},
		{"server timeout", apierrors.NewTimeoutError("slow", 1), ConnTimeout},
		{"no route", errors.New(`Get "https://x": dial tcp 10.0.0.1:6443: connect: no route to host`), ConnUnreachable},
		{"refused", errors.New("dial tcp: connect: connection refused"), ConnUnreachable},
		{"dns", &url.Error{Op: "Get", URL: "https://x", Err: &net.DNSError{Err: "no such host", IsNotFound: true}}, ConnUnreachable},
		{"x509", errors.New("x509: certificate signed by unknown authority"), ConnTLSError},
		{"tls", errors.New("tls: failed to verify certificate"), ConnTLSError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, hint := classify(tc.err)
			if got != tc.want {
				t.Errorf("classify(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if got != ConnOK && got != ConnUnknown && hint == "" {
				t.Error("every failure state must carry an actionable hint")
			}
		})
	}
}

func TestConnStateString(t *testing.T) {
	// The state name is user-visible, so it must never be a bare number.
	for state := ConnUnknown; state <= ConnConfigError; state++ {
		if state.String() == "" {
			t.Errorf("state %d has no label", state)
		}
	}
}

func TestFriendlyErrorUnwrapsClientGoLayers(t *testing.T) {
	err := errors.New(`Get "https://api.example.com/version": dial tcp 10.0.0.1:6443: connect: no route to host`)
	got := FriendlyError(err)
	if got == "" || len(got) >= len(err.Error()) {
		t.Errorf("FriendlyError did not shorten the message: %q", got)
	}
}

func TestFriendlyErrorHandlesNil(t *testing.T) {
	if got := FriendlyError(nil); got != "" {
		t.Errorf("FriendlyError(nil) = %q, want empty", got)
	}
}
