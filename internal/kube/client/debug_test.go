package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
)

func TestEphemeralContainerUpdateKeepsExistingContainersAndIdentity(t *testing.T) {
	updated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"api","namespace":"team","uid":"uid","resourceVersion":"42"},"spec":{"containers":[{"name":"app","image":"app"}],"ephemeralContainers":[{"name":"existing","image":"toolbox"}]},"status":{"phase":"Running"}}`))
			return
		}
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/namespaces/team/pods/api/ephemeralcontainers" {
			t.Errorf("wrong mutation: %s %s", r.Method, r.URL.Path)
		}
		var pod corev1.Pod
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Error(readErr)
			return
		}
		if _, _, decodeErr := scheme.Codecs.UniversalDeserializer().Decode(body, nil, &pod); decodeErr != nil {
			t.Error(decodeErr)
			return
		}
		if pod.ResourceVersion != "42" || len(pod.Spec.EphemeralContainers) != 2 || pod.Spec.EphemeralContainers[0].Name != "existing" || pod.Spec.EphemeralContainers[1].TargetContainerName != "app" {
			t.Errorf("incorrect update: %+v", pod.Spec.EphemeralContainers)
		}
		updated = true
		_ = json.NewEncoder(w).Encode(pod)
	}))
	defer server.Close()
	f := debugTestFactory(t, server.URL)
	name, err := f.AddDebugContainer(context.Background(), "staging", "team", "api", "uid", "42", "busybox:1.37.0", "app")
	if err != nil || !updated || !strings.HasPrefix(name, "correlux-debug-") {
		t.Fatalf("name=%s updated=%v err=%v", name, updated, err)
	}
	updated = false
	if _, err = f.AddDebugContainer(context.Background(), "staging", "team", "api", "different-uid", "42", "busybox", "app"); err == nil || updated {
		t.Fatal("mutated a replacement pod")
	}
}

func TestDebugSessionListIsScopedAndBoundedOnTheServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/batch/v1/namespaces/team/jobs" || r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("continue") != "next" || r.URL.Query().Get("labelSelector") != "correlux.dev/debug-session=true" {
			t.Errorf("wrong scope: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"batch/v1","kind":"JobList","items":[]}`))
	}))
	defer server.Close()
	f := debugTestFactory(t, server.URL)
	if _, err := f.DebugSessions(context.Background(), "staging", "team", "next"); err != nil {
		t.Fatal(err)
	}
}

func debugTestFactory(t *testing.T, serverURL string) *Factory {
	t.Helper()
	f := newFactory(t, Options{})
	f.raw.Clusters["staging"].Server = serverURL
	raw, err := clientcmd.Write(f.raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.rules.ExplicitPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return f
}
