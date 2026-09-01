package main

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// clean deletes everything the seeder created, and only that: every object
// carries the managed-by label, and namespaces are removed wholesale.
func clean(ctx context.Context, c *clients) error {
	start := time.Now()
	selector := metav1.ListOptions{LabelSelector: managedByLabel + "=" + managedByValue}

	namespaces, err := c.core.CoreV1().Namespaces().List(ctx, selector)
	if err != nil {
		return fmt.Errorf("list seeded namespaces: %w", err)
	}
	for i := range namespaces.Items {
		name := namespaces.Items[i].Name
		// Force-delete the pods first. Nothing confirms a graceful deletion for
		// a pod on a node with no kubelet, so a namespace containing one would
		// sit in Terminating indefinitely.
		if delErr := c.core.CoreV1().Pods(name).DeleteCollection(ctx,
			metav1.DeleteOptions{GracePeriodSeconds: ptr(int64(0))},
			metav1.ListOptions{}); delErr != nil {
			if !apierrors.IsNotFound(delErr) {
				return fmt.Errorf("force-delete pods in %s: %w", name, delErr)
			}
		}
		if delErr := c.core.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); delErr != nil {
			if !apierrors.IsNotFound(delErr) {
				return fmt.Errorf("delete namespace %s: %w", name, delErr)
			}
		}
	}

	crds, err := c.ext.ApiextensionsV1().CustomResourceDefinitions().List(ctx, selector)
	if err != nil {
		return fmt.Errorf("list seeded CRDs: %w", err)
	}
	for i := range crds.Items {
		name := crds.Items[i].Name
		if delErr := c.ext.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, name, metav1.DeleteOptions{}); delErr != nil {
			if !apierrors.IsNotFound(delErr) {
				return fmt.Errorf("delete crd %s: %w", name, delErr)
			}
		}
	}

	if delErr := c.core.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{}); delErr != nil {
		if !apierrors.IsNotFound(delErr) {
			return fmt.Errorf("delete node %s: %w", nodeName, delErr)
		}
	}

	// Namespace deletion is asynchronous. Returning before it finishes makes
	// `clean && seed` fail with "namespace is being terminated", so wait.
	if err := waitForNamespacesGone(ctx, c, selector); err != nil {
		return err
	}

	fmt.Printf("deleted %d namespace(s) and %d CRD(s) in %s\n",
		len(namespaces.Items), len(crds.Items), time.Since(start).Round(time.Millisecond))
	return nil
}

func waitForNamespacesGone(ctx context.Context, c *clients, selector metav1.ListOptions) error {
	return wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 5*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			remaining, err := c.core.CoreV1().Namespaces().List(ctx, selector)
			if err != nil {
				return false, err
			}
			return len(remaining.Items) == 0, nil
		})
}

func logf(opts options, format string, args ...any) {
	if opts.quiet {
		return
	}
	fmt.Printf(format, args...)
}
