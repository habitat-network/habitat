package mcpgateway

import "errors"

// Preset names a known MCP connector. Adding one uses the connector's fixed
// URL and auth type, and the OAuth client pear holds for it, which is shared by
// every org, so the admin supplies none of them.
type Preset string

const (
	PresetGoogleDrive Preset = "google-drive"
	PresetSlack       Preset = "slack"
	PresetGitHub      Preset = "github"
)

// presetServers are the fixed parts of each preset. None of these providers
// support Dynamic Client Registration, so each needs a client that pear's
// operator pre-registers (see PresetClient).
var presetServers = map[Preset]struct {
	URL      string
	AuthType AuthType
}{
	// https://developers.google.com/workspace/drive/api/guides/configure-mcp-server
	PresetGoogleDrive: {URL: "https://drivemcp.googleapis.com/mcp/v1", AuthType: AuthTypeOAuth},
	// https://docs.slack.dev/ai/slack-mcp-server/
	PresetSlack: {URL: "https://mcp.slack.com/mcp", AuthType: AuthTypeOAuth},
	// https://github.com/github/github-mcp-server
	PresetGitHub: {URL: "https://api.githubcopilot.com/mcp/", AuthType: AuthTypeOAuth},
}

// PresetClient is the OAuth client pear holds for a Preset, pre-registered
// with the provider with pear's MCP OAuth callback as its redirect URI.
type PresetClient struct {
	ID     string
	Secret string
}

// WithPresetClients gives the store the OAuth clients for its presets. A
// preset with no client (or an empty ID) can't be added.
func WithPresetClients(clients map[Preset]PresetClient) Option {
	return func(s *store) { s.presetClients = clients }
}

// Option configures optional behavior of a Store.
type Option func(*store)

var (
	// ErrUnknownPreset is returned when adding a server with a preset that
	// pear doesn't know.
	ErrUnknownPreset = errors.New("unknown mcp preset")
	// ErrPresetNotConfigured is returned when adding a known preset that
	// this pear has no OAuth client for.
	ErrPresetNotConfigured = errors.New("this mcp preset isn't configured on this server")
	// ErrPresetConflict is returned when a preset is combined with fields it
	// sets itself.
	ErrPresetConflict = errors.New(
		"a preset can't be combined with url, auth type, client id or client secret",
	)
)

// applyPreset fills ns's URL, auth type and client from its preset, if it
// has one.
func (s *store) applyPreset(ns *NewServer) error {
	if ns.Preset == "" {
		return nil
	}
	if ns.URL != "" || ns.AuthType != "" || ns.ClientID != "" || ns.ClientSecret != "" {
		return ErrPresetConflict
	}
	fixed, ok := presetServers[ns.Preset]
	if !ok {
		return ErrUnknownPreset
	}
	client := s.presetClients[ns.Preset]
	if client.ID == "" {
		return ErrPresetNotConfigured
	}
	ns.URL, ns.AuthType = fixed.URL, fixed.AuthType
	ns.ClientID, ns.ClientSecret = client.ID, client.Secret
	return nil
}
