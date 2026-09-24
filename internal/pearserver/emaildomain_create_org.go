package pearserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/httpx"
)

// CreateEmailDomainOrg implements network.habitat.emaildomain.createOrg. It is
// unauthenticated: mapping a domain grants the caller nothing,
// since only someone completing Google OAuth for an address at the domain
// can ever sign in to the org, and the first such sign-in becomes admin.
func (p *PearServer) CreateEmailDomainOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatEmaildomainCreateOrgInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	if !parseHandle(ctx, w, input.Handle) {
		return
	}
	// Handle syntax is DNS-name syntax, which is what an email domain must be.
	domain, err := syntax.ParseHandle(strings.ToLower(input.Domain))
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, fmt.Sprintf("invalid domain: %s", input.Domain), err)
		return
	}
	// Checked up front so a taken domain doesn't leave behind an orphan org;
	// CreateDomainMapping still catches a concurrent claim.
	_, _, taken, err := p.emailDomainStore.LookupDomain(ctx, domain.String())
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("lookup domain: %w", err))
		return
	}
	if taken {
		writeDomainTaken(ctx, w)
		return
	}
	org, err := p.opensocialStore.NewOrgWithoutCreator(ctx, input.Handle)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("new org: %w", err))
		return
	}
	err = p.emailDomainStore.CreateDomainMapping(
		ctx, domain.String(), syntax.DID(org), emaildomain.LoginMethodGoogle,
	)
	if errors.Is(err, emaildomain.ErrDomainTaken) {
		writeDomainTaken(ctx, w)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create domain mapping: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatEmaildomainCreateOrgOutput{Org: org})
}

func writeDomainTaken(ctx context.Context, w http.ResponseWriter) {
	httpx.WriteError(
		ctx, w, "DomainTaken", "email domain is already mapped to an org", http.StatusConflict,
	)
}
