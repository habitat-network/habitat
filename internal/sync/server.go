package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/rs/zerolog/log"

	"github.com/habitat-network/habitat/internal/authn"
)

// SpaceView is the sync package's view of a space (avoids importing spaces package).
type SpaceView struct {
	Space     string
	Type      string
	Skey      string
	SpaceRev  string
	MemberRev string
	CreatedAt string
}

// SpaceState is the current state of a space for sync.
type SpaceState struct {
	Space     string
	SpaceType string
	SpaceRev  string
	MemberRev string
	Repos     []SpaceRepoState
}

// SpaceRepoState is the state of a single repo within a space.
type SpaceRepoState struct {
	DID string
	Rev string
}

// RecordChange is a single record change for syncing.
type RecordChange struct {
	Space      string
	Repo       string
	Rev        string
	Action     string
	Collection string
	Rkey       string
	Value      *map[string]any
}

// MemberOp is a single member operation in the oplog.
type MemberOp struct {
	Space  string
	Rev    string
	Idx    int
	Action string
	DID    string
	Access *string
}

// SpaceStore is the minimal interface the sync server needs from the spaces store.
type SpaceStore interface {
	ListSpaces(
		ctx context.Context,
		member syntax.DID,
		filterOwner *syntax.DID,
		filterType *syntax.NSID,
	) ([]SpaceView, error)
	GetSpaceState(ctx context.Context, space string) (*SpaceState, error)
	ListRecordChanges(
		ctx context.Context,
		space string,
		repo string,
		since string,
		limit int,
	) ([]RecordChange, error)
	GetMemberOplog(ctx context.Context, space string, since string, limit int) ([]MemberOp, error)
	IsMember(ctx context.Context, space string, did string) (bool, error)
	GetSpace(ctx context.Context, space string) (*SpaceView, error)
	GetEvents(ctx context.Context, since int64, limit int) ([]Event, error)
}

// Server serves the sync XRPC endpoints.
type Server struct {
	store  SpaceStore
	fanout *Fanout
	oauth  authn.Method
}

// NewServer creates a new sync Server.
func NewServer(store SpaceStore, fanout *Fanout, oauth authn.Method) *Server {
	return &Server{
		store:  store,
		fanout: fanout,
		oauth:  oauth,
	}
}

func (s *Server) getAuth(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
		return "", ErrUnauthorized
	}
	token := auth[len(prefix):]

	did, ok, err := s.oauth.ValidateRaw(r.Context(), token)
	if err != nil || !ok {
		return "", ErrUnauthorized
	}
	return did.String(), nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Warn().Err(err).Msg("failed to encode JSON response")
	}
}

func writeError(w http.ResponseWriter, err *APIError) {
	writeJSON(w, err.Status, map[string]interface{}{"error": err.Code, "message": err.Message})
}

// HandleSubscribeSpaces streams space events via SSE.
func (s *Server) HandleSubscribeSpaces(w http.ResponseWriter, r *http.Request) {
	did, err := s.getAuth(r)
	if err != nil {
		writeError(w, ErrUnauthorized)
		return
	}

	memberDID, err := syntax.ParseDID(did)
	if err != nil {
		writeError(w, &APIError{
			Code:    "InvalidRequest",
			Message: "invalid DID",
			Status:  400,
		})
		return
	}

	accessible, err := s.store.ListSpaces(r.Context(), memberDID, nil, nil)
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Msg("listSpaces for subscription failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to list spaces",
			Status:  500,
		})
		return
	}
	accessibleSet := make(map[string]struct{}, len(accessible))
	for _, sp := range accessible {
		accessibleSet[sp.Space] = struct{}{}
	}

	var cursor int64
	if c := r.URL.Query().Get("cursor"); c != "" {
		cursor, err = strconv.ParseInt(c, 10, 64)
		if err != nil {
			log.Ctx(r.Context()).Warn().Str("cursor", c).Msg("invalid cursor")
			writeError(w, &APIError{
				Code:    "InvalidRequest",
				Message: "cursor must be an integer",
				Status:  400,
			})
			return
		}
	}
	spaceTypes := r.URL.Query()["spaceTypes"]

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Ctx(r.Context()).Warn().Msg("response writer does not support flushing")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "streaming not supported",
			Status:  500,
		})
		return
	}

	ch, done := s.fanout.Subscribe(1024)
	defer s.fanout.Unsubscribe(done)

	if cursor > 0 {
		for {
			events, err := s.store.GetEvents(r.Context(), cursor, 100)
			if err != nil {
				log.Ctx(r.Context()).Warn().Err(err).Msg("getEvents for catchup failed")
				return
			}
			if len(events) == 0 {
				break
			}
			for _, ev := range events {
				if _, ok := accessibleSet[ev.Space]; !ok {
					continue
				}
				if !filterEvent(ev, spaceTypes) {
					continue
				}
				if err := writeSSE(w, flusher, ev); err != nil {
					return
				}
				cursor = ev.Seq
			}
			if len(events) < 100 {
				break
			}
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if ev.Seq <= cursor {
				continue
			}
			if _, ok := accessibleSet[ev.Space]; !ok {
				continue
			}
			if !filterEvent(ev, spaceTypes) {
				continue
			}
			if err := writeSSE(w, flusher, ev); err != nil {
				return
			}
			cursor = ev.Seq
		}
	}
}

func filterEvent(ev Event, spaceTypes []string) bool {
	if len(spaceTypes) > 0 && ev.SpaceType != "" {
		found := false
		for _, t := range spaceTypes {
			if t == ev.SpaceType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: message\ndata: %s\nid: %d\n\n", data, ev.Seq); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// HandleListSpaces returns the spaces the caller is a member of.
func (s *Server) HandleListSpaces(w http.ResponseWriter, r *http.Request) {
	did, err := s.getAuth(r)
	if err != nil {
		writeError(w, ErrUnauthorized)
		return
	}

	memberDID, err := syntax.ParseDID(did)
	if err != nil {
		writeError(w, &APIError{
			Code:    "InvalidRequest",
			Message: "invalid DID",
			Status:  400,
		})
		return
	}

	spaceTypes := r.URL.Query()["spaceTypes"]
	var filterType *syntax.NSID
	if len(spaceTypes) > 0 {
		parsed, err := syntax.ParseNSID(spaceTypes[0])
		if err != nil {
			writeError(w, &APIError{
				Code:    "InvalidRequest",
				Message: "invalid spaceTypes",
				Status:  400,
			})
			return
		}
		filterType = &parsed
	}

	views, err := s.store.ListSpaces(r.Context(), memberDID, nil, filterType)
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Msg("listSpaces failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to list spaces",
			Status:  500,
		})
		return
	}

	type spaceView struct {
		Space     string `json:"space"`
		SpaceType string `json:"spaceType"`
		SpaceRev  string `json:"spaceRev,omitempty"`
		MemberRev string `json:"memberRev,omitempty"`
		CreatedAt string `json:"createdAt,omitempty"`
	}

	spaces := make([]spaceView, 0, len(views))
	for _, v := range views {
		spaces = append(spaces, spaceView{
			Space:     v.Space,
			SpaceType: v.Type,
			SpaceRev:  v.SpaceRev,
			MemberRev: v.MemberRev,
			CreatedAt: v.CreatedAt,
		})
	}

	writeJSON(w, 200, map[string]interface{}{"spaces": spaces})
}

// HandleGetSpaceState returns the current state of a space.
func (s *Server) HandleGetSpaceState(w http.ResponseWriter, r *http.Request) {
	did, err := s.getAuth(r)
	if err != nil {
		writeError(w, ErrUnauthorized)
		return
	}

	memberDID, err := syntax.ParseDID(did)
	if err != nil {
		writeError(w, &APIError{
			Code:    "InvalidRequest",
			Message: "invalid DID",
			Status:  400,
		})
		return
	}

	space := r.URL.Query().Get("space")
	if space == "" {
		writeError(w, &APIError{Code: "InvalidRequest", Message: "space is required", Status: 400})
		return
	}

	isMember, err := s.store.IsMember(r.Context(), space, memberDID.String())
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("membership check failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to verify membership",
			Status:  500,
		})
		return
	}
	if !isMember {
		writeError(w, ErrSpaceNotFound)
		return
	}

	state, err := s.store.GetSpaceState(r.Context(), space)
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("getSpaceState failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to get space state",
			Status:  500,
		})
		return
	}
	if state == nil {
		writeError(w, ErrSpaceNotFound)
		return
	}

	type repoState struct {
		DID string `json:"did"`
		Rev string `json:"rev"`
	}
	repos := make([]repoState, len(state.Repos))
	for i, r := range state.Repos {
		repos[i] = repoState(r)
	}

	writeJSON(w, 200, map[string]interface{}{
		"space":     state.Space,
		"spaceType": state.SpaceType,
		"spaceRev":  state.SpaceRev,
		"memberRev": state.MemberRev,
		"repos":     repos,
	})
}

// HandleListRecords returns non-deleted records in a space.
func (s *Server) HandleListRecords(w http.ResponseWriter, r *http.Request) {
	did, err := s.getAuth(r)
	if err != nil {
		writeError(w, ErrUnauthorized)
		return
	}

	memberDID, err := syntax.ParseDID(did)
	if err != nil {
		writeError(w, &APIError{
			Code:    "InvalidRequest",
			Message: "invalid DID",
			Status:  400,
		})
		return
	}

	space := r.URL.Query().Get("space")
	if space == "" {
		writeError(w, &APIError{Code: "InvalidRequest", Message: "space is required", Status: 400})
		return
	}

	isMember, err := s.store.IsMember(r.Context(), space, memberDID.String())
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("membership check failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to verify membership",
			Status:  500,
		})
		return
	}
	if !isMember {
		writeError(w, ErrSpaceNotFound)
		return
	}

	repo := r.URL.Query().Get("repo")
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	changes, err := s.store.ListRecordChanges(r.Context(), space, repo, cursor, limit)
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("listRecords failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to list records",
			Status:  500,
		})
		return
	}

	type record struct {
		Space      string                 `json:"space"`
		Repo       string                 `json:"repo"`
		Rev        string                 `json:"rev"`
		Collection string                 `json:"collection"`
		Rkey       string                 `json:"rkey"`
		Value      map[string]interface{} `json:"value"`
	}
	records := make([]record, 0, len(changes))
	var lastRev string
	for _, ch := range changes {
		lastRev = ch.Rev
		if ch.Action != "delete" && ch.Value != nil {
			records = append(records, record{
				Space:      ch.Space,
				Repo:       ch.Repo,
				Rev:        ch.Rev,
				Collection: ch.Collection,
				Rkey:       ch.Rkey,
				Value:      *ch.Value,
			})
		}
	}

	writeJSON(w, 200, map[string]interface{}{
		"records": records,
		"cursor":  lastRev,
	})
}

// HandleListRecordChanges returns record changes in a space with optional cursor.
func (s *Server) HandleListRecordChanges(w http.ResponseWriter, r *http.Request) {
	did, err := s.getAuth(r)
	if err != nil {
		writeError(w, ErrUnauthorized)
		return
	}

	memberDID, err := syntax.ParseDID(did)
	if err != nil {
		writeError(w, &APIError{
			Code:    "InvalidRequest",
			Message: "invalid DID",
			Status:  400,
		})
		return
	}

	space := r.URL.Query().Get("space")
	if space == "" {
		writeError(w, &APIError{Code: "InvalidRequest", Message: "space is required", Status: 400})
		return
	}

	isMember, err := s.store.IsMember(r.Context(), space, memberDID.String())
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("membership check failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to verify membership",
			Status:  500,
		})
		return
	}
	if !isMember {
		writeError(w, ErrSpaceNotFound)
		return
	}

	repo := r.URL.Query().Get("repo")
	since := r.URL.Query().Get("since")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	changes, err := s.store.ListRecordChanges(r.Context(), space, repo, since, limit)
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("listRecordChanges failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to list record changes",
			Status:  500,
		})
		return
	}

	type change struct {
		Space      string                 `json:"space"`
		Rev        string                 `json:"rev"`
		Action     string                 `json:"action"`
		Collection string                 `json:"collection"`
		Rkey       string                 `json:"rkey"`
		Value      map[string]interface{} `json:"value,omitempty"`
	}
	changesOut := make([]change, 0, len(changes))
	var lastRev string
	for _, ch := range changes {
		lastRev = ch.Rev
		c := change{
			Space:      ch.Space,
			Rev:        ch.Rev,
			Action:     ch.Action,
			Collection: ch.Collection,
			Rkey:       ch.Rkey,
		}
		if ch.Value != nil {
			c.Value = *ch.Value
		}
		changesOut = append(changesOut, c)
	}

	writeJSON(w, 200, map[string]interface{}{
		"changes": changesOut,
		"cursor":  lastRev,
	})
}

// HandleGetMemberOplog returns the member operation log for a space.
func (s *Server) HandleGetMemberOplog(w http.ResponseWriter, r *http.Request) {
	did, err := s.getAuth(r)
	if err != nil {
		writeError(w, ErrUnauthorized)
		return
	}

	memberDID, err := syntax.ParseDID(did)
	if err != nil {
		writeError(w, &APIError{
			Code:    "InvalidRequest",
			Message: "invalid DID",
			Status:  400,
		})
		return
	}

	space := r.URL.Query().Get("space")
	if space == "" {
		writeError(w, &APIError{Code: "InvalidRequest", Message: "space is required", Status: 400})
		return
	}

	isMember, err := s.store.IsMember(r.Context(), space, memberDID.String())
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("membership check failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to verify membership",
			Status:  500,
		})
		return
	}
	if !isMember {
		writeError(w, ErrSpaceNotFound)
		return
	}

	since := r.URL.Query().Get("since")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	ops, err := s.store.GetMemberOplog(r.Context(), space, since, limit)
	if err != nil {
		log.Ctx(r.Context()).Warn().Err(err).Str("space", space).Msg("getMemberOplog failed")
		writeError(w, &APIError{
			Code:    "InternalError",
			Message: "failed to get member oplog",
			Status:  500,
		})
		return
	}

	type op struct {
		Space  string `json:"space"`
		Rev    string `json:"rev"`
		Idx    int    `json:"idx"`
		Action string `json:"action"`
		DID    string `json:"did"`
		Access string `json:"access,omitempty"`
	}
	opsOut := make([]op, 0, len(ops))
	var lastRev string
	for _, o := range ops {
		lastRev = o.Rev
		oOut := op{
			Space:  o.Space,
			Rev:    o.Rev,
			Idx:    o.Idx,
			Action: o.Action,
			DID:    o.DID,
		}
		if o.Access != nil {
			oOut.Access = *o.Access
		}
		opsOut = append(opsOut, oOut)
	}

	writeJSON(w, 200, map[string]interface{}{
		"ops":    opsOut,
		"cursor": lastRev,
	})
}
