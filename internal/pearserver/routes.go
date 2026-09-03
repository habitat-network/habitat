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
}
