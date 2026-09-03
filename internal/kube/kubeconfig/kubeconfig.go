// Package kubeconfig reads the user's kubeconfig and exposes it as a list of
// selectable contexts.
//
// It never writes to the kubeconfig: switching context inside Correlux changes
// the session only, so an external `kubectl` keeps pointing wherever the user
// left it.
package kubeconfig

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Context is one selectable kubeconfig context.
type Context struct {
	// Name is the context name, unique within the merged kubeconfig.
	Name string
	// Cluster is the name of the cluster the context points at.
	Cluster string
	// User is the name of the auth info the context uses.
	User string
	// Namespace is the default namespace recorded in the context, or "default".
	Namespace string
	// Server is the API server URL, when the cluster entry is resolvable.
	Server string
	// Production reports whether this context is classified as production.
	Production bool
	// Current reports whether this is the kubeconfig's current-context.
	Current bool
}

// Config is a loaded, merged kubeconfig.
type Config struct {
	// Contexts is sorted by name.
	Contexts []Context
	// CurrentContext is the kubeconfig's current-context ("" if unset).
	CurrentContext string
	// Sources lists the kubeconfig files that were merged, in precedence order.
	Sources []string

	raw          clientcmdapi.Config
	loadingRules *clientcmd.ClientConfigLoadingRules
}

// LoadOptions controls kubeconfig discovery.
type LoadOptions struct {
	// ExplicitPath is the value of --kubeconfig. When empty, KUBECONFIG and
	// the default path (~/.kube/config) are used.
	ExplicitPath string
	// Classifier decides which contexts count as production. Optional.
	Classifier *Classifier
}

// Load reads and merges the kubeconfig according to the standard rules.
func Load(opts LoadOptions) (*Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.ExplicitPath != "" {
		rules.ExplicitPath = opts.ExplicitPath
	}
	// Do not let a stale/unreadable file in KUBECONFIG abort the whole load;
	// clientcmd already tolerates this, but be explicit about the intent.
	rules.WarnIfAllMissing = false

	raw, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	cfg := &Config{
		CurrentContext: raw.CurrentContext,
		Sources:        rules.Precedence,
		raw:            *raw,
		loadingRules:   rules,
	}
	if rules.ExplicitPath != "" {
		cfg.Sources = []string{rules.ExplicitPath}
	}

	classifier := opts.Classifier
	if classifier == nil {
		classifier = DefaultClassifier()
	}

	for name, kctx := range raw.Contexts {
		if kctx == nil {
			continue
		}
		c := Context{
			Name:      name,
			Cluster:   kctx.Cluster,
			User:      kctx.AuthInfo,
			Namespace: kctx.Namespace,
			Current:   name == raw.CurrentContext,
		}
		if c.Namespace == "" {
			c.Namespace = "default"
		}
		if cl, ok := raw.Clusters[kctx.Cluster]; ok && cl != nil {
			c.Server = cl.Server
		}
		c.Production = classifier.IsProduction(c.Name, c.Cluster, c.Server)
		cfg.Contexts = append(cfg.Contexts, c)
	}
	sort.Slice(cfg.Contexts, func(i, j int) bool {
		return cfg.Contexts[i].Name < cfg.Contexts[j].Name
	})
	return cfg, nil
}

// Raw exposes the merged API config. Callers must treat it as read-only.
func (c *Config) Raw() clientcmdapi.Config { return c.raw }

// LoadingRules exposes the rules used for the merge, for building client
// configs against a specific context.
func (c *Config) LoadingRules() *clientcmd.ClientConfigLoadingRules { return c.loadingRules }

// Context looks up a context by name.
func (c *Config) Context(name string) (Context, bool) {
	for _, k := range c.Contexts {
		if k.Name == name {
			return k, true
		}
	}
	return Context{}, false
}

// Names returns the context names in display order.
func (c *Config) Names() []string {
	out := make([]string, 0, len(c.Contexts))
	for _, k := range c.Contexts {
		out = append(out, k.Name)
	}
	return out
}

// ResolveStartContext picks the context Correlux should open with, preferring
// an explicit request, then the configured startup context, then the
// kubeconfig's current-context, then the only context if there is exactly one.
//
// It returns an error only when nothing usable exists, so the caller can show a
// precise message instead of a generic failure.
func (c *Config) ResolveStartContext(requested, configured string) (string, error) {
	for _, candidate := range []string{requested, configured, c.CurrentContext} {
		if candidate == "" {
			continue
		}
		if _, ok := c.Context(candidate); ok {
			return candidate, nil
		}
		if candidate == requested {
			return "", fmt.Errorf("context %q not found in kubeconfig (available: %s)",
				requested, strings.Join(c.Names(), ", "))
		}
	}
	if len(c.Contexts) == 1 {
		return c.Contexts[0].Name, nil
	}
	if len(c.Contexts) == 0 {
		return "", fmt.Errorf("no contexts found in kubeconfig (%s)", strings.Join(c.Sources, ", "))
	}
	return "", fmt.Errorf("no current-context set; pass --context (available: %s)",
		strings.Join(c.Names(), ", "))
}
