package main

import (
	"context"
	"fmt"
	"math"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// health decides what a seeded application looks like. A cluster where
// everything is green is not a useful test cluster: kubeui exists to show what
// is broken, so the seeder produces a realistic mix.
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

// seedApplication creates one application: a paused Deployment, the ReplicaSet
// it owns, the pods that ReplicaSet owns, and the Service, ConfigMap and Secret
// that belong with them.
//
// The Deployment is paused and the ReplicaSet is already at its desired count,
// so the real controllers observe a satisfied state and create nothing of their
// own — the cluster stays exactly as large as the seeder made it.
func seedApplication(ctx context.Context, c *clients, opts options, namespace, app string) error {
	state := healthFor(hash(namespace + app))
	selector := map[string]string{"app.kubernetes.io/name": app}
	podLabels := labels(map[string]string{
		"app.kubernetes.io/name":     app,
		"app.kubernetes.io/instance": app,
		"pod-template-hash":          "seed",
	})

	// Clamped to the int32 range just above, so the conversion cannot overflow.
	//nolint:gosec // G115: the value is bounded by the clamp on the previous line
	replicas := int32(min(max(opts.podsPerApp, 0), math.MaxInt32))
	ready := replicas
	switch state {
	case degraded:
		ready = replicas - 1
	case down:
		ready = 0
	}
	if ready < 0 {
		ready = 0
	}

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
			Template: podTemplate(app, podLabels),
		},
	}
	if err := created(createDeployment(ctx, c, namespace, deployment)); err != nil {
		return fmt.Errorf("deployment %s/%s: %w", namespace, app, err)
	}
	deployment, getErr := c.core.AppsV1().Deployments(namespace).Get(ctx, app, metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}

	rsName := app + "-seed"
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rsName,
			Namespace: namespace,
			Labels:    podLabels,
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
			Template: podTemplate(app, podLabels),
		},
	}
	if err := created(createReplicaSet(ctx, c, namespace, rs)); err != nil {
		return fmt.Errorf("replicaset %s/%s: %w", namespace, rsName, err)
	}
	rs, getErr = c.core.AppsV1().ReplicaSets(namespace).Get(ctx, rsName, metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}

	for i := 0; i < opts.podsPerApp; i++ {
		podState := healthy
		if int64(i) >= int64(ready) {
			podState = state
		}
		if err := seedPod(ctx, c, namespace, app, rs, podLabels, i, podState); err != nil {
			return err
		}
	}

	if err := seedService(ctx, c, namespace, app, selector); err != nil {
		return err
	}
	return seedConfig(ctx, c, namespace, app)
}

func podTemplate(app string, podLabels map[string]string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
		Spec: corev1.PodSpec{
			// Bound directly to the synthetic node: no scheduler involved and
			// no kubelet to start anything.
			NodeName: nodeName,
			Tolerations: []corev1.Toleration{
				{Key: "kubeui.dev/synthetic", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
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
	rs *appsv1.ReplicaSet,
	podLabels map[string]string,
	index int,
	state health,
) error {
	name := fmt.Sprintf("%s-seed-%s", app, suffix(namespace, app, index))
	template := podTemplate(app, podLabels)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    podLabels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rs, appsv1.SchemeGroupVersion.WithKind("ReplicaSet")),
			},
		},
		Spec: template.Spec,
	}
	if err := created(createPod(ctx, c, namespace, pod)); err != nil {
		return fmt.Errorf("pod %s/%s: %w", namespace, name, err)
	}
	return setPodStatus(ctx, c, namespace, name, app, state)
}

// setPodStatus writes the status a kubelet would have written. This is what
// makes the seeded cluster useful for testing kubeui's health rendering: pods
// are Running, CrashLoopBackOff or OOMKilled because their status says so.
func setPodStatus(ctx context.Context, c *clients, namespace, name, app string, state health) error {
	pod, err := c.core.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	now := metav1.Now()
	containerState := corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}
	phase := corev1.PodRunning
	ready := true
	restarts := int32(0)

	switch state {
	case degraded:
		containerState = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason:  "CrashLoopBackOff",
			Message: "back-off 5m0s restarting failed container",
		}}
		ready = false
		restarts = 7
	case down:
		containerState = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason:     "OOMKilled",
			ExitCode:   137,
			StartedAt:  now,
			FinishedAt: now,
		}}
		phase = corev1.PodFailed
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
			Name:         app,
			Image:        "registry.k8s.io/pause:3.10",
			Ready:        ready,
			RestartCount: restarts,
			State:        containerState,
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
