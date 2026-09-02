package main

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	fmt.Printf("deleted %d namespace(s) and %d CRD(s) in %s\n",
		len(namespaces.Items), len(crds.Items), time.Since(start).Round(time.Millisecond))
	return nil
}

func logf(opts options, format string, args ...any) {
	if opts.quiet {
		return
	}
	fmt.Printf(format, args...)
}
