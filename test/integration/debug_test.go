//go:build integration

package integration

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aronk11/correlux/internal/kube/debug"
)

func TestTroubleshootingJobsPassServerValidation(t *testing.T) {
	cs, err := shared.factory.Clientset(shared.context)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range [][2]string{{"toolbox", ""}, {"dns", "kubernetes.default.svc"}, {"tcp", "kubernetes.default.svc:443"}, {"http", "https://kubernetes.default.svc"}} {
		job, buildErr := debug.Job(debug.Options{Namespace: "default", Image: "busybox:1.37.0", Mode: entry[0], Destination: entry[1], Seconds: 60})
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		created, createErr := cs.BatchV1().Jobs("default").Create(ctx(t), job, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		if createErr != nil {
			t.Fatalf("%s job failed API validation: %v", entry[0], createErr)
		}
		if created.Spec.ActiveDeadlineSeconds == nil || created.Spec.TTLSecondsAfterFinished == nil {
			t.Fatal("server discarded lifecycle limits")
		}
	}
}
