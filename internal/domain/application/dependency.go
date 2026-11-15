package application

import (
	"sort"
	"strings"
)

// annotationDependencies is the a8r.io convention for writing down what a
// service needs: a comma-separated list of names.
const annotationDependencies = "a8r.io/dependencies"

// Reference is one host a pod's environment points at.
type Reference struct {
	// Env is the variable that names it, and the whole of what is shown.
	Env string
	// Host is the host part alone — no scheme, credentials, port or path.
	Host string
	// Source is where the variable is defined when that is not the pod
	// itself: "ConfigMap/shop-config".
	Source string
}

// via renders the evidence for one reference.
func (r Reference) via() string {
	if r.Source != "" {
		return r.Source + " key " + r.Env
	}
	return "env " + r.Env
}

// ConfigSource is one place a pod's environment comes from: a whole
// ConfigMap (envFrom, with its prefix), or one key of one.
type ConfigSource struct {
	ConfigMap string
	// Key is the key taken, empty for envFrom, which takes them all.
	Key string
	// Env is the variable a single key becomes; Prefix what envFrom puts in
	// front of every key.
	Env    string
	Prefix string
}

// ConfigReference is one host a ConfigMap's value points at. Like Reference
// it keeps the key and the host and never the value.
type ConfigReference struct {
	Namespace string
	ConfigMap string
	Key       string
	Host      string
}

// LinkConfigDependencies recomputes every application's dependencies with
// what the ConfigMaps say added to what the pods say.
//
// It runs when the evidence arrives, because the ConfigMaps are read then and
// not on the dashboard's timer. The slice is updated in place; with no
// references it leaves the pod-only reading exactly as Group made it.
func LinkConfigDependencies(apps []Application, refs []ConfigReference) {
	byMap := map[string][]ConfigReference{}
	for _, r := range refs {
		key := r.Namespace + "/" + r.ConfigMap
		byMap[key] = append(byMap[key], r)
	}
	extra := map[string][]Reference{}
	for i := range apps {
		a := &apps[i]
		for j := range a.Pods {
			for _, src := range a.Pods[j].ConfigSources {
				for _, r := range byMap[a.Namespace+"/"+src.ConfigMap] {
					if src.Key != "" && src.Key != r.Key {
						continue
					}
					extra[a.Key()] = append(extra[a.Key()], Reference{
						Env: r.Key, Host: r.Host, Source: "ConfigMap/" + src.ConfigMap,
					})
				}
			}
		}
		a.DependsOn, a.UsedBy = nil, nil
	}
	linkDependenciesWith(apps, extra)
}

// Dependency is one application another one talks to, and how that is known.
type Dependency struct {
	// App is the key of the application depended on, Name and Namespace the
	// same thing in the form a sentence wants.
	App       string
	Name      string
	Namespace string
	// Service is the Service the reference resolved to.
	Service string
	// Via is the evidence: "env DATABASE_URL", or the annotation.
	Via string
	// Declared is true when somebody wrote the dependency down in an
	// annotation, rather than Correlux reading it off a connection string.
	Declared bool
}

// linkDependencies connects applications that name each other.
//
// Kubernetes records no dependencies. Two things in a cluster do say what an
// application talks to: an a8r.io/dependencies annotation, where somebody
// wrote it down, and the environment of its pods, where a DATABASE_URL names
// a Service. Both are resolved against the Services actually in the snapshot,
// and a name that resolves to nothing is dropped — it may be an external
// host, and "depends on something I cannot see" is not a relationship anyone
// can act on.
//
// It is a reading of configuration, not of traffic. An application may talk
// to things its environment never mentions, and may never call what it does
// mention. Every dependency therefore carries how it is known.
func linkDependencies(apps []Application) { linkDependenciesWith(apps, nil) }

// linkDependenciesWith is linkDependencies with further references per
// application key, read from somewhere other than the pods.
func linkDependenciesWith(apps []Application, extra map[string][]Reference) {
	type target struct{ app, name, namespace, service string }
	services := map[string]target{} // "namespace/service" → owning application
	namespaces := map[string]bool{}
	for i := range apps {
		a := &apps[i]
		namespaces[a.Namespace] = true
		for j := range a.Services {
			services[a.Namespace+"/"+a.Services[j].Name] = target{a.Key(), a.Name, a.Namespace, a.Services[j].Name}
		}
	}

	usedBy := map[string]map[string]bool{}
	for i := range apps {
		a := &apps[i]
		seen := map[string]bool{}
		add := func(t target, via string, declared bool) {
			if t.app == "" || t.app == a.Key() || seen[t.app] {
				return
			}
			seen[t.app] = true
			a.DependsOn = append(a.DependsOn, Dependency{
				App: t.app, Name: t.name, Namespace: t.namespace, Service: t.service, Via: via, Declared: declared,
			})
			if usedBy[t.app] == nil {
				usedBy[t.app] = map[string]bool{}
			}
			usedBy[t.app][a.Name] = true
		}

		// What somebody wrote down outranks what was read off a URL.
		for _, m := range a.metas() {
			for _, name := range strings.Split(m.Annotations[annotationDependencies], ",") {
				if name = strings.TrimSpace(name); name != "" {
					t, ok := resolveHost(name, a.Namespace, namespaces, func(key string) (target, bool) {
						t, ok := services[key]
						return t, ok
					})
					if !ok {
						// The annotation may name the application rather than
						// its Service.
						for j := range apps {
							if apps[j].Namespace == a.Namespace && apps[j].Name == name {
								t, ok = target{apps[j].Key(), apps[j].Name, apps[j].Namespace, ""}, true
							}
						}
					}
					if ok {
						add(t, "annotation "+annotationDependencies, true)
					}
				}
			}
		}
		for j := range a.Pods {
			for _, ref := range a.Pods[j].References {
				if t, ok := resolveHost(ref.Host, a.Namespace, namespaces, func(key string) (target, bool) {
					t, ok := services[key]
					return t, ok
				}); ok {
					add(t, ref.via(), false)
				}
			}
		}
		for _, ref := range extra[a.Key()] {
			if t, ok := resolveHost(ref.Host, a.Namespace, namespaces, func(key string) (target, bool) {
				t, ok := services[key]
				return t, ok
			}); ok {
				add(t, ref.via(), false)
			}
		}
		sort.Slice(a.DependsOn, func(x, y int) bool { return a.DependsOn[x].App < a.DependsOn[y].App })
	}
	for i := range apps {
		for name := range usedBy[apps[i].Key()] {
			apps[i].UsedBy = append(apps[i].UsedBy, name)
		}
		sort.Strings(apps[i].UsedBy)
	}
}

// resolveHost finds the Service a host name means from inside a namespace,
// the way cluster DNS would: "db" is the Service next door, "db.data" and
// "db.data.svc.cluster.local" the one in another namespace. A StatefulSet's
// per-pod name, "db-0.db", resolves to the Service behind it.
func resolveHost[T any](host, namespace string, namespaces map[string]bool, lookup func(string) (T, bool)) (T, bool) {
	var none T
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return none, false
	}
	labels := strings.Split(host, ".")
	try := func(service, ns string) (T, bool) { return lookup(ns + "/" + service) }

	switch {
	case len(labels) == 1:
		return try(labels[0], namespace)
	case len(labels) >= 3 && labels[2] == "svc":
		return try(labels[0], labels[1])
	case len(labels) >= 4 && labels[3] == "svc":
		return try(labels[1], labels[2]) // pod.service.namespace.svc
	case len(labels) == 2:
		if namespaces[labels[1]] {
			if t, ok := try(labels[0], labels[1]); ok {
				return t, true
			}
		}
		return try(labels[1], namespace) // pod.service
	case len(labels) == 3 && namespaces[labels[2]]:
		return try(labels[1], labels[2]) // pod.service.namespace
	}
	return none, false
}

// ReferencesIn reads the hosts out of one environment variable.
//
// It is deliberately narrow. A value with a scheme is a URL and its host is
// taken; a value mentioning ".svc" is cluster DNS. Anything else counts only
// when the variable's own name says it holds an address — REDIS_HOST,
// KAFKA_BROKERS — because MODE=api next to a Service called "api" is a
// coincidence, and a dependency graph built on coincidences is worse than
// none. Credentials, ports and paths are cut away before anything is kept.
func ReferencesIn(name, value string) []Reference {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 {
		return nil
	}
	addressed := addressName(name)
	var out []Reference
	seen := map[string]bool{}
	for _, piece := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		host, url := hostOf(piece)
		if host == "" || seen[host] {
			continue
		}
		if !url && !addressed && !strings.Contains(host, ".svc") {
			continue
		}
		if !hostLike(host) {
			continue
		}
		seen[host] = true
		out = append(out, Reference{Env: name, Host: host})
	}
	return out
}

// hostOf cuts one piece of a value down to its host, and reports whether the
// piece was a URL.
func hostOf(piece string) (host string, url bool) {
	if _, rest, ok := strings.Cut(piece, "://"); ok {
		piece, url = rest, true
	}
	if at := strings.IndexAny(piece, "/?#"); at >= 0 {
		piece = piece[:at]
	}
	if at := strings.LastIndex(piece, "@"); at >= 0 {
		piece = piece[at+1:] // user:password@
	}
	// tcp(host:port), as Go's MySQL driver writes it.
	if open := strings.Index(piece, "("); open >= 0 {
		piece = strings.TrimSuffix(piece[open+1:], ")")
	}
	if colon := strings.LastIndex(piece, ":"); colon >= 0 {
		piece = piece[:colon]
	}
	return strings.ToLower(piece), url
}

// hostLike reports whether s could be a DNS name: labels of letters, digits
// and dashes, with at least one letter so a bare IP or a number is not one.
func hostLike(s string) bool {
	if s == "" || len(s) > 253 || s == "localhost" {
		return false
	}
	letter := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			letter = true
		case r >= '0' && r <= '9', r == '-', r == '.':
		default:
			return false
		}
	}
	return letter && !strings.HasPrefix(s, ".") && !strings.HasPrefix(s, "-")
}

// addressName reports whether a variable's name says its value is an address.
func addressName(name string) bool {
	upper := strings.ToUpper(name)
	for _, word := range []string{"HOST", "URL", "URI", "ADDR", "ENDPOINT", "SERVER", "DSN", "BROKER", "BOOTSTRAP", "UPSTREAM"} {
		if strings.Contains(upper, word) {
			return true
		}
	}
	return false
}
