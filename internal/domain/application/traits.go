package application

import (
	"sort"
	"strings"
)

// Traits are what an application's own labels say about it beyond its name:
// the larger system it is a part of, the version it runs, who owns it, which
// environment it is, and where somebody wrote down how to fix it.
//
// None of it is inferred. Every field is a label or annotation somebody put
// on the workloads, read in the order the conventions rank them, and a field
// nobody set stays empty — a column of guesses is worse than a missing one.
// They are what the dashboard groups and filters by, because "everything of
// the checkout system" and "everything team payments owns" are the questions
// a namespace list does not answer.
type Traits struct {
	// System is app.kubernetes.io/part-of: the larger application this one is
	// a piece of.
	System string
	// Components are the distinct app.kubernetes.io/component values of the
	// workloads, sorted: "controller, webhook".
	Components []string
	// Version is the version label, or failing that the image tag the pods
	// run. Two values mean two versions are running at once, which is what a
	// rollout in progress looks like.
	Version string
	// Team is who owns it, Environment where it sits in the path to
	// production. Both come from labels organisations name differently, so a
	// handful of common spellings are read.
	Team        string
	Environment string
	// TeamFrom and EnvironmentFrom are "namespace" when the label was read
	// off the namespace because no workload carried one. Inherited is still
	// read, never guessed, and it is still worth saying which it was.
	TeamFrom        string
	EnvironmentFrom string
	// Runbook, Repository and Chat are the a8r.io annotations: where to read
	// what to do, where the code lives and where to ask.
	Runbook    string
	Repository string
	Chat       string
	// Role is what the application is to the cluster, when it is one of the
	// well-known pieces of a platform.
	Role Role
}

// The label spellings read for each trait, most specific convention first.
var (
	systemLabels  = []string{"app.kubernetes.io/part-of"}
	versionLabels = []string{"app.kubernetes.io/version", "version", "tags.datadoghq.com/version"}
	teamLabels    = []string{"app.kubernetes.io/team", "team", "owner", "squad", "tribe",
		"tags.datadoghq.com/team", "backstage.io/owner"}
	environmentLabels = []string{"app.kubernetes.io/environment", "environment", "env", "stage",
		"tags.datadoghq.com/env"}
	componentLabels = []string{"app.kubernetes.io/component", "component", "tier"}
)

// The annotations kept for traits. They are few on purpose: see Meta.
const (
	annotationOwner      = "a8r.io/owner"
	annotationRunbook    = "a8r.io/runbook"
	annotationRepository = "a8r.io/repository"
	annotationChat       = "a8r.io/chat"
)

// TraitAnnotations are the annotation keys a collector must keep for traits
// to be readable. Everything else in an object's annotations is dropped.
var TraitAnnotations = []string{annotationOwner, annotationRunbook, annotationRepository, annotationChat,
	annotationDependencies}

// Labels are an organisation's own spellings of the labels no standard
// names. They are read before the built-in ones, so a cluster that carries
// both acme.io/team and a stray "owner" label shows the one its people use.
type Labels struct {
	Team        []string
	Environment []string
	System      []string
}

func withCustom(custom, builtin []string) []string {
	if len(custom) == 0 {
		return builtin
	}
	out := make([]string, 0, len(custom)+len(builtin))
	out = append(out, custom...)
	return append(out, builtin...)
}

// readTraits derives the traits from the application's members. Workloads
// speak first, because their labels are what somebody wrote; pods are asked
// only where no workload answered, which is every application an operator
// runs without a Deployment in between.
func (a *Application) readTraits(custom Labels, namespace Meta) {
	teams := withCustom(custom.Team, teamLabels)
	environments := withCustom(custom.Environment, environmentLabels)
	systems := withCustom(custom.System, systemLabels)

	metas := make([]Meta, 0, len(a.Workloads)+len(a.Pods))
	for i := range a.Workloads {
		metas = append(metas, a.Workloads[i].Meta)
	}
	for i := range a.Pods {
		metas = append(metas, a.Pods[i].Meta)
	}

	t := Traits{
		System:      firstLabel(metas, systems),
		Version:     firstLabel(metas, versionLabels),
		Team:        firstLabel(metas, teams),
		Environment: firstLabel(metas, environments),
		Runbook:     firstAnnotation(metas, annotationRunbook),
		Repository:  firstAnnotation(metas, annotationRepository),
		Chat:        firstAnnotation(metas, annotationChat),
	}
	if t.Team == "" {
		t.Team = firstAnnotation(metas, annotationOwner)
	}
	// The namespace speaks where no workload did. Teams label the namespace
	// they were given far more often than every Deployment in it.
	inherit := []Meta{namespace}
	if t.Team == "" {
		if t.Team = firstLabel(inherit, teams); t.Team == "" {
			t.Team = firstAnnotation(inherit, annotationOwner)
		}
		if t.Team != "" {
			t.TeamFrom = "namespace"
		}
	}
	if t.Environment == "" {
		if t.Environment = firstLabel(inherit, environments); t.Environment != "" {
			t.EnvironmentFrom = "namespace"
		}
	}
	if t.System == "" {
		t.System = firstLabel(inherit, systems)
	}
	if t.Version == "" {
		t.Version = imageVersion(a.Pods)
	}

	seen := map[string]bool{}
	for i := range a.Workloads {
		for _, key := range componentLabels {
			if v := strings.TrimSpace(a.Workloads[i].Labels[key]); v != "" {
				if !seen[v] {
					seen[v] = true
					t.Components = append(t.Components, v)
				}
				break
			}
		}
	}
	sort.Strings(t.Components)

	t.Role = roleOf(a)
	a.Traits = t
}

func firstLabel(metas []Meta, keys []string) string {
	for _, key := range keys {
		for i := range metas {
			if v := strings.TrimSpace(metas[i].Labels[key]); v != "" {
				return v
			}
		}
	}
	return ""
}

func firstAnnotation(metas []Meta, key string) string {
	for i := range metas {
		if v := strings.TrimSpace(metas[i].Annotations[key]); v != "" {
			return v
		}
	}
	return ""
}

// imageVersion reads the version off the images the pods run, for the many
// applications nobody labelled with one.
//
// Only the first regular container of each pod is asked: it is the
// application by every chart's convention, and a mesh sidecar's tag says
// nothing about what was deployed. Pods that have finished are left out, or a
// CronJob would report every version it ever ran. More than two distinct
// tags is not a version, it is a list, and reads as "mixed".
func imageVersion(pods []Pod) string {
	var tags []string
	seen := map[string]bool{}
	for i := range pods {
		p := &pods[i]
		if p.Terminal() || p.Phase == "Failed" {
			continue
		}
		if c := mainContainer(p); c != nil {
			if tag := ImageTag(c.Image); tag != "" && !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	sort.Strings(tags)
	switch len(tags) {
	case 0:
		return ""
	case 1:
		return tags[0]
	case 2:
		return tags[0] + ", " + tags[1]
	default:
		return "mixed"
	}
}

// meshProxies are the containers a service mesh puts into a pod. With
// holdApplicationUntilProxyStarts Istio lists its proxy first, and the proxy
// is never the application.
var meshProxies = map[string]bool{"istio-proxy": true, "linkerd-proxy": true}

// mainContainer is the container that is the application: the first regular
// one, by every chart's convention, that is not a mesh proxy.
func mainContainer(p *Pod) *Container {
	var first *Container
	for i := range p.Containers {
		c := &p.Containers[i]
		if c.Init {
			continue
		}
		if !meshProxies[c.Name] {
			return c
		}
		if first == nil {
			first = c
		}
	}
	return first
}

// ImageTag is the tag of an image reference, or a shortened digest for an
// image pinned by one. An image with neither runs "latest", and saying so is
// the point: it is the tag that explains why two pods of one ReplicaSet run
// different code.
func ImageTag(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	name, digest, pinned := strings.Cut(image, "@")
	// The tag separator is the last colon after the last slash; a colon before
	// that is a registry port.
	tag := ""
	if slash := strings.LastIndex(name, "/"); strings.LastIndex(name, ":") > slash {
		tag = name[strings.LastIndex(name, ":")+1:]
	}
	switch {
	case tag != "":
		return tag
	case pinned:
		_, hex, ok := strings.Cut(digest, ":")
		if !ok {
			hex = digest
		}
		if len(hex) > 12 {
			hex = hex[:12]
		}
		return "@" + hex
	default:
		return "latest"
	}
}

// ImageRepository is an image reference without its registry host, tag and
// digest: "ingress-nginx/controller" for
// "registry.k8s.io/ingress-nginx/controller:v1.11.2@sha256:…".
func ImageRepository(image string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(image), "@")
	if slash := strings.LastIndex(name, "/"); strings.LastIndex(name, ":") > slash {
		name = name[:strings.LastIndex(name, ":")]
	}
	// The first segment is a registry host when it could not be anything
	// else: it has a dot or a port, or it is localhost.
	if first, rest, ok := strings.Cut(name, "/"); ok &&
		(strings.ContainsAny(first, ".:") || first == "localhost") {
		name = rest
	}
	return name
}
