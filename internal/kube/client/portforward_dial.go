package client

import (
	"context"
	"net/http"
	"net/url"

	"k8s.io/apimachinery/pkg/util/httpstream" //nolint:staticcheck // client-go v0.37 portforward and spdy still require this interface
	"k8s.io/client-go/transport/spdy"
)

type contextForwardDialer struct {
	ctx       context.Context
	transport http.RoundTripper
	upgrader  spdy.Upgrader
	url       *url.URL
}

func (d *contextForwardDialer) Dial(protocols ...string) (httpstream.Connection, string, error) {
	req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, d.url.String(), nil)
	if err != nil {
		return nil, "", err
	}
	return spdy.Negotiate(d.upgrader, &http.Client{Transport: d.transport}, req, protocols...)
}
