package opensocial

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("github.com/habitat-network/habitat/internal/opensocial")

// Span attribute keys for community operations. Org, space and caller use
// the shared keys in internal/telemetry.
const (
	// memberKey is the DID of the member an operation acts on, as distinct
	// from the caller (enduser.id) who asked for it.
	memberKey = attribute.Key("habitat.opensocial.member")
	// rolesKey lists the role rkeys an operation reads or sets.
	rolesKey = attribute.Key("habitat.opensocial.roles")
	// roleKey is the rkey of the one role an operation acts on.
	roleKey = attribute.Key("habitat.opensocial.role")
	// actionKey is the permissions action being checked.
	actionKey = attribute.Key("habitat.opensocial.action")
	// allowedKey is the outcome of a permission or access check.
	allowedKey = attribute.Key("habitat.opensocial.allowed")
	// inviteIDKey is an invite's id.
	inviteIDKey = attribute.Key("habitat.opensocial.invite.id")
	// clientIDKey is the OAuth client id of an app being granted or checked
	// for access.
	clientIDKey = attribute.Key("habitat.opensocial.client_id")
	// mcpServerKey is the rkey of an org's MCP server record.
	mcpServerKey = attribute.Key("habitat.opensocial.mcp_server")
)

func memberAttr(did syntax.DID) attribute.KeyValue {
	return memberKey.String(did.String())
}
