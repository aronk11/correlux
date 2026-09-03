// Command seed fills a Kubernetes cluster with realistic load for Correlux's
// integration and performance tests.
//
// It creates native resources (namespaces, deployments, replica sets, pods,
// services, config maps, secrets, ingresses) and custom ones (CRDs with printer
// columns, plus their custom resources), wired together with the owner
// references and selectors Correlux's application inference will rely on.
//
// Nothing it creates actually runs. Pods are attached to a node object that has
// no kubelet behind it and their status is written directly, which is how ten
// thousand pods can exist on a laptop: the API server and etcd carry exactly
// the load they would in production, and no container is started.
//
// Everything it creates carries the label
// app.kubernetes.io/managed-by=Correlux-seed, so --clean removes precisely the
// load and nothing else.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "correlux-seed"
	nodeName       = "correlux-load-node"
	// seededLabel marks pods the seeder created itself, so pods a controller
	// created during a resize can be told apart and removed.
	seededLabel = "correlux.dev/seeded"
	crdGroup    = "load.correlux.dev"
)

type options struct {
	kubeconfig       string
	namespaces       int
	appsPerNamespace int
	podsPerApp       int
	crds             int
	customResources  int
	workers          int
	prefix           string
	clean            bool
	quiet            bool
}

func main() {
	var opts options
	flag.StringVar(&opts.kubeconfig, "kubeconfig", "", "path to the kubeconfig (defaults to the usual rules)")
	flag.IntVar(&opts.namespaces, "namespaces", 5, "number of namespaces to create")
	flag.IntVar(&opts.appsPerNamespace, "apps-per-namespace", 4, "applications per namespace")
	flag.IntVar(&opts.podsPerApp, "pods-per-app", 3, "pods per application")
	flag.IntVar(&opts.crds, "crds", 2, "number of CustomResourceDefinitions to install")
	flag.IntVar(&opts.customResources, "custom-resources", 20, "custom resources per CRD")
	flag.IntVar(&opts.workers, "workers", 24, "parallel API writers")
	flag.StringVar(&opts.prefix, "prefix", "correlux-load", "name prefix for everything created")
	flag.BoolVar(&opts.clean, "clean", false, "delete everything the seeder created and exit")
	flag.BoolVar(&opts.quiet, "quiet", false, "only print the summary")
	flag.Parse()

	os.Exit(main2(opts))
}

// main2 exists so the signal handler's cleanup runs before the process exits.
func main2(opts options) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, "seed: "+err.Error())
		return 1
	}
	return 0
}

type clients struct {
	core    *kubernetes.Clientset
	ext     *apiextensionsclient.Clientset
	dynamic *dynamic.DynamicClient
}

func run(ctx context.Context, opts options) error {
	cfg, err := restConfig(opts.kubeconfig)
	if err != nil {
		return err
	}
	// The seeder is a load generator; it is allowed to be impolite.
	cfg.QPS = 200
	cfg.Burst = 400

	c := &clients{}
	if c.core, err = kubernetes.NewForConfig(cfg); err != nil {
		return err
	}
	if c.ext, err = apiextensionsclient.NewForConfig(cfg); err != nil {
		return err
	}
	if c.dynamic, err = dynamic.NewForConfig(cfg); err != nil {
		return err
	}

	if opts.clean {
		return clean(ctx, c)
	}
	return seed(ctx, c, opts)
}

func restConfig(path string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path != "" {
		rules.ExplicitPath = path
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func seed(ctx context.Context, c *clients, opts options) error {
	start := time.Now()
	total := opts.namespaces * opts.appsPerNamespace * opts.podsPerApp

	logf(opts, "seeding %d namespaces x %d apps x %d pods = %d pods, %d CRDs x %d custom resources\n",
		opts.namespaces, opts.appsPerNamespace, opts.podsPerApp, total, opts.crds, opts.customResources)

	if err := ensureNode(ctx, c.core); err != nil {
		return fmt.Errorf("create the load node: %w", err)
	}
	if err := ensureCRDs(ctx, c, opts); err != nil {
		return err
	}

	// Namespaces first: everything else needs them to exist.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.workers)
	for i := 0; i < opts.namespaces; i++ {
		name := namespaceName(opts, i)
		g.Go(func() error { return ensureNamespace(gctx, c.core, name) })
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("create namespaces: %w", err)
	}

	g, gctx = errgroup.WithContext(ctx)
	g.SetLimit(opts.workers)
	for i := 0; i < opts.namespaces; i++ {
		namespace := namespaceName(opts, i)
		for a := 0; a < opts.appsPerNamespace; a++ {
			app := fmt.Sprintf("app-%02d", a)
			g.Go(func() error { return seedApplication(gctx, c, opts, namespace, app) })
		}
		for crd := 0; crd < opts.crds; crd++ {
			g.Go(func() error { return seedCustomResources(gctx, c, opts, namespace, crd) })
		}
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// The ReplicaSet controllers work from an informer cache, so one may create
	// a pod or two before it observes the ones the seeder just made. Those are
	// scheduler-bound, can never run, and would skew every count — so settle
	// the cluster before declaring the run finished.
	strays, err := settle(ctx, c)
	if err != nil {
		return err
	}

	elapsed := time.Since(start)
	if strays > 0 {
		logf(opts, "removed %d pod(s) created by controllers during seeding\n", strays)
	}
	fmt.Printf("seeded %d pods across %d namespaces in %s (%.0f objects/s)\n",
		total, opts.namespaces, elapsed.Round(time.Millisecond),
		float64(objectCount(opts))/elapsed.Seconds())
	return nil
}

// objectCount is the number of API objects a full run creates, used for the
// throughput figure the load workflow records.
// settle repeatedly removes pods no controller can ever schedule, until a pass
// finds none. It returns how many it removed.
func settle(ctx context.Context, c *clients) (int, error) {
	const passes = 8
	removed := 0

	for pass := 0; pass < passes; pass++ {
		pods, err := c.core.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			LabelSelector: managedByLabel + "=" + managedByValue + ",!" + seededLabel,
		})
		if err != nil {
			return removed, fmt.Errorf("list unmanaged pods: %w", err)
		}
		if len(pods.Items) == 0 {
			return removed, nil
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			if delErr := c.core.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
				GracePeriodSeconds: ptr(int64(0)),
			}); delErr != nil && !apierrors.IsNotFound(delErr) {
				return removed, fmt.Errorf("delete pod %s/%s: %w", pod.Namespace, pod.Name, delErr)
			}
			removed++
		}
		select {
		case <-ctx.Done():
			return removed, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return removed, nil
}

func objectCount(opts options) int {
	perApp := 1 + 1 + 1 + 1 + 1 + opts.podsPerApp // deployment, rs, service, configmap, secret, pods
	return opts.namespaces*(opts.appsPerNamespace*perApp+opts.crds*opts.customResources) + opts.namespaces
}

func namespaceName(opts options, i int) string {
	return fmt.Sprintf("%s-%03d", opts.prefix, i)
}

func labels(extra map[string]string) map[string]string {
	out := map[string]string{managedByLabel: managedByValue}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// created reports whether an object was newly created; an AlreadyExists error
// means a previous run made it, which is not a failure.
func created(err error) error {
	if err == nil || apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// ensureNode registers a node with no kubelet behind it. Pods bound to it are
// never started, so the cluster can hold ten thousand of them on a laptop.
func ensureNode(ctx context.Context, cs kubernetes.Interface) error {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeName,
			Labels: labels(map[string]string{"kubernetes.io/hostname": nodeName, "correlux.dev/synthetic": "true"}),
		},
		Spec: corev1.NodeSpec{
			// Nothing real may be scheduled here by accident.
			Taints: []corev1.Taint{{
				Key:    "correlux.dev/synthetic",
				Value:  "true",
				Effect: corev1.TaintEffectNoSchedule,
			}},
		},
	}
	if err := created(createNode(ctx, cs, node)); err != nil {
		return err
	}
	return setNodeReady(ctx, cs)
}

func createNode(ctx context.Context, cs kubernetes.Interface, node *corev1.Node) error {
	_, err := cs.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	return err
}

// setNodeReady writes a Ready condition and a plausible capacity, so the node
// looks like a node to anything that reads it — Correlux included.
//
// The node lifecycle controller is writing to the same object (it has noticed
// there is no kubelet), so this races by construction and retries on conflict.
func setNodeReady(ctx context.Context, cs kubernetes.Interface) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return setNodeReadyOnce(ctx, cs)
	})
}

func setNodeReadyOnce(ctx context.Context, cs kubernetes.Interface) error {
	node, err := cs.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	node.Status.Capacity = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("64"),
		corev1.ResourceMemory: resource.MustParse("256Gi"),
		corev1.ResourcePods:   resource.MustParse("20000"),
	}
	node.Status.Allocatable = node.Status.Capacity
	node.Status.NodeInfo.KubeletVersion = "v0.0.0-correlux-seed"
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionTrue,
		Reason:             "KubeletReady",
		Message:            "synthetic node created by the correlux load seeder",
		LastHeartbeatTime:  metav1.Now(),
		LastTransitionTime: metav1.Now(),
	}}
	_, err = cs.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{})
	return err
}

func ensureNamespace(ctx context.Context, cs kubernetes.Interface, name string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels(nil)}}
	_, err := cs.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	return created(err)
}
