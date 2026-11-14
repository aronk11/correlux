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
	if app[5] != "12" {
		t.Errorf("restarts = %q", app[5])
	}
	if !strings.Contains(app[6], "cpu=100m") || !strings.Contains(app[6], "memory=128Mi") {
		t.Errorf("requests = %q", app[6])
	}
	if app[7] != "memory=256Mi" {
		t.Errorf("limits = %q", app[7])
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

func fieldsOf(t *testing.T, sections []Section, title string) map[string]string {
	t.Helper()
	for _, s := range sections {
		if s.Title != title {
			continue
		}
		out := map[string]string{}
		for _, r := range s.Rows {
			if len(r) >= 2 {
				out[r[0]] = r[len(r)-1]
			}
		}
		return out
	}
	t.Fatalf("no section %q", title)
	return nil
}

func TestAZeroThatIsTheNewsIsNotDropped(t *testing.T) {
	deployment := Object("Deployment", []byte(`{"kind":"Deployment","spec":{"replicas":3},"status":{"readyReplicas":0,"updatedReplicas":3}}`))
	if got := fieldsOf(t, deployment, "Status"); got["Ready"] != "0" || got["Available"] != "0" {
		t.Errorf("a Deployment with nothing ready must say so: %v", got)
	}

	budget := Object("PodDisruptionBudget", []byte(`{"kind":"PodDisruptionBudget","spec":{"maxUnavailable":0,"selector":{"matchLabels":{"app":"x"}}},"status":{}}`))
	if got := fieldsOf(t, budget, "Status"); got["Max unavailable"] != "0" {
		t.Errorf("maxUnavailable 0 is why a drain hangs and must be shown: %v", got)
	}

	hpa := Object("HorizontalPodAutoscaler", []byte(`{"kind":"HorizontalPodAutoscaler","spec":{"minReplicas":0,"maxReplicas":5,"scaleTargetRef":{"kind":"Deployment","name":"x"}},"status":{}}`))
	if got := fieldsOf(t, hpa, "Status"); !strings.HasPrefix(got["Range"], "0") {
		t.Errorf("an explicit minReplicas of 0 is scale-to-zero: %v", got)
	}
}

func TestAFailedPodSaysWhyAndHowItsLastRunEnded(t *testing.T) {
	sections := Object("Pod", []byte(`{"kind":"Pod","spec":{"containers":[{"name":"app","image":"x"}]},
		"status":{"phase":"Failed","reason":"Evicted","message":"The node was low on resource: memory.",
		"containerStatuses":[{"name":"app","restartCount":4,"state":{"waiting":{"reason":"CrashLoopBackOff"}},
		"lastState":{"terminated":{"reason":"OOMKilled","exitCode":137}}}]}}`))
	status := fieldsOf(t, sections, "Status")
	if status["Reason"] != "Evicted" || !strings.Contains(status["Message"], "low on resource") {
		t.Errorf("status = %v", status)
	}
	for _, s := range sections {
		if s.Title == "Containers" && s.Rows[0][3] != "OOMKilled exit 137" {
			t.Errorf("last run = %q", s.Rows[0][3])
		}
	}
}

func TestIngressBackendsKeepNamedPortsAndTheDefault(t *testing.T) {
	sections := Object("Ingress", []byte(`{"kind":"Ingress","spec":{"defaultBackend":{"service":{"name":"fallback","port":{"number":80}}},
		"rules":[{"host":"a.example","http":{"paths":[{"path":"/","backend":{"service":{"name":"web","port":{"name":"http"}}}}]}}]}}`))
	for _, s := range sections {
		if s.Title != "Rules" {
			continue
		}
		if len(s.Rows) != 2 || s.Rows[0][2] != "fallback:80" || s.Rows[1][2] != "web:http" {
			t.Errorf("rules = %v", s.Rows)
		}
	}
}

func TestALoadBalancerWithoutAnAddressSaysItIsPending(t *testing.T) {
	pending := Object("Service", []byte(`{"kind":"Service","spec":{"type":"LoadBalancer"},"status":{"loadBalancer":{}}}`))
	if got := fieldsOf(t, pending, "Status"); !strings.Contains(got["External address"], "pending") {
		t.Errorf("status = %v", got)
	}
	ready := Object("Service", []byte(`{"kind":"Service","spec":{"type":"LoadBalancer"},"status":{"loadBalancer":{"ingress":[{"hostname":"lb.example"}]}}}`))
	if got := fieldsOf(t, ready, "Status"); got["External address"] != "lb.example" {
		t.Errorf("status = %v", got)
	}
}
