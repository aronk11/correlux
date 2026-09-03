package discovery

import (
	"errors"

	"k8s.io/client-go/discovery"
)

// asGroupDiscoveryFailed unwraps the partial-discovery error. It is its own
// function because the type is easy to miss and getting it wrong turns "one
// aggregated API is down" into "Correlux cannot show you anything".
func asGroupDiscoveryFailed(err error, target **discovery.ErrGroupDiscoveryFailed) bool {
	return errors.As(err, target)
}
