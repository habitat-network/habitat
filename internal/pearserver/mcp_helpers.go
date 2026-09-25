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

// isMcpConfigError reports whether err is a validation error from
// mcpgateway that should be reported as an invalid request.
func isMcpConfigError(err error) bool {
	return errors.Is(err, mcpgateway.ErrInvalidServerName) ||
		errors.Is(err, mcpgateway.ErrServerNameTaken) ||
		errors.Is(err, mcpgateway.ErrInvalidServerURL) ||
		errors.Is(err, mcpgateway.ErrInvalidAuthType)
}
