package pear

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
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/utils"
)

type Server struct {
	// Implementation of permission-enforcint atprotocol repo
	pear *Pear
	// Used for resolving handles -> did, did -> PDS
	dir identity.Directory
	// Used for validating oauth tokens
	oauthServer *oauthserver.OAuthServer
}

// NewServer returns a pear server.
func NewServer(
	dir identity.Directory,
	pear *Pear,
	oauthServer *oauthserver.OAuthServer,
) *Server {
	server := &Server{
		dir:         dir,
		pear:        pear,
		oauthServer: oauthServer,
	}
	return server
}

var formDecoder = schema.NewDecoder()

type didGrantee string

func (didGrantee) isGrantee() {}

type cliqueGrantee string

func (cliqueGrantee) isGrantee() {}

type grantee interface {
	isGrantee()
}

// Parse the grantees input which is typed as an interface
func parseGrantees(grantees []interface{}) ([]grantee, error) {
	parsed := make([]grantee, len(grantees))
	for i, generic := range grantees {
		unknownGrantee, ok := generic.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected type in grantees field: %v", generic)
		}

		granteeType, ok := unknownGrantee["$type"]
		if !ok {
			return nil, fmt.Errorf("malformatted grantee has no $type field: %v", unknownGrantee)
		}

		switch granteeType {
		case "network.habitat.repo.putRecord#didGrantee":
			did, ok := unknownGrantee["did"]
			if !ok {
				return nil, fmt.Errorf("malformatted did grantee has no did field: %v", unknownGrantee)
			}
			asStr, ok := did.(string)
			if !ok {
				return nil, fmt.Errorf("malformatted did grantee has non-string did field: %v", unknownGrantee)
			}
			parsed[i] = didGrantee(asStr)
		case "network.habitat.repo.putRecord#cliqueRef":
			uri, ok := unknownGrantee["uri"]
			if !ok {
				return nil, fmt.Errorf("malformatted clique grantee has no uri field: %v", unknownGrantee)
			}
			asStr, ok := uri.(string)
			if !ok {
				return nil, fmt.Errorf("malformatted clique grantee has non-string uri field: %v", unknownGrantee)
			}
			parsed[i] = cliqueGrantee(asStr)
		default:
			return nil, fmt.Errorf("malformatted grantee has unknown $type of %v: %v", granteeType, unknownGrantee)
		}
	}
	return parsed, nil
}

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

	err = s.pear.putRecord(ownerDID.String(), req.Collection, record, rkey, &v)
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
		parsed, err := parseGrantees(req.Grantees)
		if err != nil {
			utils.LogAndHTTPError(
				w,
				err,
				fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
				http.StatusInternalServerError,
			)
			return
		}

		didGrantees := []string{}
		for _, grantee := range parsed {
			switch g := grantee.(type) {
			case didGrantee:
				didGrantees = append(didGrantees, string(g))
			case cliqueGrantee:
				// If we ever run into a non-DID grantee, return an error as this is not yet supported
				utils.LogAndHTTPError(
					w,
					err,
					fmt.Sprintf("non-DID grantees are not supported: %v", grantee),
					http.StatusInternalServerError,
				)
				return
			}
		}

		err = s.pear.permissions.AddReadPermission(
			didGrantees,
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

	record, err := s.pear.getRecord(params.Collection, params.Rkey, targetDID, callerDID)
	if err != nil {
		if errors.Is(err, repo.ErrRecordNotFound) {
			utils.LogAndHTTPError(w, err, "record not found", http.StatusNotFound)
			return
		} else if errors.Is(err, ErrNotLocalRepo) {
			utils.LogAndHTTPError(w, err, "forwarding not implemented", http.StatusNotImplemented)
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
	if err := json.Unmarshal([]byte(record.Value), &output.Value); err != nil {
		utils.LogAndHTTPError(w, err, "unmarshalling record", http.StatusInternalServerError)
		return
	}
	if json.NewEncoder(w).Encode(output) != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

// getAuthedUser attempts to get the calling user from the Habitat-Auth-Method header which uses oauth.
// If this fails, it will write an http error response with the appropriate status, so no need for the caller to do that.
func (s *Server) getAuthedUser(w http.ResponseWriter, r *http.Request) (syntax.DID, bool) {
	if r.Header.Get("Habitat-Auth-Method") == "oauth" {
		// If the header could not be validated, an error response is written by Validate()
		did, ok := s.oauthServer.Validate(w, r)
		if !ok {
			return "", false
		}
		return did, true
	}
	// If no header was provided, also write an err
	utils.WriteHTTPError(w, fmt.Errorf("no habitat auth header provided"), http.StatusUnauthorized)
	return "", false
}

func (s *Server) getServiceAuthedUser(w http.ResponseWriter, r *http.Request) (syntax.DID, bool) {
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

	blob, err := s.pear.uploadBlob(string(callerDID), bytes, mimeType)
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

	mimeType, blob, err := s.pear.getBlob(params.Did, params.Cid)
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

	var params habitat.NetworkHabitatListRecordsInput
	err := formDecoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	// TODO: this is wrong
	repo := params.Subjects[0]

	// Handle both @handles and dids
	did, err := s.fetchDID(r.Context(), repo)
	if err != nil {
		utils.LogAndHTTPError(w, err, "identity lookup", http.StatusBadRequest)
		return
	}

	repo = did.String()
	records, err := s.pear.listRecords(did, params.Collection, callerDID)
	if err != nil {
		if errors.Is(err, ErrNotLocalRepo) {
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
				repo,
				params.Collection,
				record.Rkey,
			),
		}
		if err := json.Unmarshal([]byte(record.Value), &next.Value); err != nil {
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
	permissions, err := s.pear.permissions.ListReadPermissionsByLexicon(callerDID.String())
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

func (s *Server) AddPermission(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	req := &habitat.NetworkHabitatPermissionsAddPermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	err = s.pear.permissions.AddReadPermission(
		[]string{req.Did},
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
	req := &habitat.NetworkHabitatPermissionsRemovePermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	err = s.pear.permissions.RemoveReadPermission(req.Did, callerDID.String(), req.Lexicon)
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing permission", http.StatusInternalServerError)
		return
	}
}

func (s *Server) NotifyOfUpdate(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getServiceAuthedUser(w, r)
	if !ok {
		return
	}

	req := &habitat.NetworkHabitatInternalNotifyOfUpdateInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}

	err = s.pear.notifyOfUpdate(r.Context(), callerDID, syntax.DID(req.Recipient), req.Collection, req.Rkey)
	if err != nil {
		utils.LogAndHTTPError(w, err, "notify of update", http.StatusInternalServerError)
		return
	}
}
