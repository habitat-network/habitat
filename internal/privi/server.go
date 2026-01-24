package privi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/eagraf/habitat-new/api/habitat"
	"github.com/eagraf/habitat-new/internal/oauthclient"
	"github.com/eagraf/habitat-new/internal/oauthserver"
	"github.com/eagraf/habitat-new/internal/pdscred"
	"github.com/eagraf/habitat-new/internal/permissions"
	"github.com/eagraf/habitat-new/internal/utils"
	"github.com/gorilla/schema"
)

type Server struct {
	// TODO: allow privy server to serve many stores, not just one user
	store *store
	// Used for resolving handles -> did, did -> PDS
	dir identity.Directory
	// TODO: should this really live here?
	repo        *sqliteRepo
	oauthServer *oauthserver.OAuthServer
	inbox       *Inbox
	credStore   pdscred.PDSCredentialStore
	oauthClient oauthclient.OAuthClient
}

// NewServer returns a privi server.
func NewServer(
	perms permissions.Store,
	repo *sqliteRepo,
	inbox *Inbox,
	oauthServer *oauthserver.OAuthServer,
	credStore pdscred.PDSCredentialStore,
	oauthClient oauthclient.OAuthClient,
) *Server {
	server := &Server{
		store:       newStore(perms, repo),
		dir:         identity.DefaultDirectory(),
		repo:        repo,
		oauthServer: oauthServer,
		inbox:       inbox,
		credStore:   credStore,
		oauthClient: oauthClient,
	}
	return server
}

var formDecoder = schema.NewDecoder()

// PutRecord puts a potentially encrypted record (see s.inner.putRecord)
func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatRepoPutRecordInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	ownerDID, err := s.fetchDID(r.Context(), req.Repo)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	if ownerDID.String() != callerDID.String() {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("only owner can put record"),
			"only owner can put record",
			http.StatusMethodNotAllowed,
		)
		return
	}

	var rkey string
	if req.Rkey == "" {
		rkey = uuid.NewString()
	} else {
		rkey = req.Rkey
	}

	record, ok := req.Record.(map[string]any)
	if !ok {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("record must be a JSON object"),
			"invalid record type",
			http.StatusBadRequest,
		)
		return
	}

	v := true
	err = s.store.putRecord(ownerDID.String(), req.Collection, record, rkey, &v)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			fmt.Sprintf("putting record for did %s", ownerDID.String()),
			http.StatusInternalServerError,
		)
		return
	}

	if len(req.Grantees) > 0 {
		err := s.store.permissions.AddLexiconReadPermission(
			req.Grantees,
			ownerDID.String(),
			req.Collection+"."+rkey,
		)
		if err != nil {
			utils.LogAndHTTPError(
				w,
				err,
				fmt.Sprintf("adding permissions for did %s", ownerDID.String()),
				http.StatusInternalServerError,
			)
			return
		}
	}

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatRepoPutRecordOutput{
		Uri: fmt.Sprintf("habitat://%s/%s/%s", ownerDID.String(), req.Collection, rkey),
	}); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) fetchDID(ctx context.Context, didOrHandle string) (syntax.DID, error) {
	// Try handling both handles and dids
	atid, err := syntax.ParseAtIdentifier(didOrHandle)
	if err != nil {
		return "", err
	}

	id, err := s.dir.Lookup(ctx, *atid)
	if err != nil {
		return "", err
	}
	return id.DID, nil
}

// Find desired did
// if other did, forward request there
// if our own did,
// --> if authInfo matches then fulfill the request
// --> otherwise verify requester's token via bff auth --> if they have permissions via permission store --> fulfill request

// GetRecord gets a potentially encrypted record (see s.inner.getRecord)
func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRepoGetRecordParams
	err := formDecoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	targetDID, err := s.fetchDID(r.Context(), params.Repo)
	if err != nil {
		// TODO: write helpful message
		utils.LogAndHTTPError(w, err, "identity lookup", http.StatusBadRequest)
		return
	}

	record, err := s.store.getRecord(params.Collection, params.Rkey, targetDID, callerDID)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			utils.LogAndHTTPError(w, err, "record not found", http.StatusNotFound)
			return
		}
		utils.LogAndHTTPError(w, err, "getting record", http.StatusInternalServerError)
		return
	}
	output := &habitat.NetworkHabitatRepoGetRecordOutput{
		Uri: fmt.Sprintf(
			"habitat://%s/%s/%s",
			targetDID.String(),
			params.Collection,
			params.Rkey,
		),
	}
	if err := json.Unmarshal([]byte(record.Rec), &output.Value); err != nil {
		utils.LogAndHTTPError(w, err, "unmarshalling record", http.StatusInternalServerError)
		return
	}
	if json.NewEncoder(w).Encode(output) != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) getAuthedUser(w http.ResponseWriter, r *http.Request) (syntax.DID, bool) {
	if r.Header.Get("Habitat-Auth-Method") == "oauth" {
		did, ok := s.oauthServer.Validate(w, r)
		if !ok {
			return "", false
		}
		return did, true
	}
	return "", false
}

func (s *Server) UploadBlob(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}

	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("no mimetype specified"),
			"no mimetype specified",
			http.StatusInternalServerError,
		)
		return
	}

	bytes, err := io.ReadAll(r.Body)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusInternalServerError)
		return
	}

	blob, err := s.repo.uploadBlob(string(callerDID), bytes, mimeType)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"error in repo.uploadBlob",
			http.StatusInternalServerError,
		)
		return
	}

	out := habitat.NetworkHabitatRepoUploadBlobOutput{
		Blob: blob,
	}
	err = json.NewEncoder(w).Encode(out)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"error encoding json output",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) GetBlob(w http.ResponseWriter, r *http.Request) {
	/*
		// TODO: implement permissions over getBlob
		callerDID, ok := s.getAuthedUser(w, r)
		if !ok {
			return
		}
	*/

	var params habitat.NetworkHabitatRepoGetBlobParams
	err := formDecoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	mimeType, blob, err := s.repo.getBlob(params.Did, params.Cid)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"error in repo.getBlob",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprint(len(blob)))
	_, err = w.Write(blob)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"error writing getBlob response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) ListRecords(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRepoListRecordsParams
	err := formDecoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	// Handle both @handles and dids
	did, err := s.fetchDID(r.Context(), params.Repo)
	if err != nil {
		utils.LogAndHTTPError(w, err, "identity lookup", http.StatusBadRequest)
		return
	}

	params.Repo = did.String()
	records, err := s.store.listRecords(&params, callerDID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "listing records", http.StatusInternalServerError)
		return
	}

	has, err := s.store.hasRepoForDid(did.String())
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking if repo exists", http.StatusInternalServerError)
		return
	}
	if !has {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("request forwarding not implemented"),
			fmt.Sprintf("could not forward request for did: %s", did.String()),
			http.StatusNotImplemented,
		)
		return
	}

	output := &habitat.NetworkHabitatRepoListRecordsOutput{
		Records: []habitat.NetworkHabitatRepoListRecordsRecord{},
	}
	for _, record := range records {
		rkeyParts := strings.Split(record.Rkey, ".")
		rkey := rkeyParts[len(rkeyParts)-1]
		next := habitat.NetworkHabitatRepoListRecordsRecord{
			Uri: fmt.Sprintf(
				"habitat://%s/%s/%s",
				params.Repo,
				params.Collection,
				rkey,
			),
		}
		if err := json.Unmarshal([]byte(record.Rec), &next.Value); err != nil {
			utils.LogAndHTTPError(w, err, "unmarshalling record", http.StatusInternalServerError)
			return
		}
		output.Records = append(output.Records, next)
	}
	if json.NewEncoder(w).Encode(output) != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) ListPermissions(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	permissions, err := s.store.permissions.ListReadPermissionsByLexicon(callerDID.String())
	if err != nil {
		utils.LogAndHTTPError(w, err, "list permissions from store", http.StatusInternalServerError)
		return
	}

	err = json.NewEncoder(w).Encode(permissions)
	if err != nil {
		utils.LogAndHTTPError(w, err, "json marshal response", http.StatusInternalServerError)
		log.Err(err).Msgf("error sending response for ListPermissions request")
		return
	}
}

type editPermissionRequest struct {
	DID     string `json:"did"`
	Lexicon string `json:"lexicon"`
}

func (s *Server) AddPermission(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	req := &editPermissionRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	err = s.store.permissions.AddLexiconReadPermission(
		[]string{req.DID},
		callerDID.String(),
		req.Lexicon,
	)
	if err != nil {
		utils.LogAndHTTPError(w, err, "adding permission", http.StatusInternalServerError)
		return
	}
}

func (s *Server) RemovePermission(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	req := &editPermissionRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	err = s.store.permissions.RemoveLexiconReadPermission(req.DID, callerDID.String(), req.Lexicon)
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing permission", http.StatusInternalServerError)
		return
	}
}

func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	notifications, err := s.inbox.getNotificationsByDid(callerDID.String())
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting notifications", http.StatusInternalServerError)
		return
	}

	resp := habitat.NetworkHabitatNotificationListNotificationsOutput{
		Records: []habitat.NetworkHabitatNotificationListNotificationsRecord{},
	}
	// TODO: properly fill in the CID
	for _, notification := range notifications {
		resp.Records = append(
			resp.Records,
			habitat.NetworkHabitatNotificationListNotificationsRecord{
				Uri: fmt.Sprintf(
					"habitat://%s/%s/%s",
					notification.OriginDid,
					notification.Collection,
					notification.Rkey,
				),
				Value: habitat.NetworkHabitatNotificationListNotificationsNotification{
					OriginDid:  notification.OriginDid,
					Collection: notification.Collection,
					Rkey:       notification.Rkey,
				},
			},
		)
	}
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) CreateNotification(w http.ResponseWriter, r *http.Request) {
	_, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	input := habitat.NetworkHabitatNotificationCreateNotificationInput{}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusInternalServerError)
		return
	}
	log.Info().Msgf("body: %s", string(body))
	err = json.Unmarshal(body, &input)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}

	log.Info().Msgf("input: %+v", input)

	body, err = json.Marshal(input)
	if err != nil {
		utils.LogAndHTTPError(w, err, "marshal json request", http.StatusInternalServerError)
		return
	}

	did, ok := s.oauthServer.Validate(w, r)
	if !ok {
		return
	}
	id, err := s.dir.LookupDID(r.Context(), did)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to lookup identity", http.StatusBadRequest)
		return
	}
	dpopClient := oauthclient.NewAuthedDpopHttpClient(
		id,
		s.credStore,
		s.oauthClient,
		&oauthclient.MemoryNonceProvider{},
	)
	req, err := http.NewRequest(
		http.MethodPost,
		"/xrpc/com.atproto.repo.createRecord",
		bytes.NewReader(body),
	)
	if err != nil {
		utils.LogAndHTTPError(w, err, "create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := dpopClient.Do(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "forward request", http.StatusInternalServerError)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading response body", http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("failed to create notification: %s: %s", resp.Status, string(respBody)),
			"create notification",
			resp.StatusCode,
		)
		return
	}

	log.Info().Msgf("response body: %s", string(respBody))

	output := &habitat.NetworkHabitatNotificationCreateNotificationOutput{}
	if err := json.Unmarshal(respBody, output); err != nil {
		utils.LogAndHTTPError(w, err, "unmarshalling response", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}
