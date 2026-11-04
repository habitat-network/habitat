package pearserver

import (
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/llmgateway"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func llmConnectionToAPI(c *llmgateway.Connection) habitat.NetworkHabitatLlmDefsConnection {
	return habitat.NetworkHabitatLlmDefsConnection{
		Id:          string(c.ID),
		Name:        c.Name,
		Description: c.Description,
		Provider:    string(c.Provider),
		Model:       c.Model,
		BaseUrl:     c.BaseURL,
		HasApiKey:   c.HasAPIKey,
	}
}

// llmConfigureAction is the action required to configure an org's LLM
// connections. They have no action of their own; like search, they are part
// of configuring the community.
const llmConfigureAction = opensocial.ActionCommunityConfigure
