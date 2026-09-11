package config

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Image resolves a debug image. Explicit references are never rewritten. A
// mirror prefixes Docker Hub repository paths, including the library namespace.
// Without an override or mirror, air-gapped sessions require an entered image.
func (d Debug) Image(mode string, airGapped bool) string {
	var explicit, fallback, repository string
	switch mode {
	case "toolbox", "dns", "ephemeral":
		explicit, fallback, repository = d.ToolboxImage, "busybox:1.37.0", "library/busybox:1.37.0"
	case "http":
		explicit, fallback, repository = d.CurlImage, "curlimages/curl:8.21.0", "curlimages/curl:8.21.0"
	case "tcp":
		explicit, fallback, repository = d.NetworkImage, "nicolaka/netshoot:v0.16", "nicolaka/netshoot:v0.16"
	default:
		return ""
	}
	if explicit != "" {
		return explicit
	}
	if d.RegistryMirror != "" {
		return strings.TrimRight(d.RegistryMirror, "/") + "/" + repository
	}
	if airGapped {
		return ""
	}
	return fallback
}

var mirrorRepositoryPart = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
var mirrorHostLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

func validateRegistryMirror(value string) error {
	if value == "" {
		return nil
	}
	invalid := errors.New("debug.registryMirror must be a registry host[:port] with an optional repository path, without a URL scheme, credentials, tag or digest")
	if strings.ContainsAny(value, " \t\r\n%?#@\\") || strings.Contains(value, "://") {
		return invalid
	}
	u, err := url.Parse("//" + strings.TrimRight(value, "/"))
	if err != nil || u.Host == "" || u.User != nil {
		return invalid
	}
	host := u.Hostname()
	if net.ParseIP(host) == nil {
		for _, label := range strings.Split(host, ".") {
			if !mirrorHostLabel.MatchString(label) {
				return invalid
			}
		}
	}
	// Otherwise Kubernetes would interpret the first component as a Docker Hub
	// organization, defeating the explicit registry selection.
	if host != "localhost" && !strings.ContainsAny(host, ".:") && u.Port() == "" {
		return invalid
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return invalid
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return invalid
	}
	if u.Path != "" {
		for _, part := range strings.Split(strings.TrimPrefix(u.Path, "/"), "/") {
			if !mirrorRepositoryPart.MatchString(part) {
				return invalid
			}
		}
	}
	return nil
}
