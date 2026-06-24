package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"

	"github.com/gorilla/schema"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/utils"

	habitat_err "github.com/habitat-network/habitat/internal/error"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type authMethods struct {
	oauth       authn.Method
	serviceAuth authn.Method
}

type Server struct {
	// Implementation of permission-enforcing atprotocol repo
	pear pear.Pear

	// Org store for membership lookups
	orgStore org.Store

	authMethods authMethods
	decoder     *schema.Decoder
}

// NewServer returns a pear server.
func NewServer(
	pear pear.Pear,
	oauthServer *oauthserver.OAuthServer,
	serviceAuthMethod authn.Method,
	orgStore org.Store,
) *Server {
	server := &Server{
		pear: pear,
		authMethods: authMethods{
			oauth:       oauthServer,
			serviceAuth: serviceAuthMethod,
		},
		decoder:  schema.NewDecoder(),
		orgStore: orgStore,
	}
	return server
}

// PutRecord puts a private record (see s.inner.putRecord)
func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	// TODO: only allow Puts if they have onboarded to habitat. Possibly factor this out into authn.Validate

	var req habitat.NetworkHabitatRepoPutRecordInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	target, err := syntax.ParseAtIdentifier(req.Repo)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing at identifier", http.StatusBadRequest)
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
			r.Context(),
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
			r.Context(),
			w,
			err,
			fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
			http.StatusInternalServerError,
		)
		return
	}

	v := true
	uri, err := s.pear.PutRecord(
		r.Context(),
		credInfo.Subject,
		target.DID(),
		syntax.NSID(req.Collection),
		record,
		syntax.RecordKey(rkey),
		&v,
		parsed,
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			fmt.Sprintf("putting record for did %s", target.DID().String()),
			http.StatusInternalServerError,
		)
		return
	}

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatRepoPutRecordOutput{
		Uri: uri.String(),
	}); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

// CreateRecord creates a new record
func (s *Server) CreateRecord(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatRepoCreateRecordInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	target, err := syntax.ParseAtIdentifier(req.Repo)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing at identifier", http.StatusBadRequest)
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
			r.Context(),
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
			r.Context(),
			w,
			err,
			fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
			http.StatusInternalServerError,
		)
		return
	}

	v := true
	uri, err := s.pear.CreateRecord(
		r.Context(),
		credInfo.Subject,
		target.DID(),
		syntax.NSID(req.Collection),
		record,
		syntax.RecordKey(rkey),
		&v,
		parsed,
	)
	if errors.Is(err, repo.ErrRecordAlreadyCreated) {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			fmt.Sprintf("putting record for did %s", target.DID().String()),
			http.StatusConflict,
		)
	} else if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			fmt.Sprintf("putting record for did %s", target.DID().String()),
			http.StatusInternalServerError,
		)
		return
	}

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatRepoCreateRecordOutput{
		Uri: uri.String(),
	}); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

// GetRecord gets a potentially encrypted record (see s.inner.getRecord)
func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth, s.authMethods.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRepoGetRecordParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing url", http.StatusBadRequest)
		return
	}

	target, err := syntax.ParseAtIdentifier(params.Repo)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing repo", http.StatusBadRequest)
		return
	}

	collection, err := syntax.ParseNSID(params.Collection)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"parsing collection as NSID",
			http.StatusBadRequest,
		)
		return
	}
	rkey, err := syntax.ParseRecordKey(params.Rkey)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"parsing rkey as RecordKey",
			http.StatusBadRequest,
		)
		return
	}

	record, err := s.pear.GetRecord(r.Context(), collection, rkey, target.DID(), credInfo.Subject)
	if err != nil {
		if errors.Is(err, repo.ErrRecordNotFound) {
			utils.LogAndHTTPError(r.Context(), w, err, "record not found", http.StatusNotFound)
			return
		} else if errors.Is(err, pear.ErrNotLocalRepo) {
			// TODO: is this still relevant?
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"forwarding not implemented",
				http.StatusNotImplemented,
			)
			return
		} else if errors.Is(err, habitat_err.ErrUnauthorized) {
			utils.LogAndHTTPError(r.Context(), w, err, "unauthorized", http.StatusForbidden)
			return
		}
		utils.LogAndHTTPError(r.Context(), w, err, "getting record", http.StatusInternalServerError)
		return
	}

	output := &habitat.NetworkHabitatRepoGetRecordOutput{
		Uri: fmt.Sprintf(
			"habitat://%s/%s/%s",
			target.DID().String(),
			collection,
			rkey,
		),
	}
	output.Value = record.Value

	// Lookup relevant permissions, if requested
	if params.IncludePermissions {
		grantees, err := s.pear.ListAllowGrantsForRecord(
			r.Context(),
			credInfo.Subject,
			syntax.DID(record.Did),
			syntax.NSID(record.Collection),
			syntax.RecordKey(record.Rkey),
		)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"listing permissions on fetched records",
				http.StatusInternalServerError,
			)
			return
		}
		output.Permissions = permissions.ConstructInterfaceFromGrantees(grantees)
	}

	if json.NewEncoder(w).Encode(output) != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) UploadBlob(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			fmt.Errorf("no mimetype specified"),
			"no mimetype specified",
			http.StatusInternalServerError,
		)
		return
	}

	bytes, err := io.ReadAll(r.Body)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"reading request body",
			http.StatusInternalServerError,
		)
		return
	}

	blob, err := s.pear.UploadBlob(r.Context(), credInfo.Subject, credInfo.Subject, bytes, mimeType)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
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
			r.Context(),
			w,
			err,
			"error encoding json output",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	req := &habitat.NetworkHabitatRepoDeleteRecordInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode json request", http.StatusBadRequest)
		return
	}

	repo, err := syntax.ParseAtIdentifier(req.Repo)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse repo", http.StatusBadRequest)
		return
	}

	err = s.pear.DeleteRecord(
		r.Context(),
		credInfo.Subject,
		repo.DID(),
		syntax.NSID(req.Collection),
		syntax.RecordKey(req.Rkey),
	)
	if err != nil {
		if errors.Is(err, habitat_err.ErrUnauthorized) {
			utils.LogAndHTTPError(r.Context(), w, err, "unauthorized", http.StatusForbidden)
			return
		}
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"error deleting record",
			http.StatusInternalServerError,
		)
		return
	}
}

// TODO: implement permissions over getBlob
func (s *Server) GetBlob(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(
			s.authMethods.oauth,
		), /* TODO: add service auth here when we support fwding blob reqs */
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRepoGetBlobParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing url", http.StatusBadRequest)
		return
	}

	did, err := syntax.ParseDID(params.Did)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing did", http.StatusBadRequest)
		return
	}

	cid, err := syntax.ParseCID(params.Cid)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing cid", http.StatusBadRequest)
		return
	}

	mimeType, contentLen, blob, err := s.pear.GetBlob(r.Context(), credInfo.Subject, did, cid)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"error in repo.getBlob",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", contentLen)
	_, err = io.Copy(w, blob)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"error writing getBlob response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) ListRecords(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth, s.authMethods.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRepoListRecordsParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing request params", http.StatusBadRequest)
		return
	}

	subjects := make([]syntax.AtIdentifier, len(params.Subjects))
	for i, subject := range params.Subjects {
		// TODO: support handles
		atid, err := syntax.ParseAtIdentifier(subject)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				fmt.Sprintf("parsing subject as did or handle: %s", subject),
				http.StatusBadRequest,
			)
			return
		}
		subjects[i] = atid
	}

	collection, err := syntax.ParseNSID(params.Collection)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing collection", http.StatusBadRequest)
		return
	}

	records, err := s.pear.ListRecords(r.Context(), credInfo.Subject, collection, subjects)
	if err != nil {
		if errors.Is(err, pear.ErrNotLocalRepo) {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"forwarding not implemented",
				http.StatusNotImplemented,
			)
			return
		}
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"listing records",
			http.StatusInternalServerError,
		)
		return
	}

	output := &habitat.NetworkHabitatRepoListRecordsOutput{
		Records: []habitat.NetworkHabitatRepoListRecordsRecord{},
	}

	for _, record := range records {
		next := habitat.NetworkHabitatRepoListRecordsRecord{
			Uri: fmt.Sprintf(
				"habitat://%s/%s/%s",
				record.Did,
				record.Collection,
				record.Rkey,
			),
		}
		next.Value = record.Value

		// Lookup relevant permissions, if requested
		if params.IncludePermissions {
			grantees, err := s.pear.ListAllowGrantsForRecord(
				r.Context(),
				credInfo.Subject,
				syntax.DID(record.Did),
				syntax.NSID(record.Collection),
				syntax.RecordKey(record.Rkey),
			)
			if err != nil {
				if errors.Is(err, habitat_err.ErrUnauthorized) {
					slog.ErrorContext(r.Context(),
						"[pear] list records inconsistent state",
						"caller",
						credInfo.Subject,
						"uri",
						habitat_syntax.ConstructHabitatUri(
							record.Did,
							record.Collection,
							record.Rkey,
						),
						"err",
						fmt.Errorf(
							"list records returned a record but user does not have permission to it",
						),
					)
				}
				utils.LogAndHTTPError(
					r.Context(),
					w,
					err,
					"listing permissions on fetched records",
					http.StatusInternalServerError,
				)
				return
			}
			next.Permissions = permissions.ConstructInterfaceFromGrantees(grantees)
		}
		// TODO: next.Cid = ?

		output.Records = append(output.Records, next)
	}
	if json.NewEncoder(w).Encode(output) != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) DescribeRepo(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	description, err := s.pear.DescribeRepo(r.Context(), credInfo.Subject, credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"describing repo",
			http.StatusInternalServerError,
		)
		return
	}

	output := habitat.NetworkHabitatRepoDescribeRepoOutput{
		Did:             description.DID.String(),
		Handle:          description.Handle,
		DidDoc:          description.DIDDoc,
		HandleIsCorrect: description.HandleIsCorrect,
		Collections: make(
			[]habitat.NetworkHabitatRepoDescribeRepoCollectionMetadata,
			len(description.Collections),
		),
	}

	for i, c := range description.Collections {
		grantees := permissions.ConstructInterfaceFromGrantees(c.Grantees)
		output.Collections[i] = habitat.NetworkHabitatRepoDescribeRepoCollectionMetadata{
			Grantees:    grantees,
			LastTouched: c.LastTouched.Format(time.RFC3339Nano),
			Nsid:        c.Name,
			RecordCount: int64(c.RecordCount),
		}
	}

	if err = json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

// TODO: this is a confusing name, because our ListPermissions internally takes in a generic query of grantee + owner + collection + rkey
// and returns the permissions that exist on that combination.
//
// However, this is currently only used in the UI to show all the permissions a particular user has granted to other people, as a way of
// inspecting and easily adding / removing permission grants on your data. We should rename this and/or also make it generic.
func (s *Server) ListPermissions(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}
	perms, err := s.pear.ListPermissionGrants(r.Context(), credInfo.Subject, credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"list permissions from store",
			http.StatusInternalServerError,
		)
		return
	}

	var output habitat.NetworkHabitatPermissionsListPermissionsOutput
	output.Permissions = []habitat.NetworkHabitatPermissionsListPermissionsPermission{}
	for _, p := range perms {
		// Only display allows
		output.Permissions = append(
			output.Permissions,
			habitat.NetworkHabitatPermissionsListPermissionsPermission{
				Collection: p.Collection.String(),
				Grantee:    p.Grantee.String(),
				Rkey:       p.Rkey.String(),
			},
		)
	}

	err = json.NewEncoder(w).Encode(output)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"json marshal response",
			http.StatusInternalServerError,
		)
		slog.ErrorContext(
			r.Context(),
			"error sending response for ListPermissions request",
			"err",
			err,
		)
		return
	}
}

func (s *Server) AddPermission(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}
	req := &habitat.NetworkHabitatPermissionsAddPermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode json request", http.StatusBadRequest)
		return
	}

	grantees, err := permissions.ParseGranteesFromInterface(req.Grantees)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode json request", http.StatusBadRequest)
		return
	}
	err = s.pear.AddPermissions(
		r.Context(),
		credInfo.Subject,
		grantees,
		credInfo.Subject,
		syntax.NSID(req.Collection),
		syntax.RecordKey(req.Rkey),
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"adding permission",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) RemovePermission(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.authMethods.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}
	req := &habitat.NetworkHabitatPermissionsRemovePermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode json request", http.StatusBadRequest)
		return
	}

	grantees, err := permissions.ParseGranteesFromInterface(req.Grantees)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
			http.StatusInternalServerError,
		)
		return
	}
	err = s.pear.RemovePermissions(
		r.Context(),
		credInfo.Subject,
		grantees,
		credInfo.Subject,
		syntax.NSID(req.Collection),
		syntax.RecordKey(req.Rkey),
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"removing permission",
			http.StatusInternalServerError,
		)
		return
	}
}
