package metrics

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func TestNodeSamplesCarryTheirUnitsAndTheirAge(t *testing.T) {
	raw := []byte(`{"items":[
		{"metadata":{"name":"node-1"},"timestamp":"2026-09-02T12:00:00Z","window":"30s",
		 "usage":{"cpu":"1530m","memory":"4194304Ki"}},
		{"metadata":{"name":"node-2"},"timestamp":"2026-09-02T12:00:00Z","window":"30s",
		 "usage":{"cpu":"2","memory":"1Gi"}}]}`)

	nodes, err := decodeNodes(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(nodes))
	}
	if nodes[0].CPUMilli != 1530 || nodes[0].MemoryBytes != 4<<30 {
		t.Errorf("node-1 is %dm / %d bytes", nodes[0].CPUMilli, nodes[0].MemoryBytes)
	}
	// Whole cores and millicores are the same number in different clothes.
	if nodes[1].CPUMilli != 2000 || nodes[1].MemoryBytes != 1<<30 {
		t.Errorf("node-2 is %dm / %d bytes", nodes[1].CPUMilli, nodes[1].MemoryBytes)
	}
	if nodes[0].Window != 30*time.Second {
		t.Errorf("the averaging window must be kept, got %v", nodes[0].Window)
	}
	if nodes[0].At.IsZero() {
		t.Error("a sample with no age is a number nobody can judge")
	}
}

func TestPodSampleIsTheSumOfItsContainers(t *testing.T) {
	raw := []byte(`{"items":[{"metadata":{"name":"api-0","namespace":"default"},
		"timestamp":"2026-09-02T12:00:00Z","containers":[
			{"name":"app","usage":{"cpu":"100m","memory":"64Mi"}},
			{"name":"sidecar","usage":{"cpu":"25m","memory":"16Mi"}}]}]}`)

	pods, err := decodePods(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("want 1 pod, got %d", len(pods))
	}
	if pods[0].Namespace != "default" || pods[0].Name != "api-0" {
		t.Errorf("the pod is %s/%s", pods[0].Namespace, pods[0].Name)
	}
	if pods[0].CPUMilli != 125 || pods[0].MemoryBytes != 80<<20 {
		t.Errorf("what a pod costs its node is every container of it: got %dm / %d bytes",
			pods[0].CPUMilli, pods[0].MemoryBytes)
	}
}

func TestAnUnreadableQuantityIsDroppedRatherThanGuessed(t *testing.T) {
	raw := []byte(`{"items":[{"metadata":{"name":"node-1"},
		"usage":{"cpu":"not-a-number","memory":"512Mi"}}]}`)

	nodes, err := decodeNodes(raw)
	if err != nil {
		t.Fatalf("one bad field must not lose the whole list: %v", err)
	}
	if nodes[0].CPUMilli != 0 {
		t.Errorf("an unparseable quantity must not become a number, got %dm", nodes[0].CPUMilli)
	}
	if nodes[0].MemoryBytes != 512<<20 {
		t.Errorf("the field beside it must survive, got %d bytes", nodes[0].MemoryBytes)
	}
}

func TestADocumentThatIsNotMetricsIsAnError(t *testing.T) {
	if _, err := decodeNodes([]byte("<html>404</html>")); err == nil {
		t.Error("a body that is not the metrics document must not decode as an empty cluster")
	}
	if _, err := decodePods([]byte("{")); err == nil {
		t.Error("a truncated body must be an error")
	}
}

func TestAnEmptyListIsAnAnswerNotAFailure(t *testing.T) {
	nodes, err := decodeNodes([]byte(`{"items":[]}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("want no samples, got %d", len(nodes))
	}
}

func TestReasonSeparatesTheAnswersThatDiffer(t *testing.T) {
	gr := schema.GroupResource{Group: "metrics.k8s.io", Resource: "nodes"}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"not installed", apierrors.NewNotFound(gr, ""), "not installed"},
		{"forbidden", apierrors.NewForbidden(gr, "", errors.New("nope")), "not permitted"},
		{"starting up", apierrors.NewServiceUnavailable("no endpoints"), "not answering yet"},
		{"timed out", context.DeadlineExceeded, "timed out"},
		{"cancelled", context.Canceled, "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Reason(tc.err); !strings.Contains(got, tc.want) {
				t.Errorf("Reason(%v) = %q, want it to mention %q", tc.err, got, tc.want)
			}
		})
	}
	if got := Reason(nil); got != "" {
		t.Errorf("no error is no reason, got %q", got)
	}
}

func TestReasonKeepsTheSentenceThatSaysWhatBroke(t *testing.T) {
	err := errors.New("get https://cluster/apis/metrics.k8s.io/v1beta1/nodes: connection refused by the proxy")
	if got := Reason(err); got != "connection refused by the proxy" {
		t.Errorf("Reason kept %q, want the innermost sentence", got)
	}
	// Too short to be a sentence of its own: the whole message is clearer.
	short := errors.New("reading body: EOF")
	if got := Reason(short); got != short.Error() {
		t.Errorf("Reason(%q) = %q, want the whole message", short, got)
	}
}

func TestPodsPathIsScopedByNamespace(t *testing.T) {
	if got := podsPath(""); got != GroupPath+"/pods" {
		t.Errorf("all namespaces reads %q", got)
	}
	if got := podsPath("payments"); got != GroupPath+"/namespaces/payments/pods" {
		t.Errorf("a scoped read is %q", got)
	}
}

// restClientTo builds a REST client pointed at a test server, which is how
// Collect is exercised without a cluster.
func restClientTo(t *testing.T, handler http.HandlerFunc) rest.Interface {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := rest.RESTClientFor(&rest.Config{
		Host: srv.URL,
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Group: "metrics.k8s.io", Version: "v1beta1"},
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	})
	if err != nil {
		t.Fatalf("rest client: %v", err)
	}
	return client
}

func TestCollectReadsBothHalves(t *testing.T) {
	client := restClientTo(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			io.WriteString(w, `{"items":[{"metadata":{"name":"node-1"},"window":"30s",`+
				`"usage":{"cpu":"1500m","memory":"2Gi"}}]}`)
		default:
			io.WriteString(w, `{"items":[{"metadata":{"name":"api-0","namespace":"default"},`+
				`"containers":[{"name":"app","usage":{"cpu":"100m","memory":"64Mi"}}]}]}`)
		}
	})

	snapshot, err := Collect(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(snapshot.Nodes) != 1 || len(snapshot.Pods) != 1 {
		t.Fatalf("got %d nodes and %d pods", len(snapshot.Nodes), len(snapshot.Pods))
	}
	if snapshot.Missing != "" {
		t.Errorf("nothing was missing, yet Collect says %q", snapshot.Missing)
	}
	if snapshot.At.IsZero() {
		t.Error("a snapshot with no time is a snapshot nobody can judge")
	}
}

// A partial answer is worth showing: namespace-scoped rights routinely get pod
// metrics and are refused node metrics.
func TestCollectNamesTheHalfItCouldNotRead(t *testing.T) {
	client := restClientTo(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/nodes") {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure",`+
				`"reason":"Forbidden","code":403}`)
			return
		}
		io.WriteString(w, `{"items":[{"metadata":{"name":"api-0","namespace":"default"},`+
			`"containers":[{"name":"app","usage":{"cpu":"100m","memory":"64Mi"}}]}]}`)
	})

	snapshot, err := Collect(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("half an answer must not be an error: %v", err)
	}
	if len(snapshot.Pods) != 1 {
		t.Fatalf("the half that answered must be kept, got %d pods", len(snapshot.Pods))
	}
	if !strings.HasPrefix(snapshot.Missing, "node metrics: ") {
		t.Errorf("the missing half must be named, got %q", snapshot.Missing)
	}
	if !strings.Contains(snapshot.Missing, "not permitted") {
		t.Errorf("and why, got %q", snapshot.Missing)
	}
}

// Both halves failing is one fact — usually that nothing is installed — and it
// is the caller's to report.
func TestCollectFailsWhenNeitherHalfAnswers(t *testing.T) {
	client := restClientTo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure",`+
			`"reason":"NotFound","code":404}`)
	})

	snapshot, err := Collect(context.Background(), client, "")
	if err == nil {
		t.Fatal("no metrics at all must be reported, not returned as an empty snapshot")
	}
	if len(snapshot.Nodes) != 0 || len(snapshot.Pods) != 0 {
		t.Error("a failed collect must not hand back half-built numbers")
	}
	if got := Reason(err); !strings.Contains(got, "not installed") {
		t.Errorf("Reason(%v) = %q, want it to name the missing API", err, got)
	}
}
