// Package tree turns a Kubernetes document into a tree that can be folded and
// walked, so the YAML of a large object is something to navigate rather than
// something to scroll past.
//
// It is pure: it knows nothing about keys or the terminal. The app keeps which
// paths are open and where the cursor is; this package says what that looks
// like.
package tree

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"
)

// Node is one key of the document.
type Node struct {
	// Key is the field name, "[i]" for a list item, or empty for one line of a
	// multi-line string.
	Key string
	// Path identifies the node across reloads: "spec.containers[0].image".
	Path string
	// Value is the rendered scalar; empty for a node with children.
	Value string
	// Summary says what a folded node holds: "{3 fields}", "[2 items]",
	// "(12 lines)", and for a list item its name when it has one.
	Summary  string
	Children []*Node
	Depth    int
	Parent   *Node
}

// Branch reports whether the node can be opened.
func (n *Node) Branch() bool { return len(n.Children) > 0 }

// Parse reads a YAML or JSON document into its top-level nodes. A document
// that cannot be read is nil, and the caller shows it as text instead.
func Parse(document string) []*Node {
	raw, err := yaml.YAMLToJSON([]byte(document))
	if err != nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil
	}
	return children(nil, "", doc, 0)
}

// topOrder is the order a person reads an object in; anything else follows
// alphabetically.
var topOrder = map[string]int{"apiVersion": 0, "kind": 1, "metadata": 2, "spec": 3, "data": 4, "stringData": 5, "binaryData": 6, "status": 7}

func children(parent *Node, path string, v any, depth int) []*Node {
	var out []*Node
	switch typed := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if depth == 0 {
				oi, iok := topOrder[keys[i]]
				oj, jok := topOrder[keys[j]]
				switch {
				case iok && jok:
					return oi < oj
				case iok != jok:
					return iok
				}
			}
			return keys[i] < keys[j]
		})
		for _, k := range keys {
			out = append(out, node(parent, k, join(path, k), typed[k], depth))
		}
	case []any:
		for i, item := range typed {
			out = append(out, node(parent, "["+strconv.Itoa(i)+"]", path+"["+strconv.Itoa(i)+"]", item, depth))
		}
	}
	return out
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func node(parent *Node, key, path string, v any, depth int) *Node {
	n := &Node{Key: key, Path: path, Depth: depth, Parent: parent}
	switch typed := v.(type) {
	case map[string]any:
		n.Children = children(n, path, typed, depth+1)
		n.Summary = count(len(typed), "field", "fields", "{", "}")
		if strings.HasPrefix(key, "[") {
			if name := itemName(typed); name != "" {
				n.Summary = name
			}
		}
		if len(typed) == 0 {
			n.Value, n.Summary = "{}", ""
		}
	case []any:
		n.Children = children(n, path, typed, depth+1)
		n.Summary = count(len(typed), "item", "items", "[", "]")
		if len(typed) == 0 {
			n.Value, n.Summary = "[]", ""
		}
	case string:
		lines := strings.Split(strings.TrimRight(typed, "\n"), "\n")
		if len(lines) > 1 {
			n.Summary = "(" + strconv.Itoa(len(lines)) + " lines)"
			for i, line := range lines {
				n.Children = append(n.Children, &Node{Path: path + "#" + strconv.Itoa(i), Value: line, Depth: depth + 1, Parent: n})
			}
			return n
		}
		n.Value = typed
		if typed == "" {
			n.Value = `""`
		}
	case nil:
		n.Value = "null"
	case bool:
		n.Value = strconv.FormatBool(typed)
	case json.Number:
		n.Value = typed.String()
	default:
		n.Value = ""
	}
	return n
}

// itemName is what a list item is called, when it says: a container's name,
// a condition's type.
func itemName(m map[string]any) string {
	for _, k := range []string{"name", "type", "key", "mountPath", "containerPort", "ip"} {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case json.Number:
			return v.String()
		}
	}
	return ""
}

func count(n int, one, many, open, closing string) string {
	word := many
	if n == 1 {
		word = one
	}
	return open + strconv.Itoa(n) + " " + word + closing
}

// Open decides whether a node is unfolded.
type Open func(*Node) bool

// Visible is the nodes on screen, in order: every node whose ancestors are all
// open.
func Visible(roots []*Node, open Open) []*Node {
	var out []*Node
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			out = append(out, n)
			if n.Branch() && open(n) {
				walk(n.Children)
			}
		}
	}
	walk(roots)
	return out
}

// DefaultOpen is how a document first unfolds: the top two levels, and any
// node small enough to read at a glance, so a small custom resource shows
// everything at once. Metadata starts folded; it is what everybody skips,
// and its managedFields are noise unless you are hunting for them.
func DefaultOpen(n *Node) bool {
	switch {
	case n.Path == "metadata", n.Key == "managedFields":
		return false
	case n.Depth < 2:
		return true
	}
	return leaves(n, 9) <= 8
}

// leaves counts a node's leaves, stopping once it passes limit.
func leaves(n *Node, limit int) int {
	if !n.Branch() {
		return 1
	}
	total := 0
	for _, c := range n.Children {
		total += leaves(c, limit-total)
		if total >= limit {
			return total
		}
	}
	return total
}

// Walk calls fn for every node, depth first.
func Walk(roots []*Node, fn func(*Node)) {
	for _, n := range roots {
		fn(n)
		Walk(n.Children, fn)
	}
}

// Find returns the node at path.
func Find(roots []*Node, path string) *Node {
	var found *Node
	Walk(roots, func(n *Node) {
		if found == nil && n.Path == path {
			found = n
		}
	})
	return found
}
