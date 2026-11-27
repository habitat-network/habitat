package pearserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

const (
	defaultModerationLimit = 50
	maxModerationLimit     = 100
)

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// subjectString is how a subject appears in responses: the account DID or the
// record's at:space URI.
func subjectString(subject opensocial.Subject) string {
	if subject.URI != "" {
		return subject.URI.AtSpace().String()
	}
	return string(subject.DID)
}

func subjectViewOf(view opensocial.SubjectView) opensocial_api.OrgAtgrpModDefsSubjectView {
	return opensocial_api.OrgAtgrpModDefsSubjectView{
		Subject:     subjectString(view.Subject),
		Cid:         view.Subject.CID,
		Status:      string(view.Status),
		Tags:        view.Tags,
		ReportCount: view.ReportCount,
		CreatedAt:   formatTime(view.CreatedAt),
		UpdatedAt:   formatTime(view.UpdatedAt),
	}
}

// eventViewOf renders an event. A self view withholds who acted and the
// moderators' notes.
func eventViewOf(
	event opensocial.ModerationEvent,
	self bool,
) opensocial_api.OrgAtgrpModDefsEventView {
	var body any
	switch event.Kind {
	case opensocial.EventReport:
		var e opensocial_api.OrgAtgrpModDefsReportEvent
		_ = json.Unmarshal(event.Payload, &e)
		body = e
	case opensocial.EventAppeal:
		var e opensocial_api.OrgAtgrpModDefsAppealEvent
		_ = json.Unmarshal(event.Payload, &e)
		body = e
	case opensocial.EventStatus:
		var e opensocial_api.OrgAtgrpModDefsStatusEvent
		_ = json.Unmarshal(event.Payload, &e)
		if self {
			e.Note = ""
		}
		body = e
	case opensocial.EventTag:
		var e opensocial_api.OrgAtgrpModDefsTagEvent
		_ = json.Unmarshal(event.Payload, &e)
		body = e
	case opensocial.EventLabel:
		var e opensocial_api.OrgAtgrpModDefsLabelEvent
		_ = json.Unmarshal(event.Payload, &e)
		body = e
	default:
		body = opensocial_api.OrgAtgrpModDefsEjectEvent{}
	}
	view := opensocial_api.OrgAtgrpModDefsEventView{
		EventId:   event.ID,
		CreatedAt: formatTime(event.CreatedAt),
		Subject:   subjectString(event.Subject),
		Cid:       event.Subject.CID,
		Event:     body,
	}
	if !self {
		view.CreatedBy = string(event.CreatedBy)
	}
	return view
}

func writeSubjectError(ctx context.Context, w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, opensocial.ErrInvalidSubject):
		httpx.WriteError(ctx, w, "InvalidSubject", err.Error(), http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrSubjectNotFound):
		httpx.WriteError(ctx, w, "SubjectNotFound", err.Error(), http.StatusNotFound)
	case errors.Is(err, opensocial.ErrNotAppealable):
		httpx.WriteError(ctx, w, "NotAppealable", err.Error(), http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrNoReadAccess):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
	default:
		return false
	}
	return true
}

func moderationLimit(ctx context.Context, w http.ResponseWriter, limit int64) (int, bool) {
	switch {
	case limit == 0:
		return defaultModerationLimit, true
	case limit < 1 || limit > maxModerationLimit:
		httpx.WriteInvalidRequest(ctx, w, "limit must be 1 to 100", nil)
		return 0, false
	}
	return int(limit), true
}

// groupFor resolves a group DID input and writes GroupNotFound when this host
// does not have it.
func (p *PearServer) groupFor(
	ctx context.Context,
	w http.ResponseWriter,
	raw string,
) (syntax.DID, bool) {
	group, ok := httpx.ParseDIDInput(ctx, w, raw, "group")
	if !ok {
		return "", false
	}
	exists, ok := p.isGroup(ctx, w, group)
	if !ok {
		return "", false
	}
	if !exists {
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
		return "", false
	}
	return group, true
}

// parseSubjectInput reads a subject and cid, writing InvalidSubject on a bad one.
func parseSubjectInput(
	ctx context.Context,
	w http.ResponseWriter,
	raw, cid string,
) (opensocial.Subject, bool) {
	subject, err := opensocial.ParseSubject(raw, cid)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidSubject", err.Error(), http.StatusBadRequest)
		return opensocial.Subject{}, false
	}
	return subject, true
}

// ModSubmitReport implements org.atgrp.mod.submitReport.
func (p *PearServer) ModSubmitReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.OrgAtgrpModSubmitReportInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := p.groupFor(ctx, w, input.Group)
	if !ok {
		return
	}
	subject, ok := parseSubjectInput(ctx, w, input.Subject, input.Cid)
	if !ok {
		return
	}
	if input.ReasonType == "" {
		httpx.WriteInvalidRequest(ctx, w, "reasonType is required", nil)
		return
	}
	event, err := p.opensocialStore.SubmitReport(ctx, group, credInfo.Subject, subject,
		opensocial.Report{ReasonType: input.ReasonType, Reason: input.Reason, Rule: input.Rule})
	if writeSubjectError(ctx, w, err) {
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("submit report: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.OrgAtgrpModSubmitReportOutput{
		EventId: event.ID, CreatedAt: formatTime(event.CreatedAt),
	})
}

// ModSubmitAppeal implements org.atgrp.mod.submitAppeal.
func (p *PearServer) ModSubmitAppeal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.OrgAtgrpModSubmitAppealInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := p.groupFor(ctx, w, input.Group)
	if !ok {
		return
	}
	subject, ok := parseSubjectInput(ctx, w, input.Subject, "")
	if !ok {
		return
	}
	event, err := p.opensocialStore.SubmitAppeal(ctx, group, credInfo.Subject, subject, input.Note)
	if writeSubjectError(ctx, w, err) {
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("submit appeal: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.OrgAtgrpModSubmitAppealOutput{
		EventId: event.ID, CreatedAt: formatTime(event.CreatedAt),
	})
}

// ModUpdateSubject implements org.atgrp.mod.updateSubject.
func (p *PearServer) ModUpdateSubject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, ok := p.authenticateGroup(w, r, "org.atgrp.mod.updateSubject")
	if !ok {
		return
	}
	group := caller.Group
	var input opensocial_api.OrgAtgrpModUpdateSubjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	subject, ok := parseSubjectInput(ctx, w, input.Subject, input.Cid)
	if !ok {
		return
	}
	status := opensocial.SubjectStatus(input.Status)
	if status != "" && status != opensocial.StatusOpen && status != opensocial.StatusResolved {
		httpx.WriteInvalidRequest(ctx, w, "status must be open or resolved", nil)
		return
	}
	_, err := p.opensocialStore.UpdateSubject(
		ctx,
		group,
		caller.Actor,
		subject,
		opensocial.SubjectUpdate{
			Status: status, Note: input.Note, AddTags: input.AddTags, RemoveTags: input.RemoveTags,
		},
	)
	if writeSubjectError(ctx, w, err) {
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("update subject: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.OrgAtgrpModUpdateSubjectOutput{})
}

// ModQuerySubjects implements org.atgrp.mod.querySubjects.
func (p *PearServer) ModQuerySubjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, ok := p.authenticateGroup(w, r, "org.atgrp.mod.querySubjects")
	if !ok {
		return
	}
	group := caller.Group
	var params opensocial_api.OrgAtgrpModQuerySubjectsParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query", err)
		return
	}
	limit, ok := moderationLimit(ctx, w, params.Limit)
	if !ok {
		return
	}
	filter := opensocial.SubjectFilter{
		Status: opensocial.SubjectStatus(params.Status), Tags: params.Tags,
		Sort: opensocial.SortOrder(params.Sort),
	}
	switch filter.Status {
	case "", opensocial.StatusOpen, opensocial.StatusResolved:
	default:
		httpx.WriteInvalidRequest(ctx, w, "status must be open or resolved", nil)
		return
	}
	switch filter.Sort {
	case "":
		filter.Sort = opensocial.SortOldest
	case opensocial.SortOldest, opensocial.SortNewest, opensocial.SortUpdated:
	default:
		httpx.WriteInvalidRequest(ctx, w, "unknown sort", nil)
		return
	}
	var after *opensocial.SubjectCursor
	if params.Cursor != "" {
		cursor, err := decodeSubjectCursor(params.Cursor)
		if err != nil {
			httpx.WriteError(ctx, w, "InvalidCursor", "", http.StatusBadRequest)
			return
		}
		after = &cursor
	}
	views, err := p.opensocialStore.ListSubjects(ctx, group, filter, limit, after)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list subjects: %w", err))
		return
	}
	out := opensocial_api.OrgAtgrpModQuerySubjectsOutput{
		Subjects: make([]opensocial_api.OrgAtgrpModDefsSubjectView, len(views)),
	}
	for i, view := range views {
		out.Subjects[i] = subjectViewOf(view)
	}
	if len(views) == limit {
		out.Cursor = encodeSubjectCursor(views[len(views)-1], filter.Sort)
	}
	httpx.WriteJSON(ctx, w, out)
}

// The cursor is the sort time, the subject key and its cid, joined by spaces.
func encodeSubjectCursor(view opensocial.SubjectView, sort opensocial.SortOrder) string {
	at := view.CreatedAt
	if sort == opensocial.SortUpdated {
		at = view.UpdatedAt
	}
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(
		"%d %s %s", at.UnixNano(), view.Subject.Key(), view.Subject.CID,
	)))
}

func decodeSubjectCursor(raw string) (opensocial.SubjectCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return opensocial.SubjectCursor{}, err
	}
	parts := strings.Split(string(decoded), " ")
	if len(parts) != 3 || parts[1] == "" {
		return opensocial.SubjectCursor{}, errors.New("malformed cursor")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return opensocial.SubjectCursor{}, err
	}
	return opensocial.SubjectCursor{
		At:  time.Unix(0, nanos).UTC(),
		Key: parts[1],
		CID: parts[2],
	}, nil
}

// ModQuerySubjectHistory implements org.atgrp.mod.querySubjectHistory.
func (p *PearServer) ModQuerySubjectHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, ok := p.authenticateGroup(w, r, "org.atgrp.mod.querySubjectHistory")
	if !ok {
		return
	}
	group := caller.Group
	var params opensocial_api.OrgAtgrpModQuerySubjectHistoryParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query", err)
		return
	}
	subject, ok := parseSubjectInput(ctx, w, params.Subject, params.Cid)
	if !ok {
		return
	}
	p.writeHistory(w, r, group, opensocial.HistoryFilter{
		Subject: &subject, Sort: opensocial.SortOrder(params.Sort),
	}, params.Limit, params.Cursor, false)
}

// ModQuerySelfHistory implements org.atgrp.mod.querySelfHistory.
func (p *PearServer) ModQuerySelfHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params opensocial_api.OrgAtgrpModQuerySelfHistoryParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query", err)
		return
	}
	group, ok := p.groupFor(ctx, w, params.Group)
	if !ok {
		return
	}
	filter := opensocial.HistoryFilter{
		Author: credInfo.Subject, Sort: opensocial.SortOrder(params.Sort),
		// Reports and tags are for moderators only.
		Kinds: []opensocial.EventKind{
			opensocial.EventAppeal, opensocial.EventStatus, opensocial.EventLabel,
			opensocial.EventEject,
		},
	}
	if params.Subject != "" {
		subject, ok := parseSubjectInput(ctx, w, params.Subject, "")
		if !ok {
			return
		}
		if subject.Author() != credInfo.Subject {
			httpx.WriteError(ctx, w, "InvalidSubject", "not the caller's", http.StatusBadRequest)
			return
		}
		filter.Subject = &subject
	}
	p.writeHistory(w, r, group, filter, params.Limit, params.Cursor, true)
}

func (p *PearServer) writeHistory(
	w http.ResponseWriter,
	r *http.Request,
	group syntax.DID,
	filter opensocial.HistoryFilter,
	rawLimit int64,
	cursor string,
	self bool,
) {
	ctx := r.Context()
	limit, ok := moderationLimit(ctx, w, rawLimit)
	if !ok {
		return
	}
	switch filter.Sort {
	case "":
		filter.Sort = opensocial.SortNewest
	case opensocial.SortOldest, opensocial.SortNewest:
	default:
		httpx.WriteInvalidRequest(ctx, w, "unknown sort", nil)
		return
	}
	if cursor != "" {
		if _, err := syntax.ParseTID(cursor); err != nil {
			httpx.WriteError(ctx, w, "InvalidCursor", "", http.StatusBadRequest)
			return
		}
	}
	events, err := p.opensocialStore.GetHistory(ctx, group, filter, limit, cursor)
	if writeSubjectError(ctx, w, err) {
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get history: %w", err))
		return
	}
	out := opensocial_api.OrgAtgrpModQuerySubjectHistoryOutput{
		Events: make([]opensocial_api.OrgAtgrpModDefsEventView, len(events)),
	}
	for i, event := range events {
		out.Events[i] = eventViewOf(event, self)
	}
	if len(events) == limit {
		out.Cursor = events[len(events)-1].ID
	}
	httpx.WriteJSON(ctx, w, out)
}
