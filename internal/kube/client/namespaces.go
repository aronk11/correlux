package client

import (
	"context"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NamespaceList is the result of listing namespaces, including the case where
// the user is not allowed to list them at all — a common, entirely normal
// situation for scoped service accounts that Correlux must handle without
// pretending the cluster is empty.
type NamespaceList struct {
	// Names are the namespaces that were returned, sorted.
	Names []string
	// Restricted is true when listing was denied, meaning Names is not the
	// full picture and the user should type a namespace instead.
	Restricted bool
	// Truncated is true when the server had more namespaces than we fetched.
	Truncated bool
}

// namespaceListLimit bounds the first page. Clusters with thousands of
// namespaces exist; the picker pages rather than loading everything.
const namespaceListLimit = 500

// ListNamespaces returns the namespaces visible in a context.
//
// A Forbidden response is not an error: it is reported through
// NamespaceList.Restricted so the UI can offer manual entry.
func (f *Factory) ListNamespaces(ctx context.Context, contextName string) (NamespaceList, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return NamespaceList{}, err
	}
	return listNamespaces(ctx, cs)
}

// listNamespaces is separated from the factory so it can be exercised against a
// fake clientset without constructing a kubeconfig.
func listNamespaces(ctx context.Context, cs kubernetes.Interface) (NamespaceList, error) {
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: namespaceListLimit})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return NamespaceList{Restricted: true}, nil
		}
		return NamespaceList{}, err
	}
	out := NamespaceList{
		Names:     make([]string, 0, len(list.Items)),
		Truncated: list.Continue != "",
	}
	// Index rather than range: a Namespace value is several hundred bytes and
	// this list can be thousands of entries long.
	for i := range list.Items {
		out.Names = append(out.Names, list.Items[i].Name)
	}
	sort.Strings(out.Names)
	return out, nil
}
