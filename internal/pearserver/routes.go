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
}
