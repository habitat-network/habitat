package pearserver

// registerRoutes wires all handlers onto the internal router.
func (p *PearServer) registerRoutes() {
	// Spaces
	p.router.HandleFunc("/xrpc/network.habitat.space.listSpaces", p.ListSpaces)
	p.router.HandleFunc("/xrpc/network.habitat.space.listRepos", p.ListRepos)
	p.router.HandleFunc("/xrpc/network.habitat.space.putRecord", p.PutRecord)
	p.router.HandleFunc("/xrpc/network.habitat.space.getRecord", p.GetRecord)
	p.router.HandleFunc("/xrpc/network.habitat.space.getBlob", p.GetBlob)
	p.router.HandleFunc("/xrpc/network.habitat.space.listRecords", p.ListRecords)
	p.router.HandleFunc("/xrpc/network.habitat.space.deleteRecord", p.DeleteRecord)
	p.router.HandleFunc("/xrpc/network.habitat.space.listRepoOps", p.ListRepoOps)
	p.router.HandleFunc("/xrpc/network.habitat.space.getLatestCommit", p.GetLatestCommit)
	p.router.HandleFunc("/xrpc/network.habitat.space.getRepo", p.GetRepo)
	p.router.HandleFunc("/xrpc/network.habitat.space.getDelegationToken", p.GetDelegationToken)
	p.router.HandleFunc("/xrpc/network.habitat.space.getSpaceCredential", p.GetSpaceCredential)
	p.router.HandleFunc("/xrpc/network.habitat.space.notifyWrite", p.NotifyWrite)
	p.router.HandleFunc("/xrpc/network.habitat.space.registerNotify", p.RegisterNotify)
	p.router.HandleFunc("/xrpc/network.habitat.repo.uploadBlob", p.UploadBlob)

	// Opensocial
	p.router.HandleFunc("/xrpc/network.habitat.opensocial.createOrg", p.CreateOrg)
	p.router.HandleFunc("/xrpc/community.opensocial.updateProfile", p.UpdateProfile)
	p.router.HandleFunc("/xrpc/community.opensocial.uploadImage", p.UploadImage)
	p.router.HandleFunc("/xrpc/community.opensocial.createInvite", p.CreateInvite)
	p.router.HandleFunc("/xrpc/community.opensocial.listInvites", p.ListInvites)
	p.router.HandleFunc("/xrpc/community.opensocial.listPendingInvites", p.ListPendingInvites)
	p.router.HandleFunc("/xrpc/community.opensocial.revokeInvite", p.RevokeInvite)
	p.router.HandleFunc("/xrpc/community.opensocial.requestJoin", p.RequestJoin)
	p.router.HandleFunc("/xrpc/community.opensocial.createSpace", p.CreateOpensocialSpace)
	p.router.HandleFunc("/xrpc/community.opensocial.updateSpace", p.UpdateOpensocialSpace)
	p.router.HandleFunc("/xrpc/community.opensocial.putRole", p.PutRole)
	p.router.HandleFunc("/xrpc/community.opensocial.deleteRole", p.DeleteRole)
	p.router.HandleFunc("/xrpc/community.opensocial.updatePermissions", p.UpdatePermissions)
	p.router.HandleFunc("/xrpc/community.opensocial.assignRoles", p.AssignRoles)
	p.router.HandleFunc("/xrpc/community.opensocial.ejectMember", p.EjectMember)

	// MCP gateway
	p.router.HandleFunc("/xrpc/network.habitat.mcp.addServer", p.AddServer)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.updateServer", p.UpdateServer)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.removeServer", p.RemoveServer)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.listServers", p.ListServers)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.startAuthorization", p.StartAuthorization)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.confirmConnection", p.ConfirmConnection)
	p.router.HandleFunc("/xrpc/network.habitat.mcp.disconnectServer", p.DisconnectServer)

	// Simplespace
	p.router.HandleFunc("/xrpc/network.habitat.simplespace.createSpace", p.CreateSpace)
	p.router.HandleFunc("/xrpc/network.habitat.simplespace.addMember", p.AddMember)
	p.router.HandleFunc("/xrpc/network.habitat.simplespace.removeMember", p.RemoveMember)
	p.router.HandleFunc("/xrpc/network.habitat.simplespace.listMembers", p.ListMembers)
	p.router.HandleFunc("/xrpc/network.habitat.simplespace.deleteSpace", p.DeleteSpace)

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

	// com.atproto aliases for the permissioned-data proposal's official NSIDs
	// (proposal 0016). Same handlers as the network.habitat registrations
	// above; com.atproto.simplespace.putMember aliases addMember.
	p.router.HandleFunc("/xrpc/com.atproto.space.listSpaces", p.ListSpaces)
	p.router.HandleFunc("/xrpc/com.atproto.space.listRepos", p.ListRepos)
	p.router.HandleFunc("/xrpc/com.atproto.space.putRecord", p.PutRecord)
	p.router.HandleFunc("/xrpc/com.atproto.space.getRecord", p.GetRecord)
	p.router.HandleFunc("/xrpc/com.atproto.space.getBlob", p.GetBlob)
	p.router.HandleFunc("/xrpc/com.atproto.space.listRecords", p.ListRecords)
	p.router.HandleFunc("/xrpc/com.atproto.space.deleteRecord", p.DeleteRecord)
	p.router.HandleFunc("/xrpc/com.atproto.space.listRepoOps", p.ListRepoOps)
	p.router.HandleFunc("/xrpc/com.atproto.space.getLatestCommit", p.GetLatestCommit)
	p.router.HandleFunc("/xrpc/com.atproto.space.getRepo", p.GetRepo)
	p.router.HandleFunc("/xrpc/com.atproto.space.getDelegationToken", p.GetDelegationToken)
	p.router.HandleFunc("/xrpc/com.atproto.space.getSpaceCredential", p.GetSpaceCredential)
	p.router.HandleFunc("/xrpc/com.atproto.space.registerNotify", p.RegisterNotify)
	p.router.HandleFunc("/xrpc/com.atproto.repo.uploadBlob", p.UploadBlob)
	p.router.HandleFunc("/xrpc/com.atproto.simplespace.createSpace", p.CreateSpace)
	p.router.HandleFunc("/xrpc/com.atproto.simplespace.putMember", p.AddMember)
	p.router.HandleFunc("/xrpc/com.atproto.simplespace.removeMember", p.RemoveMember)
	p.router.HandleFunc("/xrpc/com.atproto.simplespace.listMembers", p.ListMembers)
	p.router.HandleFunc("/xrpc/com.atproto.simplespace.deleteSpace", p.DeleteSpace)
}
