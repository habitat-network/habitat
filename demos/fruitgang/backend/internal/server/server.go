package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// OrgLookup reports which orgs are currently connected. *sap.Sap satisfies
// this via its existing ListManagedOrgs method.
type OrgLookup interface {
	ListManagedOrgs(ctx context.Context) ([]syntax.DID, error)
}

type memberResponse struct {
	URI           string    `json:"uri"`
	DID           string    `json:"did"`
	DisplayName   string    `json:"displayName"`
	AvatarCID     string    `json:"avatarCid,omitempty"`
	FunFact       string    `json:"funFact,omitempty"`
	FavoriteFruit string    `json:"favoriteFruit,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type chatResponse struct {
	URI       string    `json:"uri"`
	AuthorDID string    `json:"authorDid"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type replyResponse struct {
	URI       string    `json:"uri"`
	AuthorDID string    `json:"authorDid"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type logResponse struct {
	URI       string    `json:"uri"`
	AuthorDID string    `json:"authorDid"`
	Fruit     string    `json:"fruit"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"createdAt"`
}

func New(store *index.Store, orgs OrgLookup) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /getMembers", func(w http.ResponseWriter, r *http.Request) {
		members, err := store.GetMembers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]memberResponse, len(members))
		for i, m := range members {
			resp[i] = memberResponse{URI: m.URI, DID: m.DID, DisplayName: m.DisplayName, AvatarCID: m.AvatarCID, FunFact: m.FunFact, FavoriteFruit: m.FavoriteFruit, CreatedAt: m.CreatedAt}
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("GET /getChats", func(w http.ResponseWriter, r *http.Request) {
		chats, err := store.GetChats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]chatResponse, len(chats))
		for i, c := range chats {
			resp[i] = chatResponse{URI: c.URI, AuthorDID: c.AuthorDID, Text: c.Text, CreatedAt: c.CreatedAt}
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("GET /getReplies", func(w http.ResponseWriter, r *http.Request) {
		chatURI := r.URL.Query().Get("chatUri")
		if chatURI == "" {
			http.Error(w, "chatUri query param required", http.StatusBadRequest)
			return
		}
		replies, err := store.GetReplies(chatURI)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]replyResponse, len(replies))
		for i, rr := range replies {
			resp[i] = replyResponse{URI: rr.URI, AuthorDID: rr.AuthorDID, Text: rr.Text, CreatedAt: rr.CreatedAt}
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("GET /getLogs", func(w http.ResponseWriter, r *http.Request) {
		logs, err := store.GetLogs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]logResponse, len(logs))
		for i, l := range logs {
			resp[i] = logResponse{URI: l.URI, AuthorDID: l.AuthorDID, Fruit: l.Fruit, Count: l.Count, CreatedAt: l.CreatedAt}
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("GET /getSpaceURI", func(w http.ResponseWriter, r *http.Request) {
		// Fruit Gang is a single-org demo: the connected org's space URI is
		// derived from its DID rather than stored, since sap already durably
		// tracks which org (if any) has been connected.
		dids, err := orgs.ListManagedOrgs(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(dids) == 0 {
			http.Error(w, "no org connected", http.StatusNotFound)
			return
		}
		uri := habitat_syntax.ConstructSpaceURI(dids[0], "network.habitat.group", "fruitgang")
		writeJSON(w, map[string]string{"uri": uri.String()})
	})
	return corsMiddleware(mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
