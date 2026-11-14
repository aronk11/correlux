package application

import (
	"reflect"
	"testing"
)

func hosts(refs []Reference) []string {
	var out []string
	for _, r := range refs {
		out = append(out, r.Host)
	}
	return out
}

func TestReferencesKeepTheHostAndNothingElse(t *testing.T) {
	cases := []struct {
		name, value string
		want        []string
	}{
		{"DATABASE_URL", "postgres://app:s3cr3t@postgres.data.svc.cluster.local:5432/shop?sslmode=disable", []string{"postgres.data.svc.cluster.local"}},
		{"REDIS_HOST", "redis", []string{"redis"}},
		{"KAFKA_BROKERS", "kafka-0.kafka:9092,kafka-1.kafka:9092", []string{"kafka-0.kafka", "kafka-1.kafka"}},
		{"DB_DSN", "app:pw@tcp(mysql:3306)/shop", []string{"mysql"}},
		{"SPRING_DATASOURCE_URL", "jdbc:postgresql://orders-db:5432/orders", []string{"orders-db"}},
		{"UPSTREAM", "payments.shop.svc", []string{"payments.shop.svc"}},
		// A value is an address only where the variable says so.
		{"MODE", "api", nil},
		{"LOG_LEVEL", "debug", nil},
		{"CACHE_HOST", "10.0.0.12", nil},
		{"METRICS_URL", "http://localhost:9090/metrics", nil},
		{"FEATURE", "", nil},
	}
	for _, c := range cases {
		if got := hosts(ReferencesIn(c.name, c.value)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s=%q: hosts = %v, want %v", c.name, c.value, got, c.want)
		}
	}
	for _, r := range ReferencesIn("DATABASE_URL", "postgres://app:s3cr3t@postgres:5432/shop") {
		if r.Host != "postgres" || r.Env != "DATABASE_URL" {
			t.Errorf("the reference must carry the host and the variable, never the credentials: %+v", r)
		}
	}
}

// dependencySnapshot is a shop that talks to a database next door, a cache
// in another namespace, and something outside the cluster.
func dependencySnapshot() Snapshot {
	workload := func(ns, name string) Workload {
		m := meta("Deployment", name, map[string]string{"app": name})
		m.Namespace, m.UID = ns, ns+"/"+name
		return Workload{Meta: m, Desired: 1, Ready: 1, Replicated: true, Selector: map[string]string{"app": name}}
	}
	service := func(ns, name string) Service {
		m := meta("Service", name, nil)
		m.Namespace, m.UID = ns, ns+"/svc/"+name
		return Service{Meta: m, Selector: map[string]string{"app": name}}
	}
	pod := meta("Pod", "checkout-0", map[string]string{"app": "checkout"})
	return Snapshot{
		Workloads: []Workload{workload("payments", "checkout"), workload("payments", "postgres"), workload("cache", "redis")},
		Services:  []Service{service("payments", "checkout"), service("payments", "postgres"), service("cache", "redis")},
		Pods: []Pod{{Meta: pod, Phase: "Running", Ready: true, References: []Reference{
			{Env: "DATABASE_URL", Host: "postgres"},
			{Env: "REDIS_URL", Host: "redis.cache.svc.cluster.local"},
			{Env: "STRIPE_URL", Host: "api.stripe.com"},
			{Env: "SELF_URL", Host: "checkout"},
		}}},
	}
}

func TestDependenciesAreResolvedAgainstTheServicesThatExist(t *testing.T) {
	apps := Group(dependencySnapshot())
	checkout := find(t, apps, "checkout")

	var got []string
	for _, d := range checkout.DependsOn {
		got = append(got, d.App+" via "+d.Via)
	}
	want := []string{"cache/redis via env REDIS_URL", "payments/postgres via env DATABASE_URL"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("depends on %v, want %v: an external host and the application itself are not dependencies", got, want)
	}
	if used := find(t, apps, "postgres").UsedBy; len(used) != 1 || used[0] != "checkout" {
		t.Errorf("postgres is used by %v, want checkout", used)
	}
}

func TestADeclaredDependencyOutranksOneReadFromAURL(t *testing.T) {
	s := dependencySnapshot()
	s.Workloads[0].Annotations = map[string]string{"a8r.io/dependencies": "postgres, something-external"}
	checkout := find(t, Group(s), "checkout")
	for _, d := range checkout.DependsOn {
		if d.Name == "postgres" && (!d.Declared || d.Via != "annotation a8r.io/dependencies") {
			t.Errorf("what somebody wrote down is the evidence to show: %+v", d)
		}
	}
}

func TestAStatefulSetsPerPodNameResolvesToItsService(t *testing.T) {
	lookup := func(key string) (string, bool) {
		known := map[string]string{"shop/kafka": "kafka", "data/kafka": "kafka-in-data"}
		v, ok := known[key]
		return v, ok
	}
	namespaces := map[string]bool{"shop": true, "data": true}
	cases := map[string]string{
		"kafka":                                 "kafka",
		"kafka-0.kafka":                         "kafka",
		"kafka.data":                            "kafka-in-data",
		"kafka.data.svc.cluster.local":          "kafka-in-data",
		"kafka-1.kafka.data.svc.cluster.local.": "kafka-in-data",
	}
	for host, want := range cases {
		if got, ok := resolveHost(host, "shop", namespaces, lookup); !ok || got != want {
			t.Errorf("%s resolved to %q (%v), want %q", host, got, ok, want)
		}
	}
	if _, ok := resolveHost("api.stripe.com", "shop", namespaces, lookup); ok {
		t.Error("a host outside the cluster resolves to nothing")
	}
}

func TestTheNamespaceSpeaksWhereNoWorkloadDid(t *testing.T) {
	own := meta("Deployment", "labelled", map[string]string{"team": "search"})
	s := Snapshot{
		Workloads: []Workload{
			{Meta: meta("Deployment", "plain", nil), Desired: 1, Ready: 1, Replicated: true},
			{Meta: own, Desired: 1, Ready: 1, Replicated: true},
		},
		Namespaces: []Meta{{Kind: "Namespace", Name: "payments", Labels: map[string]string{"team": "payments", "env": "prod"}}},
	}
	apps := Group(s)
	plain := find(t, apps, "plain").Traits
	if plain.Team != "payments" || plain.TeamFrom != "namespace" || plain.Environment != "prod" {
		t.Errorf("an unlabelled workload inherits its namespace's labels, and says so: %+v", plain)
	}
	if labelled := find(t, apps, "labelled").Traits; labelled.Team != "search" || labelled.TeamFrom != "" {
		t.Errorf("a workload's own label wins: %+v", labelled)
	}
}

func TestAnOrganisationsOwnLabelsAreReadFirst(t *testing.T) {
	m := meta("Deployment", "api", map[string]string{"acme.io/squad": "ledger", "owner": "someone-who-left", "acme.io/stage": "canary"})
	s := Snapshot{Workloads: []Workload{{Meta: m, Desired: 1, Ready: 1, Replicated: true}}}

	if got := find(t, Group(s), "api").Traits.Team; got != "someone-who-left" {
		t.Errorf("without configuration the built-in spellings apply, got %q", got)
	}
	custom := find(t, GroupWith(s, Labels{Team: []string{"acme.io/squad"}, Environment: []string{"acme.io/stage"}}), "api").Traits
	if custom.Team != "ledger" || custom.Environment != "canary" {
		t.Errorf("configured labels come first: %+v", custom)
	}
}
