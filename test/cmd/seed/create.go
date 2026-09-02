package main

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Thin creation helpers, so the seeding logic reads as a description of the
// cluster it builds rather than as client-go boilerplate.

func createDeployment(ctx context.Context, c *clients, ns string, obj *appsv1.Deployment) error {
	_, err := c.core.AppsV1().Deployments(ns).Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func createReplicaSet(ctx context.Context, c *clients, ns string, obj *appsv1.ReplicaSet) error {
	_, err := c.core.AppsV1().ReplicaSets(ns).Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func createPod(ctx context.Context, c *clients, ns string, obj *corev1.Pod) error {
	_, err := c.core.CoreV1().Pods(ns).Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func createConfigMap(ctx context.Context, c *clients, ns string, obj *corev1.ConfigMap) error {
	_, err := c.core.CoreV1().ConfigMaps(ns).Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func createSecret(ctx context.Context, c *clients, ns string, obj *corev1.Secret) error {
	_, err := c.core.CoreV1().Secrets(ns).Create(ctx, obj, metav1.CreateOptions{})
	return err
}
