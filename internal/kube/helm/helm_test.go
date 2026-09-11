package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestRunPinsContextAndRemovesPrivateCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	// The fixture prints only arguments and verifies the temporary file mode;
	// no credentials are emitted or persisted in test output.
	script := `#!/bin/sh
printf '%s\n' "$@"
if [ -n "$HELM_KUBETOKEN" ]; then exit 8; fi
if [ ! -f "$2" ]; then exit 9; fi
`
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HELM_KUBETOKEN", "unexpected-override")
	raw := clientcmdapi.Config{Contexts: map[string]*clientcmdapi.Context{"selected": {Cluster: "selected", AuthInfo: "user"}, "other": {Cluster: "other"}}, Clusters: map[string]*clientcmdapi.Cluster{"selected": {Server: "https://selected.invalid"}, "other": {Server: "https://other.invalid"}}, AuthInfos: map[string]*clientcmdapi.AuthInfo{"user": {}}, CurrentContext: "other"}
	before, _ := json.Marshal(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := Run(ctx, raw, "selected", "team", []string{"status", "api", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(args) != 10 || args[2] != "--kube-context" || args[3] != "selected" || args[5] != "team" || args[7] != "api" {
		t.Fatalf("incorrect argument vector: %v", args)
	}
	if _, err = os.Stat(args[1]); !os.IsNotExist(err) {
		t.Fatal("temporary credentials were not removed")
	}
	after, _ := json.Marshal(raw)
	if !bytes.Equal(before, after) {
		t.Fatal("mutated the original kubeconfig")
	}
}

func TestOutputIsBounded(t *testing.T) {
	var b boundedBuffer
	if _, err := b.Write(make([]byte, maxOutput+1)); err == nil {
		t.Fatal("accepted oversized output")
	}
}
