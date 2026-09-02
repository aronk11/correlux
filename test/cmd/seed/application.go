package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// health decides what a seeded application looks like. A cluster where
// everything is green is not a useful test cluster: Correlux exists to show
// what is broken, so the seeder produces a realistic mix.
type health int

const (
	healthy health = iota
	degraded
	down
)

func healthFor(index int) health {
	switch {
	case index%17 == 0:
		return down
	case index%7 == 0:
		return degraded
	default:
		return healthy
	}
}

// seedApplication creates one application: a paused Deployment, the pods that
// belong to it, the ReplicaSet that adopts them, and the Service, ConfigMap and
// Secret that go with them.
//
// The order matters, and it is the whole trick. A ReplicaSet whose selector
// matches fewer pods than it wants has the controller create the difference —
// and those pods go through the scheduler, which cannot place them on a node
// with no kubelet, so they sit Pending forever. Creating the pods *first*, as
// orphans, and the ReplicaSet *after* means the controller finds its desired
// count already satisfied and adopts them, writing the owner references itself.
// The result is the ownership chain a real cluster has, and nothing to
// reconcile.
func seedApplication(ctx context.Context, c *clients, opts options, namespace, app string) error {
	state := healthFor(hash(namespace + app))
	selector := map[string]string{"app.kubernetes.io/name": app}

	// Two label sets, deliberately different. The pod template used by the
	// Deployment and the ReplicaSet does *not* carry the seeder's marker, so a
	// pod a controller creates from that template is distinguishable from one
	// the seeder created — which is what lets the settling pass remove exactly
	// the pods that can never run.
	templateLabels := labels(map[string]string{
		"app.kubernetes.io/name":     app,
		"app.kubernetes.io/instance": app,
		"pod-template-hash":          "seed",
	})
	podLabels := labels(map[string]string{
		"app.kubernetes.io/name":     app,
		"app.kubernetes.io/instance": app,
		"pod-template-hash":          "seed",
		seededLabel:                  "true",
	})

	// Clamped to the int32 range, so the conversion cannot overflow.
	//nolint:gosec // G115: the value is bounded by the clamp on this line
	replicas := int32(min(max(opts.podsPerApp, 0), math.MaxInt32))
	ready := replicas
	switch state {
	case degraded:
		ready = max(replicas-1, 0)
	case down:
		ready = 0
	}

	deployment, err := ensureDeployment(ctx, c, namespace, app, selector, templateLabels, replicas)
	if err != nil {
		return err
	}

	// Before touching the pods, release an existing ReplicaSet whose size no
	// longer matches. Deleting it with Orphan propagation leaves the pods
	// running and strips their owner references, so no controller reacts while
	// the set is being resized; the ReplicaSet recreated at the end adopts
	// whatever is there.
	if err := releaseReplicaSetIfResized(ctx, c, namespace, app, replicas); err != nil {
		return err
	}
	if err := sweepControllerPods(ctx, c, namespace, app); err != nil {
		return err
	}

	for i := 0; i < opts.podsPerApp; i++ {
		podState := healthy
		if int64(i) >= int64(ready) {
			podState = state
		}
		if err := seedPod(ctx, c, namespace, app, podLabels, i, podState); err != nil {
			return err
		}
	}
	// A previous run may have asked for more pods than this one does.
	if err := trimSurplusPods(ctx, c, namespace, app, opts.podsPerApp); err != nil {
		return err
	}

	if err := ensureReplicaSet(ctx, c, namespace, app, deployment, templateLabels, replicas); err != nil {
		return err
	}
	if err := seedService(ctx, c, namespace, app, selector); err != nil {
		return err
	}
	return seedConfig(ctx, c, namespace, app)
}

// ensureDeployment creates the paused Deployment at the head of the chain.
// Paused, so the deployment controller does not create a ReplicaSet of its own
// alongside the seeded one.
func ensureDeployment(
	ctx context.Context,
	c *clients,
	namespace, app string,
	selector, templateLabels map[string]string,
	replicas int32,
) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app,
			Namespace: namespace,
			Labels:    labels(selector),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Paused:   true,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: podTemplate(app, templateLabels),
		},
	}
	if err := created(createDeployment(ctx, c, namespace, deployment)); err != nil {
		return nil, fmt.Errorf("deployment %s/%s: %w", namespace, app, err)
	}

	current, err := c.core.AppsV1().Deployments(namespace).Get(ctx, app, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if current.Spec.Replicas != nil && *current.Spec.Replicas == replicas {
		return current, nil
	}
	if err := resizeDeployment(ctx, c, current, replicas); err != nil {
		return nil, fmt.Errorf("resize deployment %s/%s: %w", namespace, app, err)
	}
	return current, nil
}

// releaseReplicaSetIfResized removes a ReplicaSet whose desired count no longer
// matches, without touching the pods it owns.
func releaseReplicaSetIfResized(ctx context.Context, c *clients, namespace, app string, replicas int32) error {
	rsName := app + "-seed"
	existing, err := c.core.AppsV1().ReplicaSets(namespace).Get(ctx, rsName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing.Spec.Replicas != nil && *existing.Spec.Replicas == replicas {
		return nil
	}

	orphan := metav1.DeletePropagationOrphan
	if delErr := c.core.AppsV1().ReplicaSets(namespace).Delete(ctx, rsName, metav1.DeleteOptions{
		PropagationPolicy: &orphan,
	}); delErr != nil && !apierrors.IsNotFound(delErr) {
		return fmt.Errorf("release replicaset %s/%s: %w", namespace, rsName, delErr)
	}

	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, time.Minute, true,
		func(ctx context.Context) (bool, error) {
			_, getErr := c.core.AppsV1().ReplicaSets(namespace).Get(ctx, rsName, metav1.GetOptions{})
			if apierrors.IsNotFound(getErr) {
				return true, nil
			}
			return false, getErr
		})
}

// ensureReplicaSet creates the ReplicaSet that adopts the pods seeded above.
// By the time it runs, the pods already match the desired count, so the
// controller has nothing to create.
func ensureReplicaSet(
	ctx context.Context,
	c *clients,
	namespace, app string,
	deployment *appsv1.Deployment,
	templateLabels map[string]string,
	replicas int32,
) error {
	rsName := app + "-seed"
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rsName,
			Namespace: namespace,
			Labels:    templateLabels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, appsv1.SchemeGroupVersion.WithKind("Deployment")),
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"app.kubernetes.io/name": app,
				"pod-template-hash":      "seed",
			}},
			Template: podTemplate(app, templateLabels),
		},
	}
	if err := created(createReplicaSet(ctx, c, namespace, rs)); err != nil {
		return fmt.Errorf("replicaset %s/%s: %w", namespace, rsName, err)
	}

	return nil
}

// sweepControllerPods removes pods a ReplicaSet controller created itself.
// They are recognisable by the absence of the seeder's own label, they can
// never run — the scheduler cannot place a pod on a node with no kubelet — and
// leaving them would skew every count in the cluster.
func sweepControllerPods(ctx context.Context, c *clients, namespace, app string) error {
	pods, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=" + app + ",!" + seededLabel,
	})
	if err != nil {
		return fmt.Errorf("list controller pods in %s: %w", namespace, err)
	}
	for i := range pods.Items {
		name := pods.Items[i].Name
		if delErr := c.core.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
			GracePeriodSeconds: ptr(int64(0)),
		}); delErr != nil && !apierrors.IsNotFound(delErr) {
			return fmt.Errorf("delete controller pod %s/%s: %w", namespace, name, delErr)
		}
	}
	return nil
}

// trimSurplusPods removes the pods a previous, larger run left behind, so the
// ReplicaSet finds exactly the count it wants and creates nothing.
func trimSurplusPods(ctx context.Context, c *clients, namespace, app string, want int) error {
	pods, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=" + app,
	})
	if err != nil {
		return fmt.Errorf("list pods of %s/%s: %w", namespace, app, err)
	}
	if len(pods.Items) <= want {
		return nil
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
	for i := want; i < len(pods.Items); i++ {
		name := pods.Items[i].Name
		if delErr := c.core.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
			GracePeriodSeconds: ptr(int64(0)),
		}); delErr != nil && !apierrors.IsNotFound(delErr) {
			return fmt.Errorf("delete surplus pod %s/%s: %w", namespace, name, delErr)
		}
	}
	return nil
}

func podTemplate(app string, podLabels map[string]string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
		Spec: corev1.PodSpec{
			// Bound directly to the synthetic node: no scheduler involved and
			// no kubelet to start anything.
			NodeName: nodeName,
			// No kubelet also means nobody confirms a deletion, so a pod with
			// the usual grace period would sit in Terminating forever. Zero
			// makes the API server remove the object immediately.
			TerminationGracePeriodSeconds: ptr(int64(0)),
			Tolerations: []corev1.Toleration{
				{Key: "correlux.dev/synthetic", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
				// The node has no kubelet, so the node lifecycle controller will
				// eventually mark it unreachable. Without these tolerations it
				// would evict the load five minutes into a benchmark.
				{Key: "node.kubernetes.io/unreachable", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
				{Key: "node.kubernetes.io/not-ready", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
			},
			Containers: []corev1.Container{{
				Name:  app,
				Image: "registry.k8s.io/pause:3.10",
				Ports: []corev1.ContainerPort{{ContainerPort: 8080, Name: "http"}},
			}},
		},
	}
}

func seedPod(
	ctx context.Context,
	c *clients,
	namespace, app string,
	podLabels map[string]string,
	index int,
	state health,
) error {
	name := fmt.Sprintf("%s-seed-%s", app, suffix(namespace, app, index))
	template := podTemplate(app, podLabels)

	pod := &corev1.Pod{
		// No owner reference: the ReplicaSet created afterwards adopts the pod
		// and writes one itself, exactly as it would for any orphan.
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    podLabels,
		},
		Spec: template.Spec,
	}
	if err := created(createPod(ctx, c, namespace, pod)); err != nil {
		return fmt.Errorf("pod %s/%s: %w", namespace, name, err)
	}
	return setPodStatus(ctx, c, namespace, name, app, state)
}

// setPodStatus writes the status a kubelet would have written. This is what
// makes the seeded cluster useful for testing Correlux's health rendering: pods
// are Running, CrashLoopBackOff or OOMKilled because their status says so.
func setPodStatus(ctx context.Context, c *clients, namespace, name, app string, state health) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return setPodStatusOnce(ctx, c, namespace, name, app, state)
	})
}

func setPodStatusOnce(ctx context.Context, c *clients, namespace, name, app string, state health) error {
	pod, err := c.core.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	now := metav1.Now()
	containerState := corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}
	phase := corev1.PodRunning
	ready := true
	restarts := int32(0)

	var lastState corev1.ContainerState

	switch state {
	case degraded:
		containerState = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason:  "ImagePullBackOff",
			Message: "Back-off pulling image",
		}}
		ready = false
		restarts = 3
	case down:
		// A pod being OOM-killed in a loop stays in phase Running with a
		// container waiting to restart; that is both what a real cluster looks
		// like and what keeps the ReplicaSet controller from treating the pod
		// as gone and creating a replacement the scheduler can never place.
		containerState = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason:  "CrashLoopBackOff",
			Message: "back-off 5m0s restarting failed container",
		}}
		lastState = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason:     "OOMKilled",
			ExitCode:   137,
			StartedAt:  now,
			FinishedAt: now,
		}}
		ready = false
		restarts = 12
	}

	pod.Status = corev1.PodStatus{
		Phase:     phase,
		PodIP:     "10.244.0.1",
		HostIP:    "10.0.0.1",
		StartTime: &now,
		Conditions: []corev1.PodCondition{
			{Type: corev1.PodScheduled, Status: corev1.ConditionTrue, LastTransitionTime: now},
			{Type: corev1.PodReady, Status: conditionStatus(ready), LastTransitionTime: now},
			{Type: corev1.ContainersReady, Status: conditionStatus(ready), LastTransitionTime: now},
		},
		ContainerStatuses: []corev1.ContainerStatus{{
			Name:                 app,
			Image:                "registry.k8s.io/pause:3.10",
			Ready:                ready,
			RestartCount:         restarts,
			State:                containerState,
			LastTerminationState: lastState,
		}},
	}
	_, err = c.core.CoreV1().Pods(namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	return err
}

func conditionStatus(ok bool) corev1.ConditionStatus {
	if ok {
		return corev1.ConditionTrue
	}
	return corev1.ConditionFalse
}

func seedService(ctx context.Context, c *clients, namespace, app string, selector map[string]string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: app, Namespace: namespace, Labels: labels(selector)},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromString("http"),
			}},
		},
	}
	_, err := c.core.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if createErr := created(err); createErr != nil {
		return fmt.Errorf("service %s/%s: %w", namespace, app, createErr)
	}
	return nil
}

func seedConfig(ctx context.Context, c *clients, namespace, app string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: app, Namespace: namespace, Labels: labels(nil)},
		Data:       map[string]string{"LOG_LEVEL": "info", "REPLICAS": strconv.Itoa(3)},
	}
	if err := created(createConfigMap(ctx, c, namespace, cm)); err != nil {
		return fmt.Errorf("configmap %s/%s: %w", namespace, app, err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: app, Namespace: namespace, Labels: labels(nil)},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"token": "not-a-real-secret"},
	}
	if err := created(createSecret(ctx, c, namespace, secret)); err != nil {
		return fmt.Errorf("secret %s/%s: %w", namespace, app, err)
	}
	return nil
}

// resizeDeployment keeps a repeated run with a different --pods-per-app
// consistent. The deployment is paused, so this only changes what it reports.
func resizeDeployment(ctx context.Context, c *clients, obj *appsv1.Deployment, replicas int32) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := c.core.AppsV1().Deployments(obj.Namespace).Get(ctx, obj.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		current.Spec.Replicas = &replicas
		_, err = c.core.AppsV1().Deployments(obj.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	})
}

func ptr[T any](v T) *T { return &v }

// hash is a small, stable string hash: the health mix must be identical between
// runs so a benchmark comparison is meaningful.
func hash(s string) int {
	h := 0
	for _, r := range s {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return h
}

func suffix(namespace, app string, index int) string {
	return fmt.Sprintf("%04x%02d", hash(namespace+app)%0xffff, index)
}
