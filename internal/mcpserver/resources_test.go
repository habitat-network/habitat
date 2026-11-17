package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// The fake downstream server's resources (see addFakeResourcesAndPrompts).
const (
	fakeReadmeURI      = "memo://readme"
	fakeNoteTemplate   = "memo://notes/{id}"
	fakePromptName     = "greet"
	fakeReadmeContents = "read me"
)

// addFakeResourcesAndPrompts gives srv a "memo://readme" resource, a
// "memo://notes/{id}" resource template whose resources read as "note <id>",
// and a "greet" prompt greeting its "name" argument.
func addFakeResourcesAndPrompts(srv *mcp.Server) {
	srv.AddResource(&mcp.Resource{
		Name: "readme", URI: fakeReadmeURI, MIMEType: "text/plain",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/plain", Text: fakeReadmeContents},
		}}, nil
	})
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "note", URITemplate: fakeNoteTemplate, MIMEType: "text/plain",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id := strings.TrimPrefix(req.Params.URI, "memo://notes/")
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/plain", Text: "note " + id},
		}}, nil
	})
	srv.AddPrompt(&mcp.Prompt{
		Name:      fakePromptName,
		Arguments: []*mcp.PromptArgument{{Name: "name", Required: true}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{
			Role:    "user",
			Content: &mcp.TextContent{Text: "Hello, " + req.Params.Arguments["name"]},
		}}}, nil
	})
}

// connectToFakes connects the caller to a fake downstream server under each
// of names in testOrg, and returns a session with pear's MCP server.
func connectToFakes(t *testing.T, names ...string) *mcp.ClientSession {
	t.Helper()
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	for _, name := range names {
		servers.connect(caller, testOrg, mcpgateway.ServerName(name), newFakeMCPServer(t).URL, nil)
	}
	httpServer := newConnectedServerTestServer(t, servers)
	session, err := connectAs(t, t.Context(), httpServer.URL, caller.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestMCPServerAdvertisesResourcesAndPrompts(t *testing.T) {
	session := connectToFakes(t, "notes")

	caps := session.InitializeResult().Capabilities
	require.NotNil(t, caps.Resources)
	require.NotNil(t, caps.Prompts)
	// Pear doesn't relay connected servers' notifications.
	require.False(t, caps.Resources.Subscribe)
	require.False(t, caps.Resources.ListChanged)
	require.False(t, caps.Prompts.ListChanged)
}

// Each connected server's resources are listed under a pear URI naming the
// server, and reading that URI reads the server's own resource.
func TestMCPServerResources_ListsAndReadsConnectedResources(t *testing.T) {
	ctx := t.Context()
	session := connectToFakes(t, "notes", "wiki")

	listed, err := session.ListResources(ctx, nil)
	require.NoError(t, err)
	byName := map[string]*mcp.Resource{}
	for _, r := range listed.Resources {
		byName[r.Name] = r
	}
	require.Contains(t, byName, "notes:readme")
	require.Contains(t, byName, "wiki:readme")
	notes := byName["notes:readme"]
	require.NotEqual(t, notes.URI, byName["wiki:readme"].URI)
	require.Equal(t, "text/plain", notes.MIMEType)

	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: notes.URI})
	require.NoError(t, err)
	require.Len(t, read.Contents, 1)
	require.Equal(t, fakeReadmeContents, read.Contents[0].Text)
	require.Equal(t, notes.URI, read.Contents[0].URI)
}

// Connected servers' templates are listed as they are, so a client expands
// them into the server's own URIs, which pear routes to the server whose
// template they match.
func TestMCPServerResources_ReadsTemplatedConnectedResources(t *testing.T) {
	ctx := t.Context()
	session := connectToFakes(t, "notes")

	listed, err := session.ListResourceTemplates(ctx, nil)
	require.NoError(t, err)
	var templates []string
	for _, tmpl := range listed.ResourceTemplates {
		templates = append(templates, tmpl.Name+" "+tmpl.URITemplate)
	}
	require.Contains(t, templates, "notes:note "+fakeNoteTemplate)
	require.Contains(t, templates, "record "+recordResourceTemplate)

	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "memo://notes/42"})
	require.NoError(t, err)
	require.Equal(t, "note 42", read.Contents[0].Text)

	// A server's own listed URI, as its tools' results carry it, is routed
	// to it too.
	read, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: fakeReadmeURI})
	require.NoError(t, err)
	require.Equal(t, fakeReadmeContents, read.Contents[0].Text)
}

// A URI no connected server claims isn't sent to any of them.
func TestMCPServerResources_UnknownURINotFound(t *testing.T) {
	ctx := t.Context()
	session := connectToFakes(t, "notes")

	for _, uri := range []string{
		"other://thing",
		connectedResourceURI("missing", fakeReadmeURI),
	} {
		_, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		require.Error(t, err, uri)
	}
}

func TestMCPServerPrompts_ListsAndGetsConnectedPrompts(t *testing.T) {
	ctx := t.Context()
	session := connectToFakes(t, "notes")

	listed, err := session.ListPrompts(ctx, nil)
	require.NoError(t, err)
	require.Len(t, listed.Prompts, 1)
	require.Equal(t, "notes:"+fakePromptName, listed.Prompts[0].Name)
	require.Equal(t, "name", listed.Prompts[0].Arguments[0].Name)

	got, err := session.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "notes:" + fakePromptName, Arguments: map[string]string{"name": "Ada"},
	})
	require.NoError(t, err)
	require.Equal(t, "Hello, Ada", got.Messages[0].Content.(*mcp.TextContent).Text)

	_, err = session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "missing:" + fakePromptName})
	require.Error(t, err)
}

func TestConnectedResourceURIRoundTrips(t *testing.T) {
	for _, upstream := range []string{
		fakeReadmeURI,
		"https://example.com/a b?x=1&y=2#frag",
		"file:///tmp/a%2Fb",
	} {
		wrapped := connectedResourceURI("my-server", upstream)
		server, got, ok := parseConnectedResourceURI(wrapped)
		require.True(t, ok, wrapped)
		require.Equal(t, "my-server", server)
		require.Equal(t, upstream, got)
	}
	_, _, ok := parseConnectedResourceURI(fakeReadmeURI)
	require.False(t, ok)
	_, _, ok = parseConnectedResourceURI(connectedResourceURIScheme + ":///?uri=x")
	require.False(t, ok)
}

func TestTemplateMatches(t *testing.T) {
	require.True(t, templateMatches(fakeNoteTemplate, "memo://notes/42"))
	require.True(t, templateMatches("memo://fixed", "memo://fixed"))
	require.False(t, templateMatches("memo://fixed", "memo://fixed/more"))
	require.False(t, templateMatches(fakeNoteTemplate, "memo://other/42"))
	// A template that's all variable would claim every URI.
	require.False(t, templateMatches("{+uri}", "memo://notes/42"))
}
