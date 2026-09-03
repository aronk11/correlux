package podexec

import (
	"strings"
	"testing"

	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
)

// coreClient builds a real CoreV1 REST client against a host nothing actually
// answers on. Building a client and the request it would send makes no
// network call, which is all these tests check: client-go's fake clientset
// cannot stand in here because its RESTClient() is a deliberate nil stub — it
// has no notion of the exec subresource at all.
func coreClient(t *testing.T) restclient.Interface {
	t.Helper()
	cfg := &restclient.Config{Host: "https://api.example.com"}
	core, err := corev1client.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("build core client: %v", err)
	}
	return core.RESTClient()
}

func TestLabelNamesContainerOnlyWhenKnown(t *testing.T) {
	bare := Target{Pod: "web-1"}
	if got := bare.Label(); got != "web-1" {
		t.Errorf("Label() = %q, want the bare pod name", got)
	}

	named := Target{Pod: "web-1", Container: "app"}
	if got := named.Label(); got != "web-1/app" {
		t.Errorf("Label() = %q, want %q", got, "web-1/app")
	}
}

func TestDefaultShellCommandFallsBackFromBashToSh(t *testing.T) {
	if len(DefaultShellCommand) != 3 {
		t.Fatalf("DefaultShellCommand = %v, want a 3-element sh -c command", DefaultShellCommand)
	}
	script := DefaultShellCommand[2]
	if !strings.Contains(script, "bash") || !strings.Contains(script, "sh") {
		t.Errorf("script %q must try bash and fall back to sh", script)
	}
}

func TestRequestTargetsTheExecSubresourceWithTheGivenContainerAndCommand(t *testing.T) {
	target := Target{Namespace: "payments", Pod: "web-1", Container: "app"}

	req := request(coreClient(t), target, []string{"/bin/sh"})
	url := req.URL().String()

	for _, part := range []string{
		"/namespaces/payments/pods/web-1/exec",
		"container=app",
		"command=%2Fbin%2Fsh",
		"stdin=true",
		"stdout=true",
		"stderr=true",
		"tty=true",
	} {
		if !strings.Contains(url, part) {
			t.Errorf("request URL %q does not contain %q", url, part)
		}
	}
}

func TestRequestLeavesTheContainerToTheServerWhenNoneIsNamed(t *testing.T) {
	target := Target{Namespace: "payments", Pod: "web-1"}

	url := request(coreClient(t), target, DefaultShellCommand).URL().String()
	if strings.Contains(url, "container=") {
		t.Errorf("request URL %q names a container nobody chose", url)
	}
}
