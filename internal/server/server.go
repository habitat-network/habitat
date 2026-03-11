package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/gorilla/schema"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/utils"

	habitat_err "github.com/habitat-network/habitat/internal/error"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type authMethods struct {
	oauth       authn.Method
	serviceAuth authn.Method
}

type routeMetrics struct {
	routeErrCtr      metric.Int64Counter
	routeSuccessCtr  metric.Int64Counter
	routeLatencyHist metric.Int64Histogram
}

type metrics struct {
	routes map[string]routeMetrics
}

func (m *metrics) reportRouteErr(route string, statusCode int) {
	reporter, ok := m.routes[route]
	if !ok {
		return
	}

	reporter.routeErrCtr.Add(context.Background(), 1, metric.WithAttributes(attribute.String("status", http.StatusText(statusCode))))
}

func (m *metrics) reportRouteSuccess(route string) {
	reporter, ok := m.routes[route]
	if !ok {
		return
	}

	reporter.routeSuccessCtr.Add(context.Background(), 1)
}

func (m *metrics) reportRouteLatency(route string, ns int64) {
	reporter, ok := m.routes[route]
	if !ok {
		return
	}
	reporter.routeLatencyHist.Record(context.Background(), ns)
}

type Server struct {
	// For reporting
	metrics *metrics
	// Implementation of permission-enforcint atprotocol repo
	pear pear.Pear
	// Used for resolving handles -> did, did -> PDS
	dir identity.Directory

	authMethods authMethods
	decoder     *schema.Decoder
}

// NewServer returns a pear server.
func NewServer(
	meter metric.Meter,
	dir identity.Directory,
	pear pear.Pear,
	oauthServer *oauthserver.OAuthServer,
	serviceAuthMethod authn.Method,
) (*Server, error) {
	m := &metrics{
		routes: make(map[string]routeMetrics),
	}
	for _, route := range Routes {
		errCtr, err := meter.Int64Counter(fmt.Sprintf("pear.%s.err", route), metric.WithUnit("item"))
		if err != nil {
			return nil, err
		}
		successCtr, err := meter.Int64Counter(fmt.Sprintf("pear.%s.success", route), metric.WithUnit("item"))
		if err != nil {
			return nil, err
		}

		latencyHist, err := meter.Int64Histogram(fmt.Sprintf("pear.%s.latency", route), metric.WithUnit("us"))
		if err != nil {
			return nil, err
		}
		m.routes[route] = routeMetrics{
			routeErrCtr:      errCtr,
			routeSuccessCtr:  successCtr,
			routeLatencyHist: latencyHist,
		}
	}
	server := &Server{
		metrics: m,
		dir:     dir,
		pear:    pear,
		authMethods: authMethods{
			oauth:       oauthServer,
			serviceAuth: serviceAuthMethod,
		},
		decoder: schema.NewDecoder(),
	}
	return server, nil
}

func (s *Server) handlePearErr(w http.ResponseWriter, route string, err error) {
	if errors.Is(err, habitat_err.ErrUnauthorized) {
		switch route {
		case GetRecord, ListRecords, GetBlob, ListCollections, ListPermissions:
			http.Error(w, "not found", http.StatusNotFound)
		case PutRecord, UploadBlob, AddPermission, RemovePermission:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
		s.metrics.reportRouteErr(route, http.StatusUnauthorized)
		return
	}
	s.metrics.reportRouteErr(route, http.StatusInternalServerError)
	utils.LogAndHTTPError(w, err, route, http.StatusInternalServerError)
}

// PutRecord puts a potentially encrypted record (see s.inner.putRecord)
func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(PutRecord, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		s.metrics.reportRouteErr(PutRecord, http.StatusUnauthorized)
		return
	}

	// TODO: only allow Puts if they have onboarded to habitat. Possibly factor this out into authn.Validate
	var req habitat.NetworkHabitatRepoPutRecordInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		s.metrics.reportRouteErr(PutRecord, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	target, err := syntax.ParseAtIdentifier(req.Repo)
	if err != nil {
		s.metrics.reportRouteErr(PutRecord, http.StatusBadRequest)
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
		s.metrics.reportRouteErr(PutRecord, http.StatusBadRequest)
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
		s.metrics.reportRouteErr(PutRecord, http.StatusBadRequest)
		utils.LogAndHTTPError(
			w,
			err,
			fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
			http.StatusBadRequest,
		)
		return
	}

	v := true
	uri, err := s.pear.PutRecord(r.Context(), callerDID, target.DID(), syntax.NSID(req.Collection), record, syntax.RecordKey(rkey), &v, parsed)
	if err != nil {
		s.handlePearErr(w, PutRecord, err)
		return
	}

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatRepoPutRecordOutput{
		Uri: uri.String(),
	}); err != nil {
		s.metrics.reportRouteErr(PutRecord, http.StatusInternalServerError)
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
	s.metrics.reportRouteSuccess(PutRecord)
}

// GetRecord gets a potentially encrypted record (see s.inner.getRecord)
func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(GetRecord, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth, s.authMethods.serviceAuth)
	if !ok {
		s.metrics.reportRouteErr(GetRecord, http.StatusUnauthorized)
		return
	}
	var params habitat.NetworkHabitatRepoGetRecordParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		s.metrics.reportRouteErr(GetRecord, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	target, err := syntax.ParseAtIdentifier(params.Repo)
	if err != nil {
		s.metrics.reportRouteErr(GetRecord, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing repo", http.StatusBadRequest)
		return
	}

	collection, err := syntax.ParseNSID(params.Collection)
	if err != nil {
		s.metrics.reportRouteErr(GetRecord, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing collection as NSID", http.StatusBadRequest)
		return
	}
	rkey, err := syntax.ParseRecordKey(params.Rkey)
	if err != nil {
		s.metrics.reportRouteErr(GetRecord, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing rkey as RecordKey", http.StatusBadRequest)
		return
	}

	record, err := s.pear.GetRecord(r.Context(), collection, rkey, target.DID(), callerDID)
	if err != nil {
		s.handlePearErr(w, GetRecord, err)
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
		grantees, err := s.pear.ListAllowGrantsForRecord(r.Context(), callerDID, syntax.DID(record.Did), syntax.NSID(record.Collection), syntax.RecordKey(record.Rkey))
		if err != nil {
			if errors.Is(err, habitat_err.ErrUnauthorized) {
				log.Err(err).Msgf("fetched a record but received error reading permissions; should never happen: %s", habitat_syntax.ConstructHabitatUri(record.Did, record.Collection, record.Rkey))
			}
			s.metrics.reportRouteErr(GetRecord, http.StatusInternalServerError)
			utils.LogAndHTTPError(w, err, "listing permissions on fetched records", http.StatusInternalServerError)
			return
		}
		output.Permissions = permissions.ConstructInterfaceFromGrantees(grantees)
	}

	if json.NewEncoder(w).Encode(output) != nil {
		s.metrics.reportRouteErr(GetRecord, http.StatusInternalServerError)
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
	s.metrics.reportRouteSuccess(GetRecord)
}

func (s *Server) UploadBlob(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(UploadBlob, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		s.metrics.reportRouteErr(UploadBlob, http.StatusUnauthorized)
		return
	}

	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		s.metrics.reportRouteErr(UploadBlob, http.StatusBadRequest)
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("no mimetype specified"),
			"no mimetype specified",
			http.StatusBadRequest,
		)
		return
	}

	bytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.metrics.reportRouteErr(UploadBlob, http.StatusInternalServerError)
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusInternalServerError)
		return
	}

	blob, err := s.pear.UploadBlob(r.Context(), callerDID, callerDID, bytes, mimeType)
	if err != nil {
		s.handlePearErr(w, UploadBlob, err)
		return
	}

	out := habitat.NetworkHabitatRepoUploadBlobOutput{
		Blob: blob,
	}
	err = json.NewEncoder(w).Encode(out)
	if err != nil {
		s.metrics.reportRouteErr(UploadBlob, http.StatusInternalServerError)
		utils.LogAndHTTPError(
			w,
			err,
			"error encoding json output",
			http.StatusInternalServerError,
		)
		return
	}
	s.metrics.reportRouteSuccess(UploadBlob)
}

// TODO: implement permissions over getBlob
func (s *Server) GetBlob(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(GetBlob, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth /* TODO: add service auth here when we support fwding blob reqs */)
	if !ok {
		s.metrics.reportRouteErr(GetBlob, http.StatusUnauthorized)
		return
	}

	var params habitat.NetworkHabitatRepoGetBlobParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		s.metrics.reportRouteErr(GetBlob, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	did, err := syntax.ParseDID(params.Did)
	if err != nil {
		s.metrics.reportRouteErr(GetBlob, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing did", http.StatusBadRequest)
		return
	}

	cid, err := syntax.ParseCID(params.Cid)
	if err != nil {
		s.metrics.reportRouteErr(GetBlob, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing cid", http.StatusBadRequest)
		return
	}

	mimeType, contentLen, blob, err := s.pear.GetBlob(r.Context(), callerDID, did, cid)
	if err != nil {
		s.handlePearErr(w, GetBlob, err)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", contentLen)
	_, err = io.Copy(w, blob)
	if err != nil {
		s.metrics.reportRouteErr(GetBlob, http.StatusInternalServerError)
		utils.LogAndHTTPError(
			w,
			err,
			"error writing getBlob response",
			http.StatusInternalServerError,
		)
		return
	}
	s.metrics.reportRouteSuccess(GetBlob)
}

func (s *Server) ListRecords(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(ListRecords, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth, s.authMethods.serviceAuth)
	if !ok {
		s.metrics.reportRouteErr(ListRecords, http.StatusUnauthorized)
		return
	}

	var params habitat.NetworkHabitatRepoListRecordsParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		s.metrics.reportRouteErr(ListRecords, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing request params", http.StatusBadRequest)
		return
	}

	dids := make([]syntax.DID, len(params.Subjects))
	for i, subject := range params.Subjects {
		// TODO: support handles
		atid, err := syntax.ParseAtIdentifier(subject)
		if err != nil {
			s.metrics.reportRouteErr(ListRecords, http.StatusBadRequest)
			utils.LogAndHTTPError(w, err, fmt.Sprintf("parsing subject as did or handle: %s", subject), http.StatusBadRequest)
			return
		}

		id, err := s.dir.Lookup(r.Context(), atid)
		if err != nil {
			s.metrics.reportRouteErr(ListRecords, http.StatusBadRequest)
			utils.LogAndHTTPError(w, err, "parsing looking up atid", http.StatusBadRequest)
			return
		}
		dids[i] = id.DID
	}

	collection, err := syntax.ParseNSID(params.Collection)
	if err != nil {
		s.metrics.reportRouteErr(ListRecords, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "parsing collection", http.StatusBadRequest)
		return
	}

	records, err := s.pear.ListRecords(r.Context(), callerDID, collection, dids)
	if err != nil {
		s.handlePearErr(w, ListRecords, err)
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
			grantees, err := s.pear.ListAllowGrantsForRecord(r.Context(), callerDID, syntax.DID(record.Did), syntax.NSID(record.Collection), syntax.RecordKey(record.Rkey))
			if err != nil {
				if errors.Is(err, habitat_err.ErrUnauthorized) {
					log.Err(fmt.Errorf("list records returned a record but user does not have permission to it")).Msgf("[pear] list records inconsistent state for caller %s on %s", callerDID, habitat_syntax.ConstructHabitatUri(record.Did, record.Collection, record.Rkey))
				}
				s.metrics.reportRouteErr(ListRecords, http.StatusInternalServerError)
				utils.LogAndHTTPError(w, err, "listing permissions on fetched records", http.StatusInternalServerError)
				return
			}
			next.Permissions = permissions.ConstructInterfaceFromGrantees(grantees)
		}
		// TODO: next.Cid = ?

		output.Records = append(output.Records, next)
	}
	if json.NewEncoder(w).Encode(output) != nil {
		s.metrics.reportRouteErr(ListRecords, http.StatusInternalServerError)
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
	s.metrics.reportRouteSuccess(ListRecords)
}

func (s *Server) ListCollections(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(ListCollections, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		s.metrics.reportRouteErr(ListCollections, http.StatusUnauthorized)
		return
	}

	collections, err := s.pear.ListCollections(r.Context(), callerDID, callerDID)
	if err != nil {
		s.handlePearErr(w, ListCollections, err)
		return
	}

	var output habitat.NetworkHabitatRepoListCollectionsOutput
	output.Collections = make([]habitat.NetworkHabitatRepoListCollectionsCollectionMetadata, len(collections))

	for i, c := range collections {
		grantees := permissions.ConstructInterfaceFromGrantees(c.Grantees)
		output.Collections[i] = habitat.NetworkHabitatRepoListCollectionsCollectionMetadata{
			Grantees:    grantees,
			LastTouched: c.LastTouched.Format(time.RFC3339Nano),
			Nsid:        c.Name,
			RecordCount: int64(c.RecordCount),
		}
	}

	err = json.NewEncoder(w).Encode(output)
	if err != nil {
		s.metrics.reportRouteErr(ListCollections, http.StatusInternalServerError)
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
	s.metrics.reportRouteSuccess(ListCollections)
}

// TODO: this is a confusing name, because our ListPermissions internally takes in a generic query of grantee + owner + collection + rkey
// and returns the permissions that exist on that combination.
//
// However, this is currently only used in the UI to show all the permissions a particular user has granted to other people, as a way of
// inspecting and easily adding / removing permission grants on your data. We should rename this and/or also make it generic.
func (s *Server) ListPermissions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(ListPermissions, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		s.metrics.reportRouteErr(ListPermissions, http.StatusUnauthorized)
		return
	}
	perms, err := s.pear.ListPermissionGrants(r.Context(), callerDID, callerDID)
	if err != nil {
		s.handlePearErr(w, ListPermissions, err)
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
		s.metrics.reportRouteErr(ListPermissions, http.StatusInternalServerError)
		utils.LogAndHTTPError(w, err, "json marshal response", http.StatusInternalServerError)
		return
	}
	s.metrics.reportRouteSuccess(ListPermissions)
}

func (s *Server) AddPermission(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(AddPermission, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		s.metrics.reportRouteErr(AddPermission, http.StatusUnauthorized)
		return
	}
	req := &habitat.NetworkHabitatPermissionsAddPermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		s.metrics.reportRouteErr(AddPermission, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}

	grantees, err := permissions.ParseGranteesFromInterface(req.Grantees)
	if err != nil {
		s.metrics.reportRouteErr(AddPermission, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	err = s.pear.AddPermissions(
		callerDID,
		grantees,
		callerDID,
		syntax.NSID(req.Collection),
		syntax.RecordKey(req.Rkey),
	)
	if err != nil {
		s.handlePearErr(w, AddPermission, err)
		return
	}
	s.metrics.reportRouteSuccess(AddPermission)
}

func (s *Server) RemovePermission(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(RemovePermission, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth)
	if !ok {
		s.metrics.reportRouteErr(RemovePermission, http.StatusUnauthorized)
		return
	}
	req := &habitat.NetworkHabitatPermissionsRemovePermissionInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		s.metrics.reportRouteErr(RemovePermission, http.StatusBadRequest)
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}

	grantees, err := permissions.ParseGranteesFromInterface(req.Grantees)
	if err != nil {
		s.metrics.reportRouteErr(RemovePermission, http.StatusBadRequest)
		utils.LogAndHTTPError(
			w,
			err,
			fmt.Sprintf("unable to parse grantees field: %v", req.Grantees),
			http.StatusBadRequest,
		)
		return
	}
	err = s.pear.RemovePermissions(callerDID, grantees, callerDID, syntax.NSID(req.Collection), syntax.RecordKey(req.Rkey))
	if err != nil {
		s.handlePearErr(w, RemovePermission, err)
		return
	}
	s.metrics.reportRouteSuccess(RemovePermission)
}

func (s *Server) NotifyOfUpdate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer s.metrics.reportRouteLatency(NotifyOfUpdate, time.Since(start).Nanoseconds())

	callerDID, ok := authn.Validate(w, r, s.authMethods.serviceAuth)
	if !ok {
		s.metrics.reportRouteErr(NotifyOfUpdate, http.StatusUnauthorized)
		return
	}

	req := &habitat.NetworkHabitatInternalNotifyOfUpdateInput{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		s.metrics.reportRouteErr(NotifyOfUpdate, http.StatusBadRequest)
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
		s.handlePearErr(w, NotifyOfUpdate, err)
		return
	}
	s.metrics.reportRouteSuccess(NotifyOfUpdate)
}
