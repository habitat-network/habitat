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

// parseSubject reads a subject union: an accountSubject has a did, a
// recordSubject a uri and optionally a cid.
func parseSubject(raw any) (opensocial.Subject, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return opensocial.Subject{}, err
	}
	var fields struct {
		DID string `json:"did"`
		URI string `json:"uri"`
		CID string `json:"cid"`
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return opensocial.Subject{}, err
	}
	switch {
	case fields.URI != "":
		return opensocial.Subject{URI: fields.URI, CID: fields.CID}, nil
	case fields.DID != "":
		did, err := syntax.ParseDID(fields.DID)
		if err != nil {
			return opensocial.Subject{}, err
		}
		return opensocial.Subject{DID: did}, nil
	}
	return opensocial.Subject{}, errors.New("subject needs a did or a uri")
}

func subjectUnion(subject opensocial.Subject) any {
	if subject.URI != "" {
		return opensocial_api.GroupOpensocialDefsRecordSubject{Uri: subject.URI, Cid: subject.CID}
	}
	return opensocial_api.GroupOpensocialDefsAccountSubject{Did: string(subject.DID)}
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func subjectView(view opensocial.SubjectView) opensocial_api.GroupOpensocialDefsSubjectView {
	return opensocial_api.GroupOpensocialDefsSubjectView{
		Subject:     subjectUnion(view.Subject),
		Status:      string(view.Status),
		ReportCount: view.ReportCount,
		CreatedAt:   formatTime(view.CreatedAt),
		UpdatedAt:   formatTime(view.UpdatedAt),
	}
}

func eventView(event opensocial.ModerationEvent) opensocial_api.GroupOpensocialDefsModerationEvent {
	var body any
	switch event.Kind {
	case opensocial.EventReport:
		var e opensocial_api.GroupOpensocialDefsReportEvent
		_ = json.Unmarshal(event.Payload, &e)
		body = e
	case opensocial.EventStatus:
		var e opensocial_api.GroupOpensocialDefsStatusEvent
		_ = json.Unmarshal(event.Payload, &e)
		body = e
	case opensocial.EventLabel:
		var e opensocial_api.GroupOpensocialDefsLabelEvent
		_ = json.Unmarshal(event.Payload, &e)
		body = e
	default:
		body = opensocial_api.GroupOpensocialDefsEjectEvent{}
	}
	return opensocial_api.GroupOpensocialDefsModerationEvent{
		Id:        event.ID,
		CreatedAt: formatTime(event.CreatedAt),
		CreatedBy: string(event.CreatedBy),
		Event:     body,
	}
}

func writeSubjectError(ctx context.Context, w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, opensocial.ErrInvalidSubject):
		httpx.WriteError(ctx, w, "InvalidSubject", err.Error(), http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrSubjectNotFound):
		httpx.WriteError(ctx, w, "SubjectNotFound", err.Error(), http.StatusNotFound)
	default:
		return false
	}
	return true
}

// moderationTarget resolves the group a moderation query acts on and checks
// the caller holds lxm's rpc scope there.
func (p *PearServer) moderationTarget(
	w http.ResponseWriter,
	r *http.Request,
	rawGroup, lxm string,
) (group, actor syntax.DID, ok bool) {
	var named *syntax.DID
	if rawGroup != "" {
		parsed, ok := httpx.ParseDIDInput(r.Context(), w, rawGroup, "group")
		if !ok {
			return "", "", false
		}
		named = &parsed
	}
	cred, ok := p.groupCaller(w, r, named)
	if !ok {
		return "", "", false
	}
	group = cred.Subject
	if named != nil {
		group = *named
	}
	if !p.requireGroupRPC(w, r, group, cred, lxm) {
		return "", "", false
	}
	return group, cred.Subject, true
}

func pageLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 50, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		httpx.WriteInvalidRequest(r.Context(), w, "limit must be 1 to 100", err)
		return 0, false
	}
	return n, true
}

// GroupCreateReport implements group.opensocial.createReport. Any member may
// report a subject in their group.
func (p *PearServer) GroupCreateReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.GroupOpensocialCreateReportInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
	if !ok {
		return
	}
	subject, err := parseSubject(input.Subject)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid subject", err)
		return
	}
	if input.ReasonType == "" {
		httpx.WriteInvalidRequest(ctx, w, "reasonType is required", nil)
		return
	}
	if _, err := p.opensocialStore.JoinPolicy(
		ctx,
		group,
	); errors.Is(
		err,
		opensocial.ErrGroupNotFound,
	) {
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("join policy: %w", err))
		return
	}
	roles, err := p.opensocialStore.GetUserRoles(ctx, group, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get user roles: %w", err))
		return
	}
	if len(roles) == 0 {
		httpx.WriteUnauthorized(ctx, w, "only members may report", nil)
		return
	}
	event, err := p.opensocialStore.CreateReport(ctx, group, credInfo.Subject, subject,
		opensocial.Report{
			ReasonType: input.ReasonType, Reason: input.Reason, Rule: input.Rule,
		})
	if writeSubjectError(ctx, w, err) {
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create report: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.GroupOpensocialCreateReportOutput{
		Id: event.ID, CreatedAt: formatTime(event.CreatedAt),
	})
}

// GroupListSubjects implements group.opensocial.listSubjects.
func (p *PearServer) GroupListSubjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	group, _, ok := p.moderationTarget(
		w, r, r.URL.Query().Get("group"), "group.opensocial.listSubjects",
	)
	if !ok {
		return
	}
	limit, ok := pageLimit(w, r)
	if !ok {
		return
	}
	status := opensocial.SubjectStatus(r.URL.Query().Get("status"))
	switch status {
	case "":
		status = opensocial.StatusOpen
	case opensocial.StatusOpen, opensocial.StatusEscalated, opensocial.StatusResolved:
	default:
		httpx.WriteInvalidRequest(ctx, w, "unknown status", nil)
		return
	}
	var after *opensocial.SubjectView
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err := decodeSubjectCursor(raw)
		if err != nil {
			httpx.WriteError(ctx, w, "InvalidCursor", "", http.StatusBadRequest)
			return
		}
		after = &cursor
	}
	views, err := p.opensocialStore.ListSubjects(ctx, group, status, limit, after)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list subjects: %w", err))
		return
	}
	out := opensocial_api.GroupOpensocialListSubjectsOutput{
		Subjects: make([]opensocial_api.GroupOpensocialDefsSubjectView, len(views)),
	}
	for i, view := range views {
		out.Subjects[i] = subjectView(view)
	}
	if len(views) == limit {
		out.Cursor = encodeSubjectCursor(views[len(views)-1])
	}
	httpx.WriteJSON(ctx, w, out)
}

func encodeSubjectCursor(view opensocial.SubjectView) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(view.UpdatedAt.UnixNano(), 10) + " " + view.Subject.Key()),
	)
}

func decodeSubjectCursor(raw string) (opensocial.SubjectView, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return opensocial.SubjectView{}, err
	}
	nanos, key, found := strings.Cut(string(decoded), " ")
	if !found || key == "" {
		return opensocial.SubjectView{}, errors.New("malformed cursor")
	}
	n, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return opensocial.SubjectView{}, err
	}
	subject := opensocial.Subject{URI: key}
	if strings.HasPrefix(key, "did:") {
		subject = opensocial.Subject{DID: syntax.DID(key)}
	}
	return opensocial.SubjectView{UpdatedAt: time.Unix(0, n).UTC(), Subject: subject}, nil
}

// GroupGetSubjectHistory implements group.opensocial.getSubjectHistory.
func (p *PearServer) GroupGetSubjectHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	group, _, ok := p.moderationTarget(
		w, r, r.URL.Query().Get("group"), "group.opensocial.getSubjectHistory",
	)
	if !ok {
		return
	}
	limit, ok := pageLimit(w, r)
	if !ok {
		return
	}
	rawSubject := r.URL.Query().Get("subject")
	if rawSubject == "" {
		httpx.WriteInvalidRequest(ctx, w, "subject is required", nil)
		return
	}
	subject := opensocial.Subject{URI: rawSubject, CID: r.URL.Query().Get("cid")}
	if strings.HasPrefix(rawSubject, "did:") {
		subject = opensocial.Subject{DID: syntax.DID(rawSubject)}
	}
	events, err := p.opensocialStore.GetSubjectHistory(
		ctx, group, subject, limit, r.URL.Query().Get("cursor"),
	)
	if writeSubjectError(ctx, w, err) {
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get subject history: %w", err))
		return
	}
	out := opensocial_api.GroupOpensocialGetSubjectHistoryOutput{
		Events: make([]opensocial_api.GroupOpensocialDefsModerationEvent, len(events)),
	}
	for i, event := range events {
		out.Events[i] = eventView(event)
	}
	if len(events) == limit {
		out.Cursor = events[len(events)-1].ID
	}
	httpx.WriteJSON(ctx, w, out)
}

// GroupResolveSubject implements group.opensocial.resolveSubject.
func (p *PearServer) GroupResolveSubject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input struct {
		opensocial_api.GroupOpensocialResolveSubjectInput
		// Group names the group; a group's own OAuth session may omit it.
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	subject, err := parseSubject(input.Subject)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid subject", err)
		return
	}
	status := opensocial.SubjectStatus(input.Status)
	if status != opensocial.StatusResolved && status != opensocial.StatusEscalated {
		httpx.WriteInvalidRequest(ctx, w, "status must be resolved or escalated", nil)
		return
	}
	group, actor, ok := p.moderationTarget(w, r, input.Group, "group.opensocial.resolveSubject")
	if !ok {
		return
	}
	view, err := p.opensocialStore.ResolveSubject(ctx, group, actor, subject, status, input.Note)
	if writeSubjectError(ctx, w, err) {
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolve subject: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, subjectView(view))
}
