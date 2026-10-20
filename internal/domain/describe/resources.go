package describe

import (
	"encoding/base64"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Options changes what a description may show.
type Options struct {
	// Reveal shows a Secret's values. Without it they are masked: a value on
	// screen is a value in a screen share, a recording or a scrollback.
	Reveal bool
}

// previewWidth bounds one value in a table row; the whole value is in the
// YAML view, one key away.
const previewWidth = 96

func describeConfigMap(doc map[string]any) []Section {
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	data, binary := child(doc, "data"), child(doc, "binaryData")
	overview.add("Keys", strconv.Itoa(len(data)+len(binary)))
	if boolean(doc, "immutable") {
		overview.add("Immutable", "yes")
	}

	values := Section{Title: "Data", Columns: []string{"Key", "Size", "Value"}, Empty: "no keys"}
	files := map[string]string{}
	for _, key := range sortedKeys(data) {
		text, _ := data[key].(string)
		values.Rows = append(values.Rows, []string{key, size(len(text)), preview(text)})
		files[key] = text
	}
	for _, key := range sortedKeys(binary) {
		encoded, _ := binary[key].(string)
		n := base64.StdEncoding.DecodedLen(len(encoded))
		if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			n = len(raw)
		}
		values.Rows = append(values.Rows, []string{key, size(n), "binary"})
	}
	return append([]Section{overview, values}, fileSections(files)...)
}

// maxFileLines bounds one value written out in full; the YAML view has the rest.
const maxFileLines = 200

// fileSections writes every multi-line value out in full, one section per
// key, because a config file summarised to its first line is a config file
// nobody can check.
func fileSections(values map[string]string) []Section {
	keys := make([]string, 0, len(values))
	for k, v := range values {
		if strings.Contains(strings.TrimRight(v, "\n"), "\n") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]Section, 0, len(keys))
	for _, k := range keys {
		lines := strings.Split(strings.TrimRight(values[k], "\n"), "\n")
		section := Section{Title: k, Columns: []string{"Content"}}
		for i, line := range lines {
			if i == maxFileLines {
				section.Rows = append(section.Rows, []string{"… " + strconv.Itoa(len(lines)-maxFileLines) + " more lines in the YAML view"})
				break
			}
			section.Rows = append(section.Rows, []string{strings.ReplaceAll(line, "\t", "    ")})
		}
		out = append(out, section)
	}
	return out
}

func describeSecret(doc map[string]any, opts Options) []Section {
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	data, plain := child(doc, "data"), child(doc, "stringData")
	overview.add("Type", str(doc, "type"))
	overview.add("Keys", strconv.Itoa(len(data)+len(plain)))
	if boolean(doc, "immutable") {
		overview.add("Immutable", "yes")
	}
	if !opts.Reveal && len(data)+len(plain) > 0 {
		overview.add("Values", "hidden; decode to show them")
	}

	values := Section{Title: "Data", Columns: []string{"Key", "Size", "Value"}, Empty: "no keys"}
	files := map[string]string{}
	for _, key := range sortedKeys(data) {
		encoded, _ := data[key].(string)
		raw, err := base64.StdEncoding.DecodeString(encoded)
		switch {
		case err != nil:
			values.Rows = append(values.Rows, []string{key, "—", "not valid base64"})
		case !opts.Reveal:
			values.Rows = append(values.Rows, []string{key, size(len(raw)), "••••••"})
		case printable(string(raw)):
			values.Rows = append(values.Rows, []string{key, size(len(raw)), preview(string(raw))})
			files[key] = string(raw)
		default:
			values.Rows = append(values.Rows, []string{key, size(len(raw)), "binary"})
		}
	}
	for _, key := range sortedKeys(plain) {
		text, _ := plain[key].(string)
		shown := "••••••"
		if opts.Reveal {
			shown = preview(text)
		}
		values.Rows = append(values.Rows, []string{key + " (stringData)", size(len(text)), shown})
		if opts.Reveal {
			files[key+" (stringData)"] = text
		}
	}
	return append([]Section{overview, values}, fileSections(files)...)
}

func describeServiceAccount(doc map[string]any) []Section {
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	if _, set := doc["automountServiceAccountToken"]; set {
		overview.add("Mounts its token", yesNo(boolean(doc, "automountServiceAccountToken")))
	} else {
		overview.add("Mounts its token", "yes (default)")
	}
	overview.add("Image pull secrets", strings.Join(names(slice(doc, "imagePullSecrets")), ", "))
	overview.add("Secrets", strings.Join(names(slice(doc, "secrets")), ", "))
	return []Section{overview}
}

func describeRole(doc map[string]any) []Section {
	rules := Section{Title: "Rules", Columns: []string{"API groups", "Resources", "Verbs", "Names"}, Empty: "no rules: this role allows nothing"}
	for _, r := range slice(doc, "rules") {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		resources := strings.Join(strings1(slice(rule, "resources")), ", ")
		if urls := strings1(slice(rule, "nonResourceURLs")); len(urls) > 0 {
			resources = "URLs " + strings.Join(urls, ", ")
		}
		groups := strings1(slice(rule, "apiGroups"))
		for i, g := range groups {
			if g == "" {
				groups[i] = "core"
			}
		}
		rules.Rows = append(rules.Rows, []string{
			orDash(strings.Join(groups, ", ")),
			orDash(resources),
			orDash(strings.Join(strings1(slice(rule, "verbs")), ", ")),
			orDash(strings.Join(strings1(slice(rule, "resourceNames")), ", ")),
		})
	}
	sections := []Section{rules}
	if aggregation := child(doc, "aggregationRule"); len(aggregation) > 0 {
		agg := Section{Title: "Aggregated from", Columns: []string{"Selector"}}
		for _, s := range slice(aggregation, "clusterRoleSelectors") {
			if sel, ok := s.(map[string]any); ok {
				agg.Rows = append(agg.Rows, []string{selectorOf(sel)})
			}
		}
		sections = append(sections, agg)
	}
	return sections
}

func describeBinding(doc map[string]any) []Section {
	ref := child(doc, "roleRef")
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Grants", str(ref, "kind")+"/"+str(ref, "name"))
	subjects := Section{Title: "Subjects", Columns: []string{"Kind", "Name", "Namespace"}, Empty: "no subjects: this binding grants nothing"}
	for _, s := range slice(doc, "subjects") {
		subject, ok := s.(map[string]any)
		if !ok {
			continue
		}
		subjects.Rows = append(subjects.Rows, []string{str(subject, "kind"), str(subject, "name"), orDash(str(subject, "namespace"))})
	}
	return []Section{overview, subjects}
}

func describeNetworkPolicy(doc map[string]any) []Section {
	spec := child(doc, "spec")
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	selector := selectorOf(child(spec, "podSelector"))
	if selector == "" {
		selector = "every pod in the namespace"
	}
	overview.add("Applies to", selector)
	types := strings1(slice(spec, "policyTypes"))
	if len(types) == 0 {
		types = []string{"Ingress"}
		if len(slice(spec, "egress")) > 0 {
			types = append(types, "Egress")
		}
	}
	overview.add("Restricts", strings.Join(types, ", "))

	sections := []Section{overview}
	for _, direction := range []struct{ field, peers, title string }{{"ingress", "from", "Allowed in"}, {"egress", "to", "Allowed out"}} {
		restricted := false
		for _, t := range types {
			restricted = restricted || strings.EqualFold(t, direction.field)
		}
		if !restricted {
			continue
		}
		section := Section{Title: direction.title, Columns: []string{"Peers", "Ports"}, Empty: "nothing: all " + direction.field + " traffic is denied"}
		for _, r := range slice(spec, direction.field) {
			rule, ok := r.(map[string]any)
			if !ok {
				continue
			}
			section.Rows = append(section.Rows, []string{peers(slice(rule, direction.peers)), policyPorts(slice(rule, "ports"))})
		}
		sections = append(sections, section)
	}
	return sections
}

func peers(list []any) string {
	if len(list) == 0 {
		return "anywhere"
	}
	parts := make([]string, 0, len(list))
	for _, p := range list {
		peer, ok := p.(map[string]any)
		if !ok {
			continue
		}
		var bits []string
		if ns, ok := peer["namespaceSelector"].(map[string]any); ok {
			if s := selectorOf(ns); s != "" {
				bits = append(bits, "namespaces "+s)
			} else {
				bits = append(bits, "all namespaces")
			}
		}
		if pod, ok := peer["podSelector"].(map[string]any); ok {
			if s := selectorOf(pod); s != "" {
				bits = append(bits, "pods "+s)
			} else {
				bits = append(bits, "all pods")
			}
		}
		if block := child(peer, "ipBlock"); len(block) > 0 {
			cidr := str(block, "cidr")
			if except := strings1(slice(block, "except")); len(except) > 0 {
				cidr += " except " + strings.Join(except, ", ")
			}
			bits = append(bits, cidr)
		}
		parts = append(parts, strings.Join(bits, " in "))
	}
	return strings.Join(parts, "; ")
}

func policyPorts(list []any) string {
	if len(list) == 0 {
		return "all ports"
	}
	parts := make([]string, 0, len(list))
	for _, p := range list {
		port, ok := p.(map[string]any)
		if !ok {
			continue
		}
		label := orDash(value(port["port"]))
		if end := number(port, "endPort"); end > 0 {
			label += "-" + strconv.Itoa(end)
		}
		parts = append(parts, label+"/"+firstOf(str(port, "protocol"), "TCP"))
	}
	return strings.Join(parts, ", ")
}

func describeAutoscaler(doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")
	target := child(spec, "scaleTargetRef")
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Scales", str(target, "kind")+"/"+str(target, "name"))
	overview.add("Replicas", strconv.Itoa(number(status, "currentReplicas"))+" now, "+strconv.Itoa(number(status, "desiredReplicas"))+" wanted")
	minimum := number(spec, "minReplicas")
	if minimum == 0 {
		minimum = 1
	}
	overview.add("Range", strconv.Itoa(minimum)+"–"+strconv.Itoa(number(spec, "maxReplicas")))

	metrics := Section{Title: "Metrics", Columns: []string{"Metric", "Target", "Current"}, Empty: "none"}
	current := slice(status, "currentMetrics")
	for i, m := range slice(spec, "metrics") {
		metric, ok := m.(map[string]any)
		if !ok {
			continue
		}
		name, goal := metricOf(metric, "target")
		now := "—"
		if i < len(current) {
			if c, ok := current[i].(map[string]any); ok {
				_, now = metricOf(c, "current")
			}
		}
		metrics.Rows = append(metrics.Rows, []string{name, goal, now})
	}
	return []Section{overview, metrics}
}

// metricOf reads one autoscaling/v2 metric, whatever its source type.
func metricOf(metric map[string]any, field string) (name, amount string) {
	kind := str(metric, "type")
	source := child(metric, strings.ToLower(kind[:min(1, len(kind))])+kind[min(1, len(kind)):])
	switch kind {
	case "Resource", "ContainerResource":
		name = str(source, "name")
		if c := str(source, "container"); c != "" {
			name += " (" + c + ")"
		}
	default:
		name = str(child(source, "metric"), "name")
		if name == "" {
			name = kind
		}
	}
	m := child(source, field)
	switch {
	case m["averageUtilization"] != nil:
		amount = value(m["averageUtilization"]) + "%"
	case m["averageValue"] != nil:
		amount = value(m["averageValue"]) + " avg"
	default:
		amount = orDash(value(m["value"]))
	}
	return name, amount
}

func describeBudget(doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	selector, set := spec["selector"].(map[string]any)
	switch {
	case !set:
		overview.add("Applies to", "no pods (no selector)")
	case selectorOf(selector) == "":
		overview.add("Applies to", "every pod in the namespace")
	default:
		overview.add("Applies to", selectorOf(selector))
	}
	overview.add("Min available", value(spec["minAvailable"]))
	overview.add("Max unavailable", value(spec["maxUnavailable"]))
	overview.Rows = append(overview.Rows,
		[]string{"Healthy", strconv.Itoa(number(status, "currentHealthy")) + " of " + strconv.Itoa(number(status, "desiredHealthy")) + " required"},
		[]string{"Disruptions allowed", strconv.Itoa(number(status, "disruptionsAllowed"))})
	return []Section{overview}
}

func describeVolume(doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Phase", str(status, "phase"))
	overview.add("Capacity", resourceList(child(spec, "capacity")))
	overview.add("Access modes", strings.Join(strings1(slice(spec, "accessModes")), ", "))
	overview.add("Storage class", str(spec, "storageClassName"))
	overview.add("Reclaim policy", str(spec, "persistentVolumeReclaimPolicy"))
	overview.add("Volume mode", str(spec, "volumeMode"))
	if claim := child(spec, "claimRef"); len(claim) > 0 {
		overview.add("Bound to", str(claim, "namespace")+"/"+str(claim, "name"))
	}
	if csi := child(spec, "csi"); len(csi) > 0 {
		overview.add("CSI driver", str(csi, "driver"))
		overview.add("Volume handle", str(csi, "volumeHandle"))
	}
	for _, source := range []string{"hostPath", "nfs", "local", "awsElasticBlockStore", "gcePersistentDisk", "azureDisk"} {
		if s := child(spec, source); len(s) > 0 {
			overview.add("Source", source+" "+firstOf(str(s, "path"), str(s, "server"), str(s, "volumeID"), str(s, "pdName"), str(s, "diskName")))
		}
	}
	overview.add("Message", str(status, "message"))
	return []Section{overview}
}

func describeStorageClass(doc map[string]any) []Section {
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Provisioner", str(doc, "provisioner"))
	overview.add("Reclaim policy", firstOf(str(doc, "reclaimPolicy"), "Delete"))
	overview.add("Binding mode", firstOf(str(doc, "volumeBindingMode"), "Immediate"))
	overview.add("Expansion", yesNo(boolean(doc, "allowVolumeExpansion")))
	if child(child(doc, "metadata"), "annotations")["storageclass.kubernetes.io/is-default-class"] == "true" {
		overview.add("Default", "yes")
	}
	params := Section{Title: "Parameters", Columns: []string{"Key", "Value"}, Empty: "none"}
	p := child(doc, "parameters")
	for _, k := range sortedKeys(p) {
		params.Rows = append(params.Rows, []string{k, value(p[k])})
	}
	return []Section{overview, params}
}

func describeNamespace(doc map[string]any) []Section {
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Phase", str(child(doc, "status"), "phase"))
	overview.add("Finalizers", strings.Join(strings1(slice(child(doc, "spec"), "finalizers")), ", "))
	return []Section{overview}
}

func describeQuota(doc map[string]any) []Section {
	hard, used := child(child(doc, "status"), "hard"), child(child(doc, "status"), "used")
	if len(hard) == 0 {
		hard = child(child(doc, "spec"), "hard")
	}
	usage := Section{Title: "Usage", Columns: []string{"Resource", "Used", "Hard"}, Empty: "no limits"}
	for _, k := range sortedKeys(hard) {
		usage.Rows = append(usage.Rows, []string{k, orDash(value(used[k])), value(hard[k])})
	}
	return []Section{usage}
}

func describeLimitRange(doc map[string]any) []Section {
	limits := Section{Title: "Limits", Columns: []string{"Type", "Resource", "Min", "Max", "Default request", "Default limit"}, Empty: "none"}
	for _, l := range slice(child(doc, "spec"), "limits") {
		limit, ok := l.(map[string]any)
		if !ok {
			continue
		}
		minimum, maximum := child(limit, "min"), child(limit, "max")
		request, def := child(limit, "defaultRequest"), child(limit, "default")
		seen := map[string]bool{}
		for _, m := range []map[string]any{minimum, maximum, request, def} {
			for k := range m {
				seen[k] = true
			}
		}
		for _, k := range sortedKeys(toAny(seen)) {
			limits.Rows = append(limits.Rows, []string{str(limit, "type"), k,
				orDash(value(minimum[k])), orDash(value(maximum[k])), orDash(value(request[k])), orDash(value(def[k]))})
		}
	}
	return []Section{limits}
}

func describeEndpointSlice(doc map[string]any) []Section {
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Service", str(child(child(doc, "metadata"), "labels"), "kubernetes.io/service-name"))
	overview.add("Address type", str(doc, "addressType"))
	var ports []string
	for _, p := range slice(doc, "ports") {
		if port, ok := p.(map[string]any); ok {
			ports = append(ports, firstOf(str(port, "name"), "unnamed")+" "+strconv.Itoa(number(port, "port"))+"/"+firstOf(str(port, "protocol"), "TCP"))
		}
	}
	overview.add("Ports", strings.Join(ports, ", "))
	endpoints := Section{Title: "Endpoints", Columns: []string{"Address", "Ready", "Serving", "Terminating", "Target", "Node"}, Empty: "no endpoints: the service routes to nothing"}
	for _, e := range slice(doc, "endpoints") {
		endpoint, ok := e.(map[string]any)
		if !ok {
			continue
		}
		conditions := child(endpoint, "conditions")
		cond := func(k string) string {
			if v, ok := conditions[k].(bool); ok {
				return yesNo(v)
			}
			return "—"
		}
		endpoints.Rows = append(endpoints.Rows, []string{
			strings.Join(strings1(slice(endpoint, "addresses")), ", "), cond("ready"), cond("serving"), cond("terminating"),
			orDash(str(child(endpoint, "targetRef"), "name")), orDash(str(endpoint, "nodeName")),
		})
	}
	return []Section{overview, endpoints}
}

func describeCRD(doc map[string]any) []Section {
	spec := child(doc, "spec")
	names := child(spec, "names")
	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Group", str(spec, "group"))
	overview.add("Kind", str(names, "kind"))
	overview.add("Plural", str(names, "plural"))
	overview.add("Scope", str(spec, "scope"))
	overview.add("Short names", strings.Join(strings1(slice(names, "shortNames")), ", "))
	versions := Section{Title: "Versions", Columns: []string{"Version", "Served", "Storage", "Deprecated"}, Empty: "none"}
	for _, v := range slice(spec, "versions") {
		version, ok := v.(map[string]any)
		if !ok {
			continue
		}
		versions.Rows = append(versions.Rows, []string{str(version, "name"), yesNo(boolean(version, "served")), yesNo(boolean(version, "storage")), yesNo(boolean(version, "deprecated"))})
	}
	return []Section{overview, versions}
}

// specSection is what a kind with no status of its own still has to say: the
// fields its spec sets, flattened two levels deep.
func specSection(doc map[string]any) Section {
	section := Section{Title: "Spec", Columns: []string{"Field", "Value"}, Empty: "the object sets no spec fields"}
	var walk func(prefix string, m map[string]any, depth int)
	walk = func(prefix string, m map[string]any, depth int) {
		for _, k := range sortedKeys(m) {
			if len(section.Rows) >= 40 {
				return
			}
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if nested, ok := m[k].(map[string]any); ok && depth < 2 {
				walk(path, nested, depth+1)
				continue
			}
			if rendered := value(m[k]); rendered != "" {
				section.Rows = append(section.Rows, []string{path, preview(rendered)})
			}
		}
	}
	walk("", child(doc, "spec"), 0)
	return section
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func toAny(m map[string]bool) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func strings1(list []any) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func names(list []any) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			out = append(out, str(m, "name"))
		}
	}
	return out
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// preview is one value as it fits in a row: its first line, cut at a width,
// with how much more there is said rather than hidden.
func preview(s string) string {
	if s == "" {
		return "(empty)"
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	first := strings.TrimSpace(lines[0])
	if utf8.RuneCountInString(first) > previewWidth {
		first = string([]rune(first)[:previewWidth]) + "…"
	}
	if len(lines) > 1 {
		first += "  … (" + strconv.Itoa(len(lines)) + " lines)"
	}
	return first
}

func printable(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func size(n int) string {
	switch {
	case n < 1024:
		return strconv.Itoa(n) + " B"
	case n < 1024*1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KiB"
	default:
		return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MiB"
	}
}

// topLevelSection describes a kind that keeps its fields at the top of the
// document rather than under spec — PriorityClass, the webhook
// configurations, Event: whatever is there, lists counted.
func topLevelSection(doc map[string]any) Section {
	section := Section{Title: "Fields", Columns: []string{"Field", "Value"}, Empty: "the object has no fields beyond its metadata"}
	for _, k := range sortedKeys(doc) {
		switch k {
		case "apiVersion", "kind", "metadata", "status", "spec":
			continue
		}
		switch v := doc[k].(type) {
		case []any:
			if names := names(v); len(names) > 0 && names[0] != "" {
				section.Rows = append(section.Rows, []string{k, preview(strings.Join(names, ", "))})
			} else {
				section.Rows = append(section.Rows, []string{k, strconv.Itoa(len(v)) + " items"})
			}
		default:
			if rendered := value(v); rendered != "" {
				section.Rows = append(section.Rows, []string{k, preview(rendered)})
			}
		}
	}
	return section
}
