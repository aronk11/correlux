package describe

import (
	"strings"
	"testing"
)

func rowsOf(t *testing.T, sections []Section, title string) [][]string {
	t.Helper()
	for _, s := range sections {
		if s.Title == title {
			return s.Rows
		}
	}
	t.Fatalf("no %q section in %+v", title, sections)
	return nil
}

func joined(rows [][]string) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, strings.Join(r, " | "))
	}
	return strings.Join(parts, "\n")
}

func TestAConfigMapShowsEveryKeyWithItsValue(t *testing.T) {
	doc := `{"kind":"ConfigMap","data":{"LOG_LEVEL":"debug","app.yaml":"server:\n  port: 8080\n  host: 0.0.0.0\n"},"binaryData":{"logo.png":"iVBORw0KGgo="}}`
	rows := joined(rowsOf(t, Object("ConfigMap", []byte(doc)), "Data"))
	for _, want := range []string{"LOG_LEVEL | 5 B | debug", "app.yaml", "server:  … (3 lines)", "logo.png | 8 B | binary"} {
		if !strings.Contains(rows, want) {
			t.Errorf("missing %q in:\n%s", want, rows)
		}
	}
}

func TestASecretIsMaskedUnlessRevealed(t *testing.T) {
	doc := `{"kind":"Secret","type":"Opaque","data":{"password":"aHVudGVyMg==","blob":"AAEC"}}`
	masked := joined(rowsOf(t, Object("Secret", []byte(doc)), "Data"))
	if strings.Contains(masked, "hunter2") || !strings.Contains(masked, "password | 7 B | ••••••") {
		t.Errorf("masked:\n%s", masked)
	}
	shown := joined(rowsOf(t, ObjectWith("Secret", []byte(doc), Options{Reveal: true}), "Data"))
	if !strings.Contains(shown, "password | 7 B | hunter2") || !strings.Contains(shown, "blob | 3 B | binary") {
		t.Errorf("revealed:\n%s", shown)
	}
}

func TestRBACShowsRulesAndSubjects(t *testing.T) {
	role := `{"kind":"ClusterRole","rules":[{"apiGroups":[""],"resources":["pods","pods/log"],"verbs":["get","list"]},{"nonResourceURLs":["/healthz"],"verbs":["get"]}]}`
	rules := joined(rowsOf(t, Object("ClusterRole", []byte(role)), "Rules"))
	if !strings.Contains(rules, "core | pods, pods/log | get, list") || !strings.Contains(rules, "URLs /healthz") {
		t.Errorf("rules:\n%s", rules)
	}
	binding := `{"kind":"RoleBinding","roleRef":{"kind":"Role","name":"reader"},"subjects":[{"kind":"ServiceAccount","name":"ci","namespace":"build"}]}`
	sections := Object("RoleBinding", []byte(binding))
	if !strings.Contains(joined(rowsOf(t, sections, "Status")), "Role/reader") ||
		!strings.Contains(joined(rowsOf(t, sections, "Subjects")), "ServiceAccount | ci | build") {
		t.Errorf("binding: %+v", sections)
	}
}

func TestANetworkPolicySaysWhatItAllowsAndWhatItDenies(t *testing.T) {
	doc := `{"kind":"NetworkPolicy","spec":{"podSelector":{"matchLabels":{"app":"payments"}},"policyTypes":["Ingress","Egress"],
		"ingress":[{"from":[{"namespaceSelector":{"matchLabels":{"team":"edge"}},"podSelector":{"matchLabels":{"app":"ingress"}}}],"ports":[{"port":8080}]}]}}`
	sections := Object("NetworkPolicy", []byte(doc))
	in := joined(rowsOf(t, sections, "Allowed in"))
	if !strings.Contains(in, "namespaces team=edge in pods app=ingress | 8080/TCP") {
		t.Errorf("allowed in:\n%s", in)
	}
	for _, s := range sections {
		if s.Title == "Allowed out" && (len(s.Rows) != 0 || !strings.Contains(s.Empty, "denied")) {
			t.Errorf("egress with no rules is a deny-all: %+v", s)
		}
	}
}

func TestAnAutoscalerShowsItsMetricsAgainstTheirTargets(t *testing.T) {
	doc := `{"kind":"HorizontalPodAutoscaler","spec":{"scaleTargetRef":{"kind":"Deployment","name":"payments"},"minReplicas":2,"maxReplicas":10,
		"metrics":[{"type":"Resource","resource":{"name":"cpu","target":{"type":"Utilization","averageUtilization":70}}}]},
		"status":{"currentReplicas":4,"desiredReplicas":5,"currentMetrics":[{"type":"Resource","resource":{"name":"cpu","current":{"averageUtilization":91}}}]}}`
	sections := Object("HorizontalPodAutoscaler", []byte(doc))
	if m := joined(rowsOf(t, sections, "Metrics")); m != "cpu | 70% | 91%" {
		t.Errorf("metrics = %q", m)
	}
	if s := joined(rowsOf(t, sections, "Status")); !strings.Contains(s, "Deployment/payments") || !strings.Contains(s, "2–10") {
		t.Errorf("status:\n%s", s)
	}
}

func TestStorageQuotaAndSliceKindsAreDescribed(t *testing.T) {
	cases := map[string]struct{ doc, section, want string }{
		"PodDisruptionBudget":      {`{"spec":{"minAvailable":1,"selector":{"matchLabels":{"app":"x"}}},"status":{"currentHealthy":1,"desiredHealthy":1,"disruptionsAllowed":0}}`, "Status", "Disruptions allowed | 0"},
		"PersistentVolume":         {`{"spec":{"capacity":{"storage":"10Gi"},"accessModes":["ReadWriteOnce"],"csi":{"driver":"ebs.csi.aws.com","volumeHandle":"vol-1"},"claimRef":{"namespace":"shop","name":"data"}},"status":{"phase":"Bound"}}`, "Status", "CSI driver | ebs.csi.aws.com"},
		"StorageClass":             {`{"provisioner":"ebs.csi.aws.com","volumeBindingMode":"WaitForFirstConsumer","parameters":{"type":"gp3"}}`, "Parameters", "type | gp3"},
		"ResourceQuota":            {`{"status":{"hard":{"pods":"10"},"used":{"pods":"10"}}}`, "Usage", "pods | 10 | 10"},
		"LimitRange":               {`{"spec":{"limits":[{"type":"Container","max":{"cpu":"2"},"defaultRequest":{"cpu":"100m"}}]}}`, "Limits", "Container | cpu | — | 2 | 100m | —"},
		"EndpointSlice":            {`{"addressType":"IPv4","endpoints":[{"addresses":["10.0.0.7"],"conditions":{"ready":false},"targetRef":{"name":"payments-1"}}]}`, "Endpoints", "10.0.0.7 | no | — | — | payments-1"},
		"CustomResourceDefinition": {`{"spec":{"group":"example.com","scope":"Namespaced","names":{"kind":"Widget"},"versions":[{"name":"v1","served":true,"storage":true}]}}`, "Versions", "v1 | yes | yes | no"},
		"ServiceAccount":           {`{"automountServiceAccountToken":false,"imagePullSecrets":[{"name":"regcred"}]}`, "Status", "Image pull secrets | regcred"},
	}
	for kind, c := range cases {
		if got := joined(rowsOf(t, Object(kind, []byte(c.doc)), c.section)); !strings.Contains(got, c.want) {
			t.Errorf("%s %s: want %q in\n%s", kind, c.section, c.want, got)
		}
	}
}

func TestAnUnknownKindWithoutStatusFallsBackToItsSpec(t *testing.T) {
	doc := `{"kind":"Widget","spec":{"replicas":3,"image":{"repository":"example/widget","tag":"1.2"}}}`
	rows := joined(rowsOf(t, Object("Widget", []byte(doc)), "Spec"))
	if !strings.Contains(rows, "image.repository | example/widget") || !strings.Contains(rows, "replicas | 3") {
		t.Errorf("spec:\n%s", rows)
	}
}

func TestEveryDescriberSurvivesStrangeDocuments(t *testing.T) {
	kinds := []string{"ConfigMap", "Secret", "ServiceAccount", "Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding",
		"NetworkPolicy", "HorizontalPodAutoscaler", "PodDisruptionBudget", "PersistentVolume", "StorageClass", "Namespace",
		"ResourceQuota", "LimitRange", "EndpointSlice", "CustomResourceDefinition", "Widget"}
	docs := []string{`{}`, `{"data":"x","spec":[],"status":7,"rules":[1,"x",null],"metadata":{"labels":5}}`,
		`{"spec":{"metrics":[{"type":""},{"type":"Pods","pods":{"target":{}}}],"limits":[{"type":"Pod","max":3}]}}`}
	for _, kind := range kinds {
		for _, doc := range docs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s with %s panicked: %v", kind, doc, r)
					}
				}()
				ObjectWith(kind, []byte(doc), Options{Reveal: true})
			}()
		}
	}
}

func TestAKindWithTopLevelFieldsShowsThem(t *testing.T) {
	webhook := `{"kind":"ValidatingWebhookConfiguration","webhooks":[{"name":"policy.example.com"},{"name":"quota.example.com"}]}`
	if rows := joined(rowsOf(t, Object("ValidatingWebhookConfiguration", []byte(webhook)), "Fields")); !strings.Contains(rows, "webhooks | policy.example.com, quota.example.com") {
		t.Errorf("webhooks:\n%s", rows)
	}
	priority := `{"kind":"PriorityClass","value":1000000,"globalDefault":false,"description":"critical workloads"}`
	if rows := joined(rowsOf(t, Object("PriorityClass", []byte(priority)), "Fields")); !strings.Contains(rows, "value | 1000000") {
		t.Errorf("priority:\n%s", rows)
	}
}

func TestAMultiLineValueIsWrittenOutInFull(t *testing.T) {
	doc := `{"kind":"ConfigMap","data":{"app.yaml":"server:\n  port: 8080\n","flag":"on"}}`
	rows := joined(rowsOf(t, Object("ConfigMap", []byte(doc)), "app.yaml"))
	if rows != "server:\n  port: 8080" {
		t.Errorf("content = %q", rows)
	}
	secret := `{"kind":"Secret","data":{"key.pem":"LS0tLS1CRUdJTgpBQkMKLS0tLS1FTkQ="}}`
	for _, s := range Object("Secret", []byte(secret)) {
		if s.Title == "key.pem" {
			t.Fatal("a masked secret must not be written out")
		}
	}
	if rows := joined(rowsOf(t, ObjectWith("Secret", []byte(secret), Options{Reveal: true}), "key.pem")); !strings.Contains(rows, "ABC") {
		t.Errorf("revealed content = %q", rows)
	}
}
