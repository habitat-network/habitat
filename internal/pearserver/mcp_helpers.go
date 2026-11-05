package pearserver

import (
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

func mcpServerToAPI(s *mcpgateway.Server) habitat.NetworkHabitatMcpDefsServer {
	return habitat.NetworkHabitatMcpDefsServer{
		Id:          string(s.Name),
		Name:        string(s.Name),
		Description: s.Description,
		AuthType:    string(s.AuthType),
		Url:         s.URL,
	}
}
