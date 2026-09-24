package pearserver

import (
	"errors"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

func mcpServerToAPI(s *mcpgateway.Server) habitat.NetworkHabitatMcpDefsServer {
	return habitat.NetworkHabitatMcpDefsServer{
		Id:          string(s.ID),
		Name:        s.Name,
		Description: s.Description,
		AuthType:    string(s.AuthType),
	}
}

// mcpHeadersFromAPI converts API headers to the map mcpgateway takes,
// preserving nil (meaning "unset") for a nil slice.
func mcpHeadersFromAPI(headers []habitat.NetworkHabitatMcpDefsHeader) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for _, h := range headers {
		out[h.Name] = h.Value
	}
	return out
}

// isMcpConfigError reports whether err is a validation error from
// mcpgateway that should be reported as an invalid request.
func isMcpConfigError(err error) bool {
	return errors.Is(err, mcpgateway.ErrInvalidServerName) ||
		errors.Is(err, mcpgateway.ErrServerNameTaken) ||
		errors.Is(err, mcpgateway.ErrInvalidServerURL) ||
		errors.Is(err, mcpgateway.ErrInvalidHeaderName)
}
