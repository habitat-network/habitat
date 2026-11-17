package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yosida95/uritemplate/v3"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// This file multiplexes the resources and prompts of the caller's connected
// MCP servers into this server's own, the way tools.go does for their tools:
//
//   - A connected server's resources are listed under a pear URI naming the
//     server (see connectedResourceURI), and their names are namespaced like
//     tools (see namespaceTool). Reading that URI reads the server's own.
//   - Its resource templates are listed with their own URI templates, since a
//     client expands those itself and a wrapped template couldn't be expanded
//     into a URI pear can unwrap. Reading a URI that isn't pear's goes to the
//     first connected server that claims it: one of whose templates matches it,
//     or which lists it (as its tools' results carry it). A URI no server
//     claims is never sent to any of them.
//   - Its prompts are namespaced like tools, and getting one gets the server's.
//
// Pear opens a session per request rather than holding one per connected
// server, so it relays neither resource subscriptions nor list_changed
// notifications, and doesn't advertise them.

// connectedResourceURIScheme is the scheme of the pear URIs of connected
// servers' resources, which look like
// habitat-mcp:///?server=<server name>&uri=<the server's own uri>.
const connectedResourceURIScheme = "habitat-mcp"

// connectedResourceURI is the pear URI of serverName's resource at uri.
func connectedResourceURI(serverName, uri string) string {
	q := url.Values{"server": {serverName}, "uri": {uri}}
	return (&url.URL{
		Scheme: connectedResourceURIScheme, Path: "/", RawQuery: q.Encode(),
	}).String()
}

// parseConnectedResourceURI is the inverse of connectedResourceURI. ok is
// false if raw isn't a pear URI of a connected server's resource.
func parseConnectedResourceURI(raw string) (serverName, uri string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != connectedResourceURIScheme {
		return "", "", false
	}
	q := u.Query()
	serverName, uri = q.Get("server"), q.Get("uri")
	if serverName == "" || uri == "" {
		return "", "", false
	}
	return serverName, uri, true
}

// templateMatches reports whether uri is one of template's expansions. A
// template that doesn't start with a literal (one that's all variable, say
// "{+uri}") matches nothing, since it would claim every URI.
func templateMatches(template, uri string) bool {
	if template == "" || strings.HasPrefix(template, "{") {
		return false
	}
	tmpl, err := uritemplate.New(template)
	if err != nil {
		return false
	}
	return tmpl.Regexp().MatchString(uri)
}

// callerOf is the authenticated caller of req, if any.
func callerOf(req mcp.Request) (syntax.DID, bool) {
	extra := req.GetExtra()
	if extra == nil || extra.TokenInfo == nil || extra.TokenInfo.UserID == "" {
		return "", false
	}
	return syntax.DID(extra.TokenInfo.UserID), true
}

// connectedCapabilities reports which of resources and prompts a session's
// server offers.
func connectedCapabilities(session *mcp.ClientSession) (resources, prompts bool) {
	init := session.InitializeResult()
	if init == nil || init.Capabilities == nil {
		return false, false
	}
	return init.Capabilities.Resources != nil, init.Capabilities.Prompts != nil
}

// connectedListing is what one connected server lists: its resources,
// templates and prompts, unnamespaced.
type connectedListing struct {
	resources []*mcp.Resource
	templates []*mcp.ResourceTemplate
	prompts   []*mcp.Prompt
}

// listingKind selects what listConnected lists.
type listingKind int

const (
	listResources listingKind = 1 << iota
	listTemplates
	listPrompts
)

// listConnected lists what kinds selects of each of servers, in the same
// order as servers, concurrently and bounded like listToolsForServers. A
// server that can't be listed is logged and lists nothing, and one that
// doesn't offer resources or prompts isn't asked for them.
func listConnected(
	ctx context.Context,
	connectedServers mcpgateway.ConnectedServers,
	caller syntax.DID,
	servers []*mcpgateway.Server,
	kinds listingKind,
) []connectedListing {
	out := make([]connectedListing, len(servers))
	mcpgateway.ForEach(ctx, servers, mcpgateway.DefaultServerTimeout,
		func(ctx context.Context, i int, server *mcpgateway.Server) {
			var listing connectedListing
			err := mcpgateway.WithSession(ctx, connectedServers, caller, server,
				func(ctx context.Context, session *mcp.ClientSession) error {
					hasResources, hasPrompts := connectedCapabilities(session)
					if hasResources && kinds&listResources != 0 {
						for r, err := range session.Resources(ctx, nil) {
							if err != nil {
								return fmt.Errorf("list resources: %w", err)
							}
							listing.resources = append(listing.resources, r)
						}
					}
					if hasResources && kinds&listTemplates != 0 {
						for tmpl, err := range session.ResourceTemplates(ctx, nil) {
							if err != nil {
								return fmt.Errorf("list resource templates: %w", err)
							}
							listing.templates = append(listing.templates, tmpl)
						}
					}
					if hasPrompts && kinds&listPrompts != 0 {
						for p, err := range session.Prompts(ctx, nil) {
							if err != nil {
								return fmt.Errorf("list prompts: %w", err)
							}
							listing.prompts = append(listing.prompts, p)
						}
					}
					return nil
				})
			if err != nil {
				slog.WarnContext(
					ctx, "mcp: listing connected server",
					"server", string(server.Name), "err", err,
				)
				return
			}
			out[i] = listing
		})
	return out
}

// mergeConnectedResourcesMiddleware merges the resources, resource templates
// and prompts of the caller's connected MCP servers in org into this server's
// own listings of them. Connected servers' entries all come on the first
// page, so later pages (of this server's own) don't repeat them.
func mergeConnectedResourcesMiddleware(
	connectedServers mcpgateway.ConnectedServers,
	org syntax.DID,
) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if err != nil {
				return result, err
			}
			caller, ok := callerOf(req)
			if !ok {
				return result, nil
			}
			servers := func() []*mcpgateway.Server {
				return listConnectedServers(ctx, connectedServers, caller, org)
			}
			switch res := result.(type) {
			case *mcp.ListResourcesResult:
				params, _ := req.GetParams().(*mcp.ListResourcesParams)
				if params != nil && params.Cursor != "" {
					return result, nil
				}
				srvs := servers()
				for i, l := range listConnected(ctx, connectedServers, caller, srvs, listResources) {
					name := string(srvs[i].Name)
					for _, r := range l.resources {
						wrapped := *r
						wrapped.Name = namespaceTool(name, r.Name)
						wrapped.URI = connectedResourceURI(name, r.URI)
						res.Resources = append(res.Resources, &wrapped)
					}
				}
			case *mcp.ListResourceTemplatesResult:
				params, _ := req.GetParams().(*mcp.ListResourceTemplatesParams)
				if params != nil && params.Cursor != "" {
					return result, nil
				}
				srvs := servers()
				for i, l := range listConnected(ctx, connectedServers, caller, srvs, listTemplates) {
					for _, tmpl := range l.templates {
						namespaced := *tmpl
						namespaced.Name = namespaceTool(string(srvs[i].Name), tmpl.Name)
						res.ResourceTemplates = append(res.ResourceTemplates, &namespaced)
					}
				}
			case *mcp.ListPromptsResult:
				params, _ := req.GetParams().(*mcp.ListPromptsParams)
				if params != nil && params.Cursor != "" {
					return result, nil
				}
				srvs := servers()
				for i, l := range listConnected(ctx, connectedServers, caller, srvs, listPrompts) {
					for _, p := range l.prompts {
						namespaced := *p
						namespaced.Name = namespaceTool(string(srvs[i].Name), p.Name)
						res.Prompts = append(res.Prompts, &namespaced)
					}
				}
			}
			return result, nil
		}
	}
}

// proxyConnectedResourcesMiddleware routes resources/read and prompts/get for
// connected servers' resources and prompts to the caller's connected server
// that owns them (see the top of this file). Anything else is left to this
// server's own dispatch, which reports it as not found.
func proxyConnectedResourcesMiddleware(
	connectedServers mcpgateway.ConnectedServers,
	org syntax.DID,
) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			switch params := req.GetParams().(type) {
			case *mcp.ReadResourceParams:
				caller, ok := callerOf(req)
				if !ok || isOwnResourceURI(params.URI) {
					return next(ctx, method, req)
				}
				servers := listConnectedServers(ctx, connectedServers, caller, org)
				if serverName, uri, ok := parseConnectedResourceURI(params.URI); ok {
					server := serverNamed(servers, serverName)
					if server == nil {
						return nil, mcp.ResourceNotFoundError(params.URI)
					}
					return readConnectedResource(
						ctx, connectedServers, caller, server, uri, params.URI,
					)
				}
				server := claimingServer(ctx, connectedServers, caller, servers, params.URI)
				if server == nil {
					return next(ctx, method, req)
				}
				return readConnectedResource(
					ctx, connectedServers, caller, server, params.URI, params.URI,
				)
			case *mcp.GetPromptParams:
				serverName, promptName, ok := splitNamespacedTool(params.Name)
				if !ok {
					return next(ctx, method, req)
				}
				caller, ok := callerOf(req)
				if !ok {
					return nil, fmt.Errorf("missing authenticated caller")
				}
				servers := listConnectedServers(ctx, connectedServers, caller, org)
				server := serverNamed(servers, serverName)
				if server == nil {
					return next(ctx, method, req)
				}
				return getConnectedPrompt(ctx, connectedServers, caller, server, promptName, params)
			}
			return next(ctx, method, req)
		}
	}
}

// isOwnResourceURI reports whether uri is one of this server's own resources.
func isOwnResourceURI(uri string) bool {
	return strings.HasPrefix(uri, recordURIScheme+":") || strings.HasPrefix(uri, blobURIScheme+":")
}

func serverNamed(servers []*mcpgateway.Server, name string) *mcpgateway.Server {
	for _, server := range servers {
		if string(server.Name) == name {
			return server
		}
	}
	return nil
}

// claimingServer is the first of servers that claims uri, by a template that
// matches it or by listing it, or nil if none does.
func claimingServer(
	ctx context.Context,
	connectedServers mcpgateway.ConnectedServers,
	caller syntax.DID,
	servers []*mcpgateway.Server,
	uri string,
) *mcpgateway.Server {
	listings := listConnected(ctx, connectedServers, caller, servers, listResources|listTemplates)
	for i, l := range listings {
		for _, tmpl := range l.templates {
			if templateMatches(tmpl.URITemplate, uri) {
				return servers[i]
			}
		}
		for _, r := range l.resources {
			if r.URI == uri {
				return servers[i]
			}
		}
	}
	return nil
}

// readConnectedResource reads server's resource at uri as caller. Contents
// for uri itself are relabeled with requestedURI, the URI the client asked
// for, which differs when it's a pear URI.
func readConnectedResource(
	ctx context.Context,
	connectedServers mcpgateway.ConnectedServers,
	caller syntax.DID,
	server *mcpgateway.Server,
	uri, requestedURI string,
) (mcp.Result, error) {
	var res *mcp.ReadResourceResult
	err := mcpgateway.WithSession(ctx, connectedServers, caller, server,
		func(ctx context.Context, session *mcp.ClientSession) (err error) {
			res, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
			return err
		})
	if err != nil {
		// Avoid returning a typed-nil *ReadResourceResult as a non-nil mcp.Result.
		return nil, err
	}
	for _, c := range res.Contents {
		if c != nil && c.URI == uri {
			c.URI = requestedURI
		}
	}
	return res, nil
}

// getConnectedPrompt gets server's prompt promptName as caller, with params'
// arguments.
func getConnectedPrompt(
	ctx context.Context,
	connectedServers mcpgateway.ConnectedServers,
	caller syntax.DID,
	server *mcpgateway.Server,
	promptName string,
	params *mcp.GetPromptParams,
) (mcp.Result, error) {
	var res *mcp.GetPromptResult
	err := mcpgateway.WithSession(ctx, connectedServers, caller, server,
		func(ctx context.Context, session *mcp.ClientSession) (err error) {
			res, err = session.GetPrompt(ctx, &mcp.GetPromptParams{
				Name: promptName, Arguments: params.Arguments,
			})
			return err
		})
	if err != nil {
		return nil, err
	}
	return res, nil
}
