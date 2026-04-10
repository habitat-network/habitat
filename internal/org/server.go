package org

import "net/http"

type Server struct {
}

// Org APIs
func (s *Server) BootstrapAdmin(w http.ResponseWriter, r *http.Request) {
	// TODO: implement once we have a provisioner process; til then this is manual
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) GetAdmins(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) GetMembers(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) AddAdmin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) AddMembers(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) RemoveAdmin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) RemoveMembers(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}
