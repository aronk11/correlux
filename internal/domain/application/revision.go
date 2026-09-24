package application

import (
	"sort"
	"strings"
)

// Revision is one rollout of a Deployment: the ReplicaSet its controller made
// for one version of the pod template.
//
// It is how Kubernetes remembers what changed, and the only such memory it
// keeps without an audit log: every edit to a Deployment's template leaves the
// previous ReplicaSet behind, numbered, until revisionHistoryLimit prunes it.
type Revision struct {
	Meta
	// Number is the controller's own revision stamp; zero when it is missing,
	// which a ReplicaSet created by hand can be.
	Number int64
	// Desired and Ready are the ReplicaSet's replica counts: how much of the
	// application this revision is serving right now.
	Desired int32
	Ready   int32
	// Template is the pod template flattened to one entry per field worth
	// comparing between two rollouts, keyed by a readable path:
	// "container api image" → "registry/api:1.9". See TemplateChanges.
	Template map[string]string
	// ChangeCause is kubernetes.io/change-cause, when somebody wrote one.
	ChangeCause string
}

// Owner returns the UID of the Deployment that controls this revision.
func (r *Revision) Owner() string {
	if o, ok := r.Controller(); ok {
		return o.UID
	}
	return ""
}

// RevisionsOf returns a Deployment's revisions, newest first.
func (c Context) RevisionsOf(uid string) []Revision {
	if uid == "" {
		return nil
	}
	var out []Revision
	for i := range c.Revisions {
		if c.Revisions[i].Owner() == uid {
			out = append(out, c.Revisions[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Number != out[j].Number {
			return out[i].Number > out[j].Number
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Change is one field that differs between two pod templates.
type Change struct {
	Field string
	// From and To are empty when the field was added or removed.
	From string
	To   string
	// Sensitive fields are compared but never quoted: an environment
	// variable's value is routinely a password, and WHY is read on shared
	// screens during incidents.
	Sensitive bool
}

// String renders the change for one line of screen.
func (c Change) String() string {
	switch {
	case c.Sensitive && c.From == "":
		return c.Field + " added"
	case c.Sensitive && c.To == "":
		return c.Field + " removed"
	case c.Sensitive:
		return c.Field + " changed"
	case c.From == "":
		return c.Field + " added: " + c.To
	case c.To == "":
		return c.Field + " removed (was " + c.From + ")"
	default:
		return c.Field + ": " + c.From + " → " + c.To
	}
}

// TemplateChanges lists what differs between two flattened pod templates,
// images first — the change most rollouts are — then everything else in a
// stable order.
func TemplateChanges(from, to map[string]string) []Change {
	var out []Change
	for field, value := range to {
		if old, ok := from[field]; !ok || old != value {
			out = append(out, Change{Field: field, From: from[field], To: value, Sensitive: sensitiveField(field)})
		}
	}
	for field, value := range from {
		if _, ok := to[field]; !ok {
			out = append(out, Change{Field: field, From: value, Sensitive: sensitiveField(field)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ii, ij := isImage(out[i].Field), isImage(out[j].Field)
		if ii != ij {
			return ii
		}
		return out[i].Field < out[j].Field
	})
	return out
}

func isImage(field string) bool { return strings.HasSuffix(field, " image") }

// sensitiveField reports the fields whose values are not shown: environment
// variables written into the template, and anything named like a credential.
func sensitiveField(field string) bool {
	if strings.Contains(field, " env ") {
		return true
	}
	lower := strings.ToLower(field)
	for _, word := range []string{"password", "secret", "token", "credential", "apikey", "api-key"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}
