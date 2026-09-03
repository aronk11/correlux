package client

import (
	"strings"
	"testing"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// notCompiledIn is the message client-go produces for a provider no binary
// registered. It names the binary rather than the cluster, and no amount of
// fixing the kubeconfig makes it go away — which is exactly why it must never
// be what a user sees for a provider Kubernetes still ships.
const notCompiledIn = "no Auth Provider found for name"

func TestOIDCContextsAreNotRejectedForLackOfAProvider(t *testing.T) {
	_, err := rest.GetAuthProvider("https://cluster.example", &clientcmdapi.AuthProviderConfig{
		Name: "oidc",
		Config: map[string]string{
			"idp-issuer-url": "https://issuer.example",
			"client-id":      "correlux",
			"id-token":       "not-a-real-token",
		},
	}, nil)
	if err != nil && strings.Contains(err.Error(), notCompiledIn) {
		t.Fatalf("kubeconfigs authenticating through OIDC must work: %v", err)
	}
}

// A provider Kubernetes itself removed must still fail — but with its own
// explanation of what to install instead, not with the message above.
func TestRemovedCloudProvidersExplainThemselves(t *testing.T) {
	for _, name := range []string{"gcp", "azure"} {
		t.Run(name, func(t *testing.T) {
			_, err := rest.GetAuthProvider("https://cluster.example",
				&clientcmdapi.AuthProviderConfig{Name: name}, nil)
			if err == nil {
				t.Skipf("%s still works in this client-go", name)
			}
			if strings.Contains(err.Error(), notCompiledIn) {
				t.Errorf("%s must say what to use instead, got %q", name, err)
			}
		})
	}
}

// The guarantee above is worth nothing if every name passes.
func TestAnUnknownProviderIsStillAnError(t *testing.T) {
	_, err := rest.GetAuthProvider("https://cluster.example",
		&clientcmdapi.AuthProviderConfig{Name: "not-a-provider"}, nil)
	if err == nil {
		t.Fatal("an unknown auth provider must be reported, not accepted")
	}
	if !strings.Contains(err.Error(), notCompiledIn) {
		t.Errorf("unexpected error for an unknown provider: %v", err)
	}
}
