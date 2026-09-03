// Package config loads Correlux's user configuration.
//
// Configuration is optional: Correlux must run correctly with no config file at
// all. A missing file is never an error; a malformed file is reported to the
// caller so the UI can surface it instead of silently ignoring user intent.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Theme selects the colour scheme.
type Theme string

const (
	ThemeAuto  Theme = "auto"
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

// Config is the whole of Correlux's user configuration. Every field has a
// usable zero value after Defaults() has been applied.
type Config struct {
	// Theme is "auto", "dark" or "light".
	Theme Theme `json:"theme"`

	// Startup pins the context/namespace Correlux opens with. Empty means "use
	// whatever the kubeconfig says".
	Startup Startup `json:"startup"`

	// Safety controls how aggressively Correlux guards mutating actions.
	Safety Safety `json:"dangerousActions"`

	// Refresh controls the timed reload of whatever is on screen.
	Refresh Refresh `json:"refresh"`

	// Fleet lists the contexts the fleet overview covers, by name.
	//
	// It is empty by default and stays that way until somebody fills it in:
	// Correlux must never discover, on its own, that opening it authenticated
	// against every production cluster the user has credentials for.
	Fleet []string `json:"fleet"`

	// FleetGroups are named, independent views over kubeconfig contexts. They
	// let operators keep production, staging, regions or teams separate without
	// authenticating against clusters outside the selected group. The legacy
	// Fleet field remains supported as the implicit "default" group.
	FleetGroups []FleetGroup `json:"fleetGroups"`

	// Keybindings maps an action name to a keystroke, overriding the defaults.
	Keybindings map[string]string `json:"keybindings"`

	// SourcePath records where this config was read from ("" if defaults).
	SourcePath string `json:"-"`
}

// FleetGroup is one explicitly named set of kubeconfig contexts.
type FleetGroup struct {
	Name     string   `json:"name"`
	Contexts []string `json:"contexts"`
}

// Startup pins the initial context and namespace.
type Startup struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
}

// Refresh controls Correlux's timed reload.
//
// It is off by default and stays off until the user asks for it: a tool that
// polls a production API server every few seconds without being told to is a
// tool that gets banned from production.
type Refresh struct {
	// Auto starts Correlux with the timed reload already running.
	Auto bool `json:"auto"`
	// Every is the interval as a duration string ("10s", "1m"). Anything
	// shorter than MinInterval is raised to it.
	Every string `json:"every"`
}

// Refresh interval bounds.
const (
	// DefaultRefreshInterval is slow enough to be unnoticeable on the API
	// server and fast enough to watch a rollout.
	DefaultRefreshInterval = 10 * time.Second
	// MinRefreshInterval stops a mistyped config from turning Correlux into a
	// load generator.
	MinRefreshInterval = 2 * time.Second
)

// Interval reports how often to reload. A malformed value is reported rather
// than silently replaced, so the user learns their config was not honoured.
func (r Refresh) Interval() (time.Duration, error) {
	value := strings.TrimSpace(r.Every)
	if value == "" {
		return DefaultRefreshInterval, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return DefaultRefreshInterval, fmt.Errorf("invalid refresh interval %q: %w", r.Every, err)
	}
	if d < MinRefreshInterval {
		return MinRefreshInterval, nil
	}
	return d, nil
}

// Safety describes how production contexts are recognised and treated.
type Safety struct {
	// ProductionConfirmation requires a stronger confirmation for mutating
	// actions in contexts classified as production.
	ProductionConfirmation bool `json:"productionConfirmation"`

	// ProductionPatterns are regular expressions matched (case-insensitively)
	// against the context name, cluster name and API server URL.
	ProductionPatterns []string `json:"productionPatterns"`

	// ProductionContexts are context names that are always production,
	// regardless of the patterns above.
	ProductionContexts []string `json:"productionContexts"`
}

// DefaultProductionPatterns classify a context as production by name. They are
// deliberately conservative: a false positive costs one extra confirmation, a
// false negative costs an outage.
var DefaultProductionPatterns = []string{
	`(^|[-_./])(prod|prd|production|live)([-_./]|$)`,
}

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		Theme: ThemeAuto,
		Safety: Safety{
			ProductionConfirmation: true,
			ProductionPatterns:     append([]string(nil), DefaultProductionPatterns...),
		},
		Keybindings: map[string]string{},
	}
}

// Dir returns the OS-appropriate configuration directory for Correlux:
//
//	Linux/BSD: $XDG_CONFIG_HOME/correlux   (default ~/.config/correlux)
//	macOS:     ~/.config/correlux          (XDG_CONFIG_HOME honoured if set)
//	Windows:   %APPDATA%\correlux
func Dir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("CORRELUX_CONFIG_DIR")); v != "" {
		return v, nil
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "correlux"), nil
		}
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "correlux"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "correlux"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "correlux"), nil
}

// Path returns the full path of the configuration file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads the configuration from path. A missing file yields Default() with
// no error. An unreadable or malformed file yields Default() *and* an error, so
// callers can keep running while telling the user what went wrong.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.SourcePath = path
	cfg.normalise()
	return cfg, nil
}

// LoadDefault loads the configuration from the OS-appropriate location.
func LoadDefault() (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	return Load(path)
}

func (c *Config) normalise() {
	switch c.Theme {
	case ThemeDark, ThemeLight, ThemeAuto:
	case "":
		c.Theme = ThemeAuto
	default:
		c.Theme = ThemeAuto
	}
	if len(c.Safety.ProductionPatterns) == 0 {
		c.Safety.ProductionPatterns = append([]string(nil), DefaultProductionPatterns...)
	}
	if c.Keybindings == nil {
		c.Keybindings = map[string]string{}
	}
}
