package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugImageResolution(t *testing.T) {
	for _, tt := range []struct{ mode, public, mirrored string }{
		{"toolbox", "busybox:1.37.0", "library/busybox:1.37.0"},
		{"dns", "busybox:1.37.0", "library/busybox:1.37.0"},
		{"ephemeral", "busybox:1.37.0", "library/busybox:1.37.0"},
		{"http", "curlimages/curl:8.21.0", "curlimages/curl:8.21.0"},
		{"tcp", "nicolaka/netshoot:v0.16", "nicolaka/netshoot:v0.16"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			d := Debug{}
			if got := d.Image(tt.mode, false); got != tt.public {
				t.Fatalf("public = %q", got)
			}
			if got := d.Image(tt.mode, true); got != "" {
				t.Fatalf("offline fallback = %q", got)
			}
			for _, prefix := range []string{"registry.internal", "registry.internal:5000/dockerhub/", "localhost:5000", "[::1]:5000/cache"} {
				d.RegistryMirror = prefix
				for _, offline := range []bool{true, false} {
					want := strings.TrimRight(prefix, "/") + "/" + tt.mirrored
					if got := d.Image(tt.mode, offline); got != want {
						t.Fatalf("image = %q, want %q", got, want)
					}
				}
			}
			// Full custom references, including digests and unqualified preloaded
			// images, must stay exact even when a default mirror is configured.
			for _, image := range []string{"local-toolbox:1", "other.internal/team/toolbox@sha256:" + strings.Repeat("a", 64)} {
				d.ToolboxImage, d.CurlImage, d.NetworkImage = image, image, image
				if got := d.Image(tt.mode, true); got != image {
					t.Fatalf("override rewritten: %q", got)
				}
			}
		})
	}
}

func TestRegistryMirrorConfigValidation(t *testing.T) {
	for _, mirror := range []string{"registry.internal", "registry.internal:5000/dockerhub/", "localhost", "registry:5000/cache", "127.0.0.1:5000", "[2001:db8::1]:5000/team/cache", "mirror.internal/team_name/repo--cache"} {
		t.Run(mirror, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			write(t, path, "airGapped: true\ndebug:\n  registryMirror: '"+mirror+"'\n")
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Debug.RegistryMirror != mirror || cfg.Debug.Image("toolbox", true) == "" {
				t.Fatal("mirror was not loaded")
			}
		})
	}
	for _, mirror := range []string{"https://registry.internal", "user:password@registry.internal", "registry", "registry.internal/cache:latest", "registry.internal/cache@sha256:abc", "registry.internal:0", "registry.internal:65536", "registry.internal:", "registry.internal/cache?query=1", "registry.internal/cache#tag", "registry.internal/../cache", "registry.internal/a//b", "registry.internal/CACHE", "registry.internal/a%2fb", "registry.internal/has space", "registry..internal/cache"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		write(t, path, "airGapped: true\ndebug:\n  registryMirror: '"+mirror+"'\n")
		if _, err := Load(path); err == nil {
			t.Errorf("accepted invalid registry mirror %q", mirror)
		}
	}
}
