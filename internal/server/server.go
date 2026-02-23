package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gorilla/schema"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/utils"
)

type authMethods struct {
	oauth       authn.Method
	serviceAuth authn.Method
}

type Server struct {
	// Implementation of permission-enforcint atprotocol repo
	pear pear.Pear
	// Used for resolving handles -> did, did -> PDS
	dir identity.Directory

	authMethods authMethods
}

// NewServer returns a pear server.
func NewServer(
	dir identity.Directory,
	pear pear.Pear,
	oauthServer *oauthserver.OAuthServer,
	serviceAuthMethod authn.Method,
) *Server {
	server := &Server{
		dir:  dir,
		pear: pear,
		authMethods: authMethods{
			oauth:       oauthServer,
			serviceAuth: serviceAuthMethod,
		},
	}
	return server
}

var formDecoder = schema.NewDecoder()

// PutRecord puts a potentially encrypted record (see s.inner.putRecord)
func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
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

	parsed, err := permissions.ParseGranteesFromInterface(req.Grantees)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
			http.StatusInternalServerError,
		)
		return
	}

	v := true
	uri, err := s.pear.PutRecord(r.Context(), callerDID, ownerDID, syntax.NSID(req.Collection), record, syntax.RecordKey(rkey), &v, parsed)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			fmt.Sprintf("putting record for did %s", ownerDID.String()),
			http.StatusInternalServerError,
		)
		return
	}

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatRepoPutRecordOutput{
		Uri: uri.String(),
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

/*
func (s *Server) fetchDIDs(ctx context.Context, didOrHandles []string) ([]syntax.DID, error) {
	dids := make([]syntax.DID, len(didOrHandles))
	for i, did := range didOrHandles {
		resolved, err := s.fetchDID(ctx, did)
		if err != nil {
			return nil, err
		}
		dids[i] = resolved
	}
	return dids, nil
}
*/

// Find desired did
// if other did, forward request there
// if our own did,
// --> if authInfo matches then fulfill the request
// --> otherwise verify requester's token via bff auth --> if they have permissions via permission store --> fulfill request

// GetRecord gets a potentially encrypted record (see s.inner.getRecord)
func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth, s.authMethods.serviceAuth)
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
		utils.LogAndHTTPError(w, err, "identity lookup", http.StatusBadRequest)
		return
	}

	record, err := s.pear.GetRecord(r.Context(), syntax.NSID(params.Collection), syntax.RecordKey(params.Rkey), targetDID, callerDID)
	if err != nil {
		if errors.Is(err, repo.ErrRecordNotFound) {
			utils.LogAndHTTPError(w, err, "record not found", http.StatusNotFound)
			return
		} else if errors.Is(err, pear.ErrNotLocalRepo) {
			// TODO: is this still relevant?
			utils.LogAndHTTPError(w, err, "forwarding not implemented", http.StatusNotImplemented)
			return
		} else if errors.Is(err, pear.ErrUnauthorized) {
			utils.LogAndHTTPError(w, err, "unauthorized", http.StatusForbidden)
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
	output.Value = record.Value
	if json.NewEncoder(w).Encode(output) != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) UploadBlob(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
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

	blob, err := s.pear.UploadBlob(r.Context(), string(callerDID), bytes, mimeType)
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

// TODO: implement permissions over getBlob
func (s *Server) GetBlob(w http.ResponseWriter, r *http.Request) {
	var params habitat.NetworkHabitatRepoGetBlobParams
	err := formDecoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	mimeType, blob, err := s.pear.GetBlob(r.Context(), params.Did, params.Cid)
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
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth, s.authMethods.serviceAuth)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatListRecordsInput
	err := formDecoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	// TODO: fix this
	if len(params.Subjects) > 0 {
		utils.LogAndHTTPError(w, err, "don't allow filters by repo yet", http.StatusBadRequest)
		return
	}

	dids := make([]syntax.DID, len(params.Subjects))
	for i, subject := range params.Subjects {
		// TODO: support handles
		atid, err := syntax.ParseAtIdentifier(subject)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parsing subject as did or handle", http.StatusBadRequest)
			return
		}

		id, err := s.dir.Lookup(r.Context(), *atid)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parsing looking up atid", http.StatusBadRequest)
			return
		}
		dids[i] = id.DID
	}

	records, err := s.pear.ListRecords(r.Context(), callerDID, syntax.NSID(params.Collection), dids)
	if err != nil {
		if errors.Is(err, pear.ErrNotLocalRepo) {
			utils.LogAndHTTPError(w, err, "forwarding not implemented", http.StatusNotImplemented)
			return
		}
		utils.LogAndHTTPError(w, err, "listing records", http.StatusInternalServerError)
		return
	}

	output := &habitat.NetworkHabitatListRecordsOutput{
		Records: []habitat.NetworkHabitatListRecordsRecord{},
	}
	for _, record := range records {
		next := habitat.NetworkHabitatListRecordsRecord{
			Uri: fmt.Sprintf(
				"habitat://%s/%s/%s",
				record.Did,
				params.Collection,
				record.Rkey,
			),
		}
		next.Value = record.Value
		// TODO: next.Cid = ?

		output.Records = append(output.Records, next)
	}
	if json.NewEncoder(w).Encode(output) != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

// TODO: this is a confusing name, because our ListPermissions internally takes in a generic query of grantee + owner + collection + rkey
// and returns the permissions that exist on that combination.
//
// However, this is currently only used in the UI to show all the permissions a particular user has granted to other people, as a way of
// inspecting and easily adding / removing permission grants on your data. We should rename this and/or also make it generic.
func (s *Server) ListPermissions(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		return
	}
	perms, err := s.pear.ListPermissionGrants(r.Context(), callerDID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "list permissions from store", http.StatusInternalServerError)
		return
	}

	var output habitat.NetworkHabitatPermissionsListPermissionsOutput
	output.Permissions = []habitat.NetworkHabitatPermissionsListPermissionsPermission{}
	for _, p := range perms {
		// Only display allows
		if p.Effect == permissions.Deny {
			continue
		}
		output.Permissions = append(output.Permissions, habitat.NetworkHabitatPermissionsListPermissionsPermission{
			Collection: p.Collection.String(),
			Effect:     string(p.Effect),
			Grantee:    p.Grantee.String(),
			Rkey:       p.Rkey.String(),
		})
	}

	err = json.NewEncoder(w).Encode(output)
	if err != nil {
		utils.LogAndHTTPError(w, err, "json marshal response", http.StatusInternalServerError)
		log.Err(err).Msgf("error sending response for ListPermissions request")
		return
	}
}

func (s *Server) AddPermission(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		return
	}
	req := &habitat.NetworkHabitatPermissionsAddPermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}

	grantees, err := permissions.ParseGranteesFromInterface(req.Grantees)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	err = s.pear.AddPermissions(
		grantees,
		callerDID,
		syntax.NSID(req.Collection),
		syntax.RecordKey(req.Rkey),
	)
	if err != nil {
		utils.LogAndHTTPError(w, err, "adding permission", http.StatusInternalServerError)
		return
	}
}

func (s *Server) RemovePermission(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		return
	}
	req := &habitat.NetworkHabitatPermissionsRemovePermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}

	grantees, err := permissions.ParseGranteesFromInterface(req.Grantees)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
			http.StatusInternalServerError,
		)
		return
	}
	err = s.pear.RemovePermissions(grantees, callerDID, syntax.NSID(req.Collection), syntax.RecordKey(req.Rkey))
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing permission", http.StatusInternalServerError)
		return
	}
}

func (s *Server) NotifyOfUpdate(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.serviceAuth)
	if !ok {
		return
	}

	req := &habitat.NetworkHabitatInternalNotifyOfUpdateInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}

	err = s.pear.NotifyOfUpdate(
		r.Context(),
		callerDID,
		syntax.DID(req.Recipient),
		req.Collection,
		req.Rkey,
	)
	if err != nil {
		utils.LogAndHTTPError(w, err, "notify of update", http.StatusInternalServerError)
		return
	}
}
