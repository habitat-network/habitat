package integration

import "net/http/httputil"

type DockerBridge struct {
	*httputil.ReverseProxy
}

func NewDockerBridge() *DockerBridge {
	return &DockerBridge{
		ReverseProxy: &httputil.ReverseProxy{
			Rewrite: func(*httputil.ProxyRequest),
		},
	}
}


func (db *DockerBridge) rewriteRequest(r *httputil.ProxyRequest) {
	r.Out.
}
