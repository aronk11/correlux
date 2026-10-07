package client

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		{"nil", nil, false},
		{"timeout", apierrors.NewTimeoutError("slow", 1), true},
		{"service unavailable", apierrors.NewServiceUnavailable("down"), true},
		{"internal error", apierrors.NewInternalError(errors.New("oops")), true},
		{"context deadline", context.DeadlineExceeded, true},
		{"context canceled", context.Canceled, false},
		{"unauthorized", apierrors.NewUnauthorized("bad token"), false},
		{"forbidden", apierrors.NewForbidden(
			schema.GroupResource{Resource: "pods"}, "x", errors.New("no")), false},
		{"not found", apierrors.NewNotFound(
			schema.GroupResource{Resource: "pods"}, "x"), false},
		{"bad request", apierrors.NewBadRequest("invalid"), false},
		{"conflict", apierrors.NewConflict(
			schema.GroupResource{Resource: "pods"}, "x", errors.New("conflict")), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransient(tt.err); got != tt.transient {
				t.Errorf("IsTransient(%v) = %v, want %v", tt.err, got, tt.transient)
			}
		})
	}
}

func TestIsAuthentication(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		isAuth bool
	}{
		{"unauthorized", apierrors.NewUnauthorized("bad token"), true},
		{"forbidden", apierrors.NewForbidden(
			schema.GroupResource{Resource: "pods"}, "x", errors.New("no")), true},
		{"not found", apierrors.NewNotFound(
			schema.GroupResource{Resource: "pods"}, "x"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuthentication(tt.err); got != tt.isAuth {
				t.Errorf("IsAuthentication(%v) = %v, want %v", tt.err, got, tt.isAuth)
			}
		})
	}
}

func TestRetryBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1600 * time.Millisecond},
		{5, 3200 * time.Millisecond},
		{6, 3200 * time.Millisecond}, // capped at 5
		{-1, 100 * time.Millisecond}, // negative treated as 0
	}

	for _, tt := range tests {
		t.Run(("attempt " + string(rune(tt.attempt))), func(t *testing.T) {
			if got := RetryBackoff(tt.attempt); got != tt.want {
				t.Errorf("RetryBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "x")) {
		t.Error("IsNotFound should return true for 404")
	}
	if IsNotFound(apierrors.NewTimeoutError("slow", 1)) {
		t.Error("IsNotFound should return false for timeout")
	}
}

func TestIsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		invalid bool
	}{
		{"bad request", apierrors.NewBadRequest("invalid"), true},
		{"invalid", apierrors.NewInvalid(
			schema.GroupKind{Kind: "Pod"}, "x", nil), true},
		{"conflict", apierrors.NewConflict(
			schema.GroupResource{Resource: "pods"}, "x", errors.New("conflict")), true},
		{"timeout", apierrors.NewTimeoutError("slow", 1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsInvalid(tt.err); got != tt.invalid {
				t.Errorf("IsInvalid(%v) = %v, want %v", tt.err, got, tt.invalid)
			}
		})
	}
}
