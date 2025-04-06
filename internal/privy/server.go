package privy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/api/agnostic"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/eagraf/habitat-new/internal/node/api"
	"github.com/rs/zerolog/log"
)

type PutRecordRequest struct {
	Input   *agnostic.RepoPutRecord_Input
	Encrypt bool `json:"encrypt"`
}

type Server struct {
	inner   *store
	pdsHost string
}

func NewServer(pdsHost string, enc Encrypter) *Server {
	return &Server{
		pdsHost: pdsHost,
		inner: &store{
			e: enc,
		},
	}
}

func (s *Server) PutRecord(cli *xrpc.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req PutRecordRequest
		slurp, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		err = json.Unmarshal(slurp, &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		out, err := s.inner.putRecord(r.Context(), cli, req.Input, req.Encrypt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slurp, err = json.Marshal(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(slurp); err != nil {
			log.Err(err).Msgf("error sending response for PutRecord request")
		}
	}
}

func (s *Server) GetRecord(cli *xrpc.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := url.Parse(r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cid := u.Query().Get("cid")
		collection := u.Query().Get("collection")
		did := u.Query().Get("did")
		rkey := u.Query().Get("rkey")

		out, err := s.inner.getRecord(r.Context(), cli, cid, collection, did, rkey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		slurp, err := json.Marshal(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(slurp); err != nil {
			log.Err(err).Msgf("error sending response for GetRecord request")
		}
	}
}

// This creates the xrpc.Client to use in the inner privy requests
// TODO: this should actually pull out the requested did from the url param or input and re-direct there. (Potentially move this lower into the fns themselves).
// This would allow for requesting to any pds through these routes, not just the one tied to this habitat node.
func (s *Server) pdsAuthMiddleware(next func(c *xrpc.Client) http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accessJwt, err := getAccessJwt(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		c := &xrpc.Client{
			Host: fmt.Sprintf("http://%s", s.pdsHost),
			Auth: &xrpc.AuthInfo{
				AccessJwt: accessJwt,
			},
		}
		next(c).ServeHTTP(w, r)
	})
}

func getAccessJwt(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) > 7 {
		return authHeader[7:], nil
	}
	for _, cookie := range r.Cookies() {
		if cookie.Name == "access_token" {
			return cookie.Value, nil
		}
	}
	return "", fmt.Errorf("missing auth info")
}

func (s *Server) GetRoutes() []api.Route {
	return []api.Route{
		api.NewBasicRoute(
			http.MethodPost,
			"/xrpc/com.habitat.putRecord",
			s.pdsAuthMiddleware(s.PutRecord),
		),
		api.NewBasicRoute(
			http.MethodGet,
			"/xrpc/com.habitat.getRecord",
			s.pdsAuthMiddleware(s.GetRecord),
		),
	}
}
