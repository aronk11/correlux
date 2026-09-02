// Package metrics reads live CPU and memory use from the Metrics API.
//
// The API is optional (SPEC 23): most clusters have Metrics Server, plenty do
// not, and a few have it installed but not yet answering. Everything here is
// therefore written so that "no metrics" is a first-class answer rather than a
// failure — Collect says which half it could not read, and Reason turns the
// error into one sentence a user can act on.
//
// It is read through the REST client with an explicit path, the way
// kube/resources reads server-rendered tables, rather than by depending on the
// metrics-server client library for two structs and a URL.
package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// GroupPath is where the Metrics API lives. `kubeui doctor` probes it to say
// whether metrics are available at all.
const GroupPath = "/apis/metrics.k8s.io/v1beta1"

// NodeSample is one node's current use, as the metrics API reported it.
type NodeSample struct {
	Name        string
	CPUMilli    int64
	MemoryBytes int64
	// At and Window say when the sample was taken and over how long. A number
	// with no age is a number nobody can judge.
	At     time.Time
	Window time.Duration
}

// PodSample is one pod's current use, summed over its containers — which is
// what a pod costs its node.
type PodSample struct {
	Namespace   string
	Name        string
	CPUMilli    int64
	MemoryBytes int64
	At          time.Time
}

// Snapshot is one read of the Metrics API.
type Snapshot struct {
	Nodes []NodeSample
	Pods  []PodSample
	// Missing names the half that could not be read, empty when both answered.
	// A user with namespace-scoped rights routinely gets pod metrics and is
	// refused node metrics, and half an answer is worth showing.
	Missing string
	At      time.Time
}

// Collect reads node and pod use in one round trip's latency.
//
// Both lists are single requests. The metrics API holds one small sample per
// object and nothing older, so its response is bounded by the scope in exactly
// the way the pod list already is; asking for it page by page would cost more
// requests for the same bytes. The namespace scopes the pod half; nodes are
// cluster-scoped and are simply not read when the caller may not have them.
func Collect(ctx context.Context, client rest.Interface, namespace string) (Snapshot, error) {
	out := Snapshot{At: time.Now()}

	var (
		mu              sync.Mutex
		wg              sync.WaitGroup
		nodeErr, podErr error
		nodes           []NodeSample
		pods            []PodSample
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		raw, err := get(ctx, client, GroupPath+"/nodes")
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			nodeErr = err
			return
		}
		nodes, nodeErr = decodeNodes(raw)
	}()
	go func() {
		defer wg.Done()
		raw, err := get(ctx, client, podsPath(namespace))
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			podErr = err
			return
		}
		pods, podErr = decodePods(raw)
	}()
	wg.Wait()

	// Both halves failing is one fact — usually that the API is not installed —
	// and the caller reports it as such. One half failing is a partial answer.
	if nodeErr != nil && podErr != nil {
		return Snapshot{}, podErr
	}
	out.Nodes, out.Pods = nodes, pods
	switch {
	case nodeErr != nil:
		out.Missing = "node metrics: " + Reason(nodeErr)
	case podErr != nil:
		out.Missing = "pod metrics: " + Reason(podErr)
	}
	return out, nil
}

func podsPath(namespace string) string {
	if namespace == "" {
		return GroupPath + "/pods"
	}
	return GroupPath + "/namespaces/" + namespace + "/pods"
}

func get(ctx context.Context, client rest.Interface, path string) ([]byte, error) {
	return client.Get().AbsPath(path).SetHeader("Accept", "application/json").DoRaw(ctx)
}

// nodeList and podList are the parts of the metrics documents kubeui reads.
// Everything else the API sends is ignored rather than decoded, so a field
// added in a later version costs nothing.
type nodeList struct {
	Items []struct {
		Metadata  metav1.ObjectMeta `json:"metadata"`
		Timestamp time.Time         `json:"timestamp"`
		Window    string            `json:"window"`
		Usage     map[string]string `json:"usage"`
	} `json:"items"`
}

type podList struct {
	Items []struct {
		Metadata   metav1.ObjectMeta `json:"metadata"`
		Timestamp  time.Time         `json:"timestamp"`
		Containers []struct {
			Name  string            `json:"name"`
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

func decodeNodes(raw []byte) ([]NodeSample, error) {
	var list nodeList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decode node metrics: %w", err)
	}
	out := make([]NodeSample, 0, len(list.Items))
	for _, item := range list.Items {
		cpu, mem := amounts(item.Usage)
		sample := NodeSample{
			Name:        item.Metadata.Name,
			CPUMilli:    cpu,
			MemoryBytes: mem,
			At:          item.Timestamp,
		}
		if window, err := time.ParseDuration(item.Window); err == nil {
			sample.Window = window
		}
		out = append(out, sample)
	}
	return out, nil
}

func decodePods(raw []byte) ([]PodSample, error) {
	var list podList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decode pod metrics: %w", err)
	}
	out := make([]PodSample, 0, len(list.Items))
	for _, item := range list.Items {
		sample := PodSample{
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			At:        item.Timestamp,
		}
		// A pod's use is what its containers use: the API reports them one by
		// one and never totals them.
		for _, c := range item.Containers {
			cpu, mem := amounts(c.Usage)
			sample.CPUMilli += cpu
			sample.MemoryBytes += mem
		}
		out = append(out, sample)
	}
	return out, nil
}

// amounts parses the usage map. An unparseable quantity is dropped rather than
// guessed at: a wrong number here would be indistinguishable from a real one.
func amounts(usage map[string]string) (cpuMilli, memoryBytes int64) {
	if cpu, err := resource.ParseQuantity(usage["cpu"]); err == nil {
		cpuMilli = cpu.MilliValue()
	}
	if mem, err := resource.ParseQuantity(usage["memory"]); err == nil {
		memoryBytes = mem.Value()
	}
	return cpuMilli, memoryBytes
}

// Reason turns a failed read into the shortest true sentence about it. The
// three cases that matter are told apart because the answer differs: install
// Metrics Server, ask for permission, or wait.
func Reason(err error) string {
	switch {
	case err == nil:
		return ""
	case apierrors.IsNotFound(err):
		return "the metrics API is not installed in this cluster"
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return "not permitted for this user"
	case apierrors.IsServiceUnavailable(err):
		return "the metrics API is installed but not answering yet"
	case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err), errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return innermost(err.Error())
	}
}

// innermost keeps the last sentence of a wrapped error, which is the one that
// says what actually went wrong.
func innermost(msg string) string {
	if idx := strings.LastIndex(msg, ": "); idx > 0 && idx < len(msg)-2 {
		if tail := msg[idx+2:]; len(tail) > 12 {
			return tail
		}
	}
	return msg
}
