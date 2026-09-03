package describe

import (
	"strings"
	"testing"
)

// find returns one section by title.
func find(t *testing.T, sections []Section, title string) Section {
	t.Helper()
	for _, s := range sections {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("no section %q; got %v", title, titles(sections))
	return Section{}
}

func titles(sections []Section) []string {
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		out = append(out, s.Title)
	}
	return out
}

// cell finds a row by its first column and returns the column asked for.
func cell(t *testing.T, s Section, key string, column int) string {
	t.Helper()
	for _, row := range s.Rows {
		if len(row) > column && row[0] == key {
			return row[column]
		}
	}
	t.Fatalf("no row %q in section %q: %v", key, s.Title, s.Rows)
	return ""
}

const pod = `{
  "kind": "Pod",
  "metadata": {"name": "payments-7d8f-0", "namespace": "shop"},
  "spec": {
    "nodeName": "node-1",
    "serviceAccountName": "payments",
    "containers": [
      {"name": "payments", "image": "registry/payments:1.4",
	   "envFrom": [{"secretRef": {"name": "payments-env"}}],
       "resources": {"requests": {"cpu": "100m", "memory": "128Mi"}, "limits": {"memory": "256Mi"}}}
    ],
    "initContainers": [{"name": "migrate", "image": "registry/migrate:3"}],
    "volumes": [
      {"name": "data", "persistentVolumeClaim": {"claimName": "payments-data"}},
      {"name": "config", "configMap": {"name": "payments-config"}}
    ]
  },
  "status": {
    "phase": "Running",
    "podIP": "10.244.0.5",
    "qosClass": "Burstable",
    "containerStatuses": [
      {"name": "payments", "ready": false, "restartCount": 12,
       "state": {"waiting": {"reason": "CrashLoopBackOff"}}}
    ],
    "initContainerStatuses": [
      {"name": "migrate", "ready": true, "restartCount": 0,
       "state": {"terminated": {"reason": "Completed", "exitCode": 0}}}
    ],
    "conditions": [
      {"type": "Ready", "status": "False", "reason": "ContainersNotReady",
       "message": "containers with unready status: [payments]"}
    ]
  }
}`

func TestAPodIsDescribedByItsContainers(t *testing.T) {
	sections := Object("Pod", []byte(pod))

	status := find(t, sections, "Status")
	if got := cell(t, status, "Phase", 1); got != "Running" {
		t.Errorf("phase = %q", got)
	}
	if got := cell(t, status, "QoS", 1); got != "Burstable" {
		t.Errorf("QoS = %q", got)
	}

	containers := find(t, sections, "Containers")
	if len(containers.Rows) != 2 {
		t.Fatalf("both the init container and the container belong here: %v", containers.Rows)
	}
	// Init containers come first, because while one is stuck nothing else runs.
	if containers.Rows[0][0] != "init migrate" {
		t.Errorf("first row = %v, want the init container", containers.Rows[0])
	}
	app := containers.Rows[1]
	if app[2] != "waiting: CrashLoopBackOff" {
		t.Errorf("state = %q, want the reason next to the phase", app[2])
	}
	if app[4] != "12" {
		t.Errorf("restarts = %q", app[4])
	}
	if !strings.Contains(app[5], "cpu=100m") || !strings.Contains(app[5], "memory=128Mi") {
		t.Errorf("requests = %q", app[5])
	}
	if app[6] != "memory=256Mi" {
		t.Errorf("limits = %q", app[6])
	}

	volumes := find(t, sections, "Volumes")
	if got := cell(t, volumes, "data", 1); got != "claim payments-data" {
		t.Errorf("volume source = %q, want the claim it mounts", got)
	}

	conditions := find(t, sections, "Conditions")
	if got := cell(t, conditions, "Ready", 3); !strings.Contains(got, "unready status") {
		t.Errorf("the condition's message must survive, got %q", got)
	}
}

func TestAPodLinksToMountedObjects(t *testing.T) {
	links := Links("Pod", []byte(pod))
	want := map[string]string{
		"ServiceAccount/payments":             "used by pod",
		"PersistentVolumeClaim/payments-data": "mounted as data",
		"ConfigMap/payments-config":           "mounted as config",
		"Secret/payments-env":                 "environment for payments",
	}
	for _, link := range links {
		key := link.Kind + "/" + link.Name
		if detail, ok := want[key]; ok && link.Detail == detail {
			delete(want, key)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing links: %v; got %+v", want, links)
	}
}

func TestAClaimLinksToItsVolumeAndStorageClass(t *testing.T) {
	raw := []byte(`{"kind":"PersistentVolumeClaim","metadata":{"namespace":"shop"},"spec":{"volumeName":"pvc-123","storageClassName":"fast"}}`)
	links := Links("PersistentVolumeClaim", raw)
	if len(links) != 2 || links[0].Kind != "PersistentVolume" || links[1].Kind != "StorageClass" {
		t.Fatalf("links = %+v", links)
	}
	if links[0].Namespace != "" || links[1].Namespace != "" {
		t.Error("cluster-scoped storage objects must not inherit the claim namespace")
	}
}

const deployment = `{
  "kind": "Deployment",
  "metadata": {"name": "payments", "namespace": "shop", "generation": 7},
  "spec": {
    "replicas": 3,
    "paused": true,
    "strategy": {"type": "RollingUpdate"},
    "selector": {"matchLabels": {"app": "payments"},
                 "matchExpressions": [{"key": "tier", "operator": "In", "values": ["web"]}]},
    "template": {"spec": {"containers": [
      {"name": "payments", "image": "registry/payments:1.4", "ports": [{"containerPort": 8080}]}
    ]}}
  },
  "status": {"readyReplicas": 1, "updatedReplicas": 3, "availableReplicas": 1}
}`

func TestAWorkloadIsDescribedByItsReplicasAndTemplate(t *testing.T) {
	sections := Object("Deployment", []byte(deployment))

	status := find(t, sections, "Status")
	if got := cell(t, status, "Replicas", 1); got != "3" {
		t.Errorf("replicas = %q", got)
	}
	if got := cell(t, status, "Ready", 1); got != "1" {
		t.Errorf("ready = %q", got)
	}
	if got := cell(t, status, "Paused", 1); got != "yes" {
		t.Errorf("a paused rollout must be stated, got %q", got)
	}
	if got := cell(t, status, "Selector", 1); !strings.Contains(got, "app=payments") || !strings.Contains(got, "tier in") {
		t.Errorf("selector = %q, want both the labels and the expression", got)
	}

	template := find(t, sections, "Pod template")
	if len(template.Rows) != 1 || template.Rows[0][1] != "registry/payments:1.4" {
		t.Errorf("the image everyone looks for must be here: %v", template.Rows)
	}
	if template.Rows[0][2] != "8080" {
		t.Errorf("ports = %q", template.Rows[0][2])
	}
}

func TestAServiceIsDescribedByItsPortsAndSelector(t *testing.T) {
	sections := Object("Service", []byte(`{
      "kind": "Service",
      "spec": {"type": "ClusterIP", "clusterIP": "10.96.0.10", "selector": {"app": "payments"},
               "ports": [{"name": "http", "port": 80, "targetPort": 8080, "protocol": "TCP"}]}
    }`))

	status := find(t, sections, "Status")
	if got := cell(t, status, "Selector", 1); got != "app=payments" {
		t.Errorf("selector = %q", got)
	}
	ports := find(t, sections, "Ports")
	if len(ports.Rows) != 1 || ports.Rows[0][2] != "8080" {
		t.Errorf("the target port is what a request actually reaches: %v", ports.Rows)
	}
}

func TestAnIngressIsDescribedByItsRules(t *testing.T) {
	sections := Object("Ingress", []byte(`{
      "kind": "Ingress",
      "spec": {"ingressClassName": "nginx", "rules": [
        {"host": "payments.example.com", "http": {"paths": [
          {"path": "/", "backend": {"service": {"name": "payments", "port": {"number": 80}}}}]}}]},
      "status": {"loadBalancer": {"ingress": [{"ip": "203.0.113.7"}]}}
    }`))

	rules := find(t, sections, "Rules")
	if len(rules.Rows) != 1 {
		t.Fatalf("rules = %v", rules.Rows)
	}
	if rules.Rows[0][0] != "payments.example.com" || rules.Rows[0][2] != "payments:80" {
		t.Errorf("rule = %v, want host and backend", rules.Rows[0])
	}
	if got := cell(t, find(t, sections, "Status"), "Address", 1); got != "203.0.113.7" {
		t.Errorf("address = %q", got)
	}
}

func TestACronJobIsDescribedByItsSchedule(t *testing.T) {
	sections := Object("CronJob", []byte(`{
      "kind": "CronJob",
      "spec": {"schedule": "0 3 * * *", "suspend": true,
               "jobTemplate": {"spec": {"template": {"spec": {"containers": [
                 {"name": "billing", "image": "registry/billing:2"}]}}}}}
    }`))

	status := find(t, sections, "Status")
	if got := cell(t, status, "Schedule", 1); got != "0 3 * * *" {
		t.Errorf("schedule = %q", got)
	}
	if got := cell(t, status, "Suspended", 1); got != "yes" {
		t.Errorf("suspended = %q", got)
	}
	if template := find(t, sections, "Pod template"); len(template.Rows) != 1 {
		t.Errorf("the job's template must be reached through jobTemplate: %v", template.Rows)
	}
}

func TestAnUnknownKindIsStillDescribed(t *testing.T) {
	// A custom resource Correlux has never heard of: its status is what its
	// author wanted anybody to see.
	sections := Object("Widget", []byte(`{
      "kind": "Widget",
      "status": {"phase": "Ready", "size": 42, "replicas": {"a": 1},
                 "conditions": [{"type": "Available", "status": "True", "reason": "Provisioned"}]}
    }`))

	status := find(t, sections, "Status")
	if got := cell(t, status, "phase", 1); got != "Ready" {
		t.Errorf("phase = %q", got)
	}
	if got := cell(t, status, "size", 1); got != "42" {
		t.Errorf("size = %q", got)
	}
	if got := cell(t, find(t, sections, "Conditions"), "Available", 2); got != "Provisioned" {
		t.Errorf("a custom resource's conditions are read like any other, got %q", got)
	}
}

func TestAMalformedObjectDescribesWhatItCan(t *testing.T) {
	// Every field is the wrong type. Nothing here may panic.
	sections := Object("Pod", []byte(`{
      "kind": "Pod",
      "spec": {"containers": "not a list", "nodeName": 7},
      "status": {"conditions": [42, {"type": "Ready"}], "containerStatuses": {"not": "a list"}}
    }`))

	if len(sections) == 0 {
		t.Fatal("a malformed object must still produce something")
	}
	containers := find(t, sections, "Containers")
	if len(containers.Rows) != 0 {
		t.Errorf("nothing can be read from a string where a list belongs: %v", containers.Rows)
	}
}

func TestUndecodableInputIsNotADescription(t *testing.T) {
	if sections := Object("Pod", []byte("this is not JSON")); sections != nil {
		t.Errorf("got %v, want nothing at all", titles(sections))
	}
}
