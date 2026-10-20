package debug

import (
	"strings"
	"testing"
)

func TestJobHasIndependentIdentityAndServerSideCleanup(t *testing.T) {
	job, err := Job(Options{Namespace: "team", Image: "example/toolbox@sha256:abc", Mode: "toolbox", Seconds: 900, ImagePullSecrets: []string{"registry"}})
	if err != nil {
		t.Fatal(err)
	}
	spec := job.Spec.Template.Spec
	if *job.Spec.ActiveDeadlineSeconds != 900 || *job.Spec.TTLSecondsAfterFinished != 600 || *job.Spec.BackoffLimit != 0 {
		t.Fatal("missing bounded lifecycle")
	}
	if *spec.AutomountServiceAccountToken || !*spec.Containers[0].SecurityContext.RunAsNonRoot || *spec.Containers[0].SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("unsafe defaults")
	}
	if len(job.Spec.Template.Labels) != 1 || job.Spec.Template.Labels[Label] != "true" || spec.HostNetwork || len(spec.Volumes) > 0 {
		t.Fatal("copied application identity or host access")
	}
	if len(spec.ImagePullSecrets) != 1 || spec.ImagePullSecrets[0].Name != "registry" {
		t.Fatal("lost pull credentials")
	}
}

func TestProbeDestinationsAreArgumentsNotPrograms(t *testing.T) {
	cmd, err := Command("http", "https://example.test/$(touch%20/tmp/test)", 60)
	if err != nil {
		t.Fatal(err)
	}
	if cmd[0] != "curl" || cmd[len(cmd)-2] != "--" || strings.Contains(strings.Join(cmd[:2], " "), "sh") {
		t.Fatal(cmd)
	}
	for _, tt := range [][2]string{{"http", "file:///etc/passwd"}, {"http", "https://user:password@example.test"}, {"dns", "-debug"}, {"dns", "name\nother"}, {"tcp", "host:70000"}, {"tcp", "-host:80"}, {"unknown", "anything"}} {
		if _, err := Command(tt[0], tt[1], 60); err == nil {
			t.Fatalf("accepted %v", tt)
		}
	}
	if _, err := Command("tcp", "[::1]:443", 60); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidDebugSpecsNeverReachCreate(t *testing.T) {
	for _, opts := range []Options{{Namespace: "", Image: "busybox", Mode: "toolbox", Seconds: 900}, {Namespace: "team", Image: "", Mode: "toolbox", Seconds: 900}, {Namespace: "team", Image: "busybox", Mode: "toolbox", Seconds: 3601}} {
		if _, err := Job(opts); err == nil {
			t.Fatalf("accepted invalid options: %+v", opts)
		}
	}
}

func TestJobPreservesExplicitPullPolicy(t *testing.T) {
	for _, value := range []string{"", "Always", "IfNotPresent", "Never"} {
		policy, err := PullPolicy(value)
		if err != nil {
			t.Fatal(err)
		}
		job, err := Job(Options{Namespace: "team", Image: "registry.internal/toolbox:latest", Mode: "toolbox", Seconds: 900, ImagePullPolicy: policy})
		if err != nil {
			t.Fatal(err)
		}
		if string(job.Spec.Template.Spec.Containers[0].ImagePullPolicy) != value {
			t.Fatal("pull policy lost")
		}
	}
	if _, err := PullPolicy("never"); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if _, err := Job(Options{Namespace: "team", Image: "toolbox", Mode: "toolbox", Seconds: 900, ImagePullPolicy: "invalid"}); err == nil {
		t.Fatal("invalid job policy accepted")
	}
}

func TestTLSInspectionNeedsHTTPSAndPrintsTheCertificate(t *testing.T) {
	if _, err := Command("tls", "http://example.com", 0); err == nil {
		t.Fatal("plain HTTP has no certificate to inspect")
	}
	cmd, err := Command("tls", "https://example.com", 0)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd, " ")
	if !strings.Contains(joined, "--verbose") || strings.Contains(joined, "--insecure") || cmd[len(cmd)-1] != "https://example.com" {
		t.Errorf("command = %q", joined)
	}
}

func TestTheNodeJobIsPrivilegedOnPurposeAndPinnedToItsNode(t *testing.T) {
	job, err := NodeJob(NodeOptions{Node: "node-1", Namespace: "kube-system", Image: "busybox:1.37.0", Seconds: 1800})
	if err != nil {
		t.Fatal(err)
	}
	spec := job.Spec.Template.Spec
	c := spec.Containers[0]
	if spec.NodeName != "node-1" || !spec.HostPID || !spec.HostNetwork || c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
		t.Errorf("node pod is not what it says: %+v", spec)
	}
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Error("the node pod must not carry a service-account token")
	}
	if len(spec.Tolerations) != 1 || spec.Tolerations[0].Operator != "Exists" {
		t.Error("an unhealthy node is tainted; the pod must tolerate it")
	}
	if c.VolumeMounts[0].MountPath != "/host" || spec.Volumes[0].HostPath.Path != "/" {
		t.Error("the host filesystem must be at /host")
	}
	if *job.Spec.ActiveDeadlineSeconds != 1800 || job.Annotations["correlux.dev/debug-mode"] != "node" {
		t.Error("deadline or mode annotation missing")
	}
	if _, err := NodeJob(NodeOptions{Node: "Not A Node", Namespace: "kube-system", Image: "busybox", Seconds: 10}); err == nil {
		t.Error("invalid node names must be refused")
	}
}
