package pearserver

import (
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func mcpServerToAPI(s *opensocial.McpServer) habitat.NetworkHabitatMcpDefsServer {
	return habitat.NetworkHabitatMcpDefsServer{
		Id:          string(s.ID),
		Name:        s.Name,
		Description: s.Description,
	}
}
