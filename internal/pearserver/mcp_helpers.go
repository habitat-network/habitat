package pearserver

import (
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

func mcpServerToAPI(s *mcpgateway.Server) habitat.NetworkHabitatMcpDefsServer {
	return habitat.NetworkHabitatMcpDefsServer{
		Id:          string(s.ID),
		Name:        s.Name,
		Url:         s.URL,
		Description: s.Description,
		AuthType:    string(s.AuthType),
	}
}
