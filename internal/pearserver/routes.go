package pearserver

import "net/http"

// registerRoutes wires all handlers onto the internal router.
func (p *PearServer) registerRoutes() {
	// Spaces
	p.router.HandleFunc("/xrpc/network.habitat.space.listSpaces", p.ListSpaces)
	p.router.HandleFunc("/xrpc/network.habitat.space.listRepos", p.ListRepos)
	p.router.HandleFunc("/xrpc/network.habitat.space.putRecord", p.PutRecord)
	p.router.HandleFunc("/xrpc/network.habitat.space.applyWrites", p.ApplyWrites)
	p.router.HandleFunc("/xrpc/network.habitat.space.getRecord", p.GetRecord)
	p.router.HandleFunc("/xrpc/network.habitat.space.getBlob", p.GetBlob)
	p.router.HandleFunc("/xrpc/network.habitat.space.listRecords", p.ListRecords)
	p.router.HandleFunc("/xrpc/network.habitat.space.searchRecords", p.SearchRecords)
	p.router.HandleFunc("/xrpc/network.habitat.space.deleteRecord", p.DeleteRecord)
	p.router.HandleFunc("/xrpc/network.habitat.space.listRepoOps", p.ListRepoOps)
	p.router.HandleFunc("/xrpc/network.habitat.space.getLatestCommit", p.GetLatestCommit)
	p.router.HandleFunc("/xrpc/network.habitat.space.getRepo", p.GetRepo)
	p.router.HandleFunc("/xrpc/network.habitat.space.getDelegationToken", p.GetDelegationToken)
	p.router.HandleFunc("/xrpc/network.habitat.space.getSpaceCredential", p.GetSpaceCredential)
	p.router.HandleFunc("/xrpc/network.habitat.space.notifyWrite", p.NotifyWrite)
	p.router.HandleFunc("/xrpc/network.habitat.space.registerNotify", p.RegisterNotify)
	p.router.HandleFunc(
		"/xrpc/network.habitat.space.notifyCredentialRevoked", p.NotifyCredentialRevoked,
	)
	p.router.HandleFunc("/xrpc/network.habitat.space.getSnapshot", p.GetSnapshot).
		Methods(http.MethodGet)
	p.router.HandleFunc("/xrpc/network.habitat.space.subscribe", p.Subscribe).
		Methods(http.MethodGet)
	p.router.HandleFunc("/xrpc/network.habitat.repo.uploadBlob", p.UploadBlob)

	// Opensocial
	p.router.HandleFunc("/xrpc/group.opensocial.createSpace", p.GroupCreateSpace)
	p.router.HandleFunc("/xrpc/group.opensocial.deleteSpace", p.GroupDeleteSpace)
	p.router.HandleFunc("/xrpc/group.opensocial.leaveGroup", p.GroupLeaveGroup)
	p.router.HandleFunc("/xrpc/group.opensocial.requestJoin", p.GroupRequestJoin)
	p.router.HandleFunc("/xrpc/group.opensocial.cancelJoinRequest", p.GroupCancelJoinRequest)
	p.router.HandleFunc("/xrpc/group.opensocial.listJoinRequests", p.GroupListJoinRequests)
	p.router.HandleFunc("/xrpc/group.opensocial.denyJoinRequest", p.GroupDenyJoinRequest)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.createOrg", p.CreateOrg)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.getProfiles", p.GetProfiles)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.updateProfile", p.UpdateProfile)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.uploadImage", p.UploadImage)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.createInvite", p.CreateInvite)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.listInvites", p.ListInvites)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.listPendingInvites", p.ListPendingInvites)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.revokeInvite", p.RevokeInvite)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.requestJoin", p.RequestJoin)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.createSpace", p.CreateOpensocialSpace)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.updateSpace", p.UpdateOpensocialSpace)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.putRole", p.PutRole)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.deleteRole", p.DeleteRole)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.updatePermissions", p.UpdatePermissions)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.assignRoles", p.AssignRoles)
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.ejectMember", p.EjectMember)

	// Search configuration
	p.router.HandleFunc("/xrpc/network.habitat.search.listCollections", p.ListSearchCollections)
	p.router.HandleFunc("/xrpc/network.habitat.search.addCollection", p.AddSearchCollection)
	p.router.HandleFunc("/xrpc/network.habitat.search.removeCollection", p.RemoveSearchCollection)

	// MCP gateway
	p.router.HandleFunc("/xrpc/network.habitat.mcp.addServer", p.AddServer)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.updateServer", p.UpdateServer)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.removeServer", p.RemoveServer)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.listServers", p.ListServers)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.startAuthorization", p.StartAuthorization)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.disconnectServer", p.DisconnectServer)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.search", p.McpSearch)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.debug", p.McpDebug)
	p.router.HandleFunc(MCPOAuthCallbackPath, p.McpOAuthCallback).Methods(http.MethodGet)

	// LLM connections
	p.router.HandleFunc("/xrpc/network.habitat.llm.addConnection", p.AddLlmConnection)
	p.router.HandleFunc("/xrpc/network.habitat.llm.updateConnection", p.UpdateLlmConnection)
	p.router.HandleFunc("/xrpc/network.habitat.llm.removeConnection", p.RemoveLlmConnection)
	p.router.HandleFunc("/xrpc/network.habitat.llm.listConnections", p.ListLlmConnections)
	// The org's OpenAI-compatible API, routed to its connections.
	p.router.HandleFunc(llmModelsPath, p.ListLlmModels).Methods(http.MethodGet)
	p.router.HandleFunc(llmChatCompletionsPath, p.CreateLlmChatCompletion).
		Methods(http.MethodPost)

	// Relationships
	p.router.HandleFunc("/xrpc/network.habitat.relationship.setUserRelation", p.SetUserRelation)
	p.router.HandleFunc("/xrpc/network.habitat.relationship.setSpaceRelation", p.SetSpaceRelation)
	p.router.HandleFunc("/xrpc/network.habitat.relationship.deleteRelation", p.DeleteRelation)
	p.router.HandleFunc("/xrpc/network.habitat.relationship.listRelations", p.ListRelations)
	p.router.HandleFunc("/xrpc/network.habitat.relationship.checkUserRelation", p.CheckUserRelation)
	p.router.HandleFunc(
		"/xrpc/network.habitat.relationship.checkSpaceRelation",
		p.CheckSpaceRelation,
	)
	p.router.HandleFunc("/xrpc/network.habitat.relationship.resolveRelations", p.ResolveRelations)
	p.router.HandleFunc("/xrpc/network.habitat.relationship.listRelatedSpaces", p.ListRelatedSpaces)
	p.router.HandleFunc(
		"/xrpc/network.habitat.relationship.resolveSpaces",
		p.ResolveSpaces,
	)

	// com.atproto aliases for the permissioned-data proposal's official NSIDs
	// (proposal 0016). Same handlers as the network.habitat registrations
	// above; the memberspace batch methods are separate handlers.
	p.router.HandleFunc("/xrpc/com.atproto.space.listSpaces", p.ListSpaces)
	p.router.HandleFunc("/xrpc/com.atproto.space.listWriters", p.ListWriters)
	p.router.HandleFunc("/xrpc/com.atproto.space.removeWriter", p.RemoveWriter)
	p.router.HandleFunc("/xrpc/com.atproto.space.putRecord", p.PutRecord)
	p.router.HandleFunc("/xrpc/com.atproto.space.createRecord", p.CreateRecord)
	p.router.HandleFunc("/xrpc/com.atproto.space.applyWrites", p.ApplyWrites)
	p.router.HandleFunc("/xrpc/com.atproto.space.getRecord", p.GetRecord)
	p.router.HandleFunc("/xrpc/com.atproto.space.getBlob", p.GetBlob)
	p.router.HandleFunc("/xrpc/com.atproto.space.listRecords", p.ListRecords)
	p.router.HandleFunc("/xrpc/com.atproto.space.deleteRecord", p.DeleteRecord)
	p.router.HandleFunc("/xrpc/com.atproto.space.listRepoOps", p.ListRepoOps)
	p.router.HandleFunc("/xrpc/com.atproto.space.getLatestCommit", p.GetLatestCommit)
	p.router.HandleFunc("/xrpc/com.atproto.space.getRepo", p.GetRepo)
	p.router.HandleFunc("/xrpc/com.atproto.space.getDelegationToken", p.GetDelegationToken)
	p.router.HandleFunc("/xrpc/com.atproto.space.clearRecords", p.ClearRecords)
	p.router.HandleFunc("/xrpc/com.atproto.space.importRepo", p.ImportRepo)
	p.router.HandleFunc("/xrpc/com.atproto.space.exportSpaceMeta", p.ExportSpaceMeta)
	p.router.HandleFunc("/xrpc/com.atproto.space.importSpaceMeta", p.ImportSpaceMeta)
	p.router.HandleFunc("/xrpc/com.atproto.space.authorizeSpaceSync", p.AuthorizeSpaceSync)
	p.router.HandleFunc("/xrpc/com.atproto.space.notifyRepoUpdate", p.NotifyWrite)
	p.router.HandleFunc(
		"/xrpc/com.atproto.space.notifyCredentialsRevoked", p.NotifyCredentialRevoked,
	)
	p.router.HandleFunc("/xrpc/com.atproto.repo.uploadBlob", p.UploadBlob)

	// com.atproto.server aliases
	p.router.HandleFunc("/xrpc/com.atproto.server.getSession", p.GetSession)

	// Space policies (proposal 0016): lifecycle is policy-agnostic under
	// com.atproto.space, member methods belong to the memberspace policy.
	p.router.HandleFunc("/xrpc/com.atproto.space.createSpace", p.CreateSpace)
	p.router.HandleFunc("/xrpc/com.atproto.space.updateSpace", p.UpdateSpace)
	p.router.HandleFunc("/xrpc/com.atproto.space.getSpace", p.GetSpace)
	p.router.HandleFunc("/xrpc/com.atproto.space.deleteSpace", p.DeleteSpace)
	p.router.HandleFunc("/xrpc/com.atproto.memberspace.putMembers", p.PutMembers)
	p.router.HandleFunc("/xrpc/com.atproto.memberspace.removeMembers", p.RemoveMembers)
	p.router.HandleFunc("/xrpc/com.atproto.memberspace.listMembers", p.ListMembers)
}
