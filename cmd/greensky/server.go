package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/sap"
)

// Server exposes greensky's HTTP surface: the getPosts XRPC endpoint (reached
// via pear service proxying), the one-time org onboarding redirect, and the
// did:web document pear resolves to find this server.
type Server struct {
	store       *PostStore
	sap         sap.Sap
	serviceAuth authn.Method
	did         didConfig
}

func NewServer(store *PostStore, s sap.Sap, serviceAuth authn.Method, did didConfig) *Server {
	return &Server{store: store, sap: s, serviceAuth: serviceAuth, did: did}
}

// HandleGetPosts returns the calling user's threads. pear forwards the request
// here with a service-auth JWT identifying the caller, which scopes the result
// to that user — for now everyone only sees their own posts.
func (s *Server) HandleGetPosts(w http.ResponseWriter, r *http.Request) {
	cred, ok := s.serviceAuth.Validate(w, r)
	if !ok {
		return
	}

	threads, err := s.store.ThreadsForAuthor(r.Context(), cred.Subject)
	if err != nil {
		http.Error(w, "failed to load posts", http.StatusInternalServerError)
		slog.ErrorContext(r.Context(), "load threads", "err", err, "author", cred.Subject)
		return
	}

	out := habitat.NetworkHabitatGreenskyGetPostsOutput{
		Threads: make([]habitat.NetworkHabitatGreenskyGetPostsThreadView, 0, len(threads)),
	}
	for _, th := range threads {
		view := habitat.NetworkHabitatGreenskyGetPostsThreadView{
			Post:    toPostView(th.Root),
			Replies: make([]habitat.NetworkHabitatGreenskyGetPostsPostView, 0, len(th.Replies)),
		}
		for _, reply := range th.Replies {
			view.Replies = append(view.Replies, toPostView(reply))
		}
		out.Threads = append(out.Threads, view)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		slog.ErrorContext(r.Context(), "encode getPosts response", "err", err)
	}
}

// HandleAddOrg onboards an org: it asks sap for the org's OAuth authorize URL
// and 303-redirects the admin's browser there. A top-level GET navigation
// (rather than fetch) keeps this cross-origin step free of CORS preflight.
func (s *Server) HandleAddOrg(w http.ResponseWriter, r *http.Request) {
	handle := r.URL.Query().Get("handle")
	if handle == "" {
		http.Error(w, "missing required parameter: handle", http.StatusBadRequest)
		return
	}
	redirectURL, err := s.sap.AddOrg(r.Context(), handle)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Location", redirectURL)
	w.WriteHeader(http.StatusSeeOther)
}

// HandleDIDDoc serves greensky's did:web document.
func (s *Server) HandleDIDDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.did.didDocument()); err != nil {
		slog.ErrorContext(r.Context(), "encode did document", "err", err)
	}
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func toPostView(p post) habitat.NetworkHabitatGreenskyGetPostsPostView {
	return habitat.NetworkHabitatGreenskyGetPostsPostView{
		Uri:       p.URI,
		SpaceUri:  p.SpaceURI,
		Author:    p.Author,
		Text:      p.Text,
		CreatedAt: p.PostedAt.UTC().Format(time.RFC3339),
		IndexedAt: p.IndexedAt.UTC().Format(time.RFC3339),
	}
}
