// Package client turns kubeconfig contexts into ready-to-use Kubernetes
// clients.
//
// The factory is the only place in Correlux that talks to the Kubernetes API
// wiring; everything above it receives interfaces and cancellable calls. All
// operations take a context.Context, because the UI must be able to abandon a
// slow request without freezing.
package client

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	// Auth providers register themselves on import, and a provider that is
	// not compiled in fails the whole context with "no Auth Provider found
	// for name" — a message that describes the binary rather than the
	// cluster, and that no amount of fixing the kubeconfig will cure.
	//
	// This is the only import in Correlux that exists for its side effect,
	// and it belongs here because this package is the one that authenticates.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// Defaults for client behaviour. They favour a responsive UI over completeness:
// a request that has not answered within the timeout is reported as such rather
// than left hanging.
const (
	DefaultTimeout = 15 * time.Second
	DefaultQPS     = 50
	DefaultBurst   = 100
)

// UserAgent identifies Correlux in API server logs and audit trails.
var UserAgent = "correlux"

// Factory builds and caches clients per kubeconfig context.
//
// It is safe for concurrent use: the UI's async commands run on their own
// goroutines and may hit the same context simultaneously.
type Factory struct {
	raw     clientcmdapi.Config
	rules   *clientcmd.ClientConfigLoadingRules
	timeout time.Duration

	mu    sync.Mutex
	cache map[string]*cached
}

type cached struct {
	restConfig *rest.Config
	clientset  *kubernetes.Clientset
	err        error
}

// Options configures a Factory.
type Options struct {
	// Timeout bounds every individual API request. Zero means DefaultTimeout.
	Timeout time.Duration
}

// New creates a Factory over an already-merged kubeconfig.
func New(raw clientcmdapi.Config, rules *clientcmd.ClientConfigLoadingRules, opts Options) *Factory {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Factory{
		raw:     raw,
		rules:   rules,
		timeout: timeout,
		cache:   make(map[string]*cached),
	}
}

// RESTConfig returns the REST configuration for a context, building it once and
// caching the result (including failures, which are deterministic for a given
// kubeconfig).
func (f *Factory) RESTConfig(contextName string) (*rest.Config, error) {
	c := f.entry(contextName)
	return c.restConfig, c.err
}

// Clientset returns a typed client for a context.
func (f *Factory) Clientset(contextName string) (kubernetes.Interface, error) {
	c := f.entry(contextName)
	if c.err != nil {
		return nil, c.err
	}
	return c.clientset, nil
}

// RESTConfigForExec returns a REST configuration for a long-lived interactive
// session: the same authentication RESTConfig returns, but without the
// per-request timeout, which would otherwise cut an exec session off after
// Timeout() regardless of whether anyone was still typing into it.
func (f *Factory) RESTConfigForExec(contextName string) (*rest.Config, error) {
	cfg, err := f.RESTConfig(contextName)
	if err != nil {
		return nil, err
	}
	cfg = rest.CopyConfig(cfg)
	cfg.Timeout = 0
	return cfg, nil
}

func (f *Factory) entry(contextName string) *cached {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.cache[contextName]; ok {
		return c
	}
	c := f.build(contextName)
	f.cache[contextName] = c
	return c
}

func (f *Factory) build(contextName string) *cached {
	if _, ok := f.raw.Contexts[contextName]; !ok {
		return &cached{err: fmt.Errorf("context %q not found in kubeconfig", contextName)}
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(f.rules, overrides)

	restCfg, err := cc.ClientConfig()
	if err != nil {
		return &cached{err: fmt.Errorf("build client config for %q: %w", contextName, err)}
	}
	restCfg.Timeout = f.timeout
	restCfg.QPS = DefaultQPS
	restCfg.Burst = DefaultBurst
	restCfg.UserAgent = UserAgent
	// Never block the UI on an interactive auth prompt (exec plugins that want
	// a TTY): the TUI owns the terminal.
	restCfg.WarningHandler = rest.NoWarnings{}

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return &cached{restConfig: restCfg, err: fmt.Errorf("build client for %q: %w", contextName, err)}
	}
	return &cached{restConfig: restCfg, clientset: cs}
}

// Invalidate drops the cached clients for a context, so the next call rebuilds
// them (used after the kubeconfig is reloaded).
func (f *Factory) Invalidate(contextName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cache, contextName)
}

// Reset drops every cached client.
func (f *Factory) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache = make(map[string]*cached)
}

// Timeout reports the per-request timeout in use.
func (f *Factory) Timeout() time.Duration { return f.timeout }
