package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
)

// OrgService implements network.habitat.org.requestCrawl: instead of home
// crawling one hardcoded org from startup, it lazily authenticates and
// registers an org for sap to sync the first time any caller (in practice, a
// member of that org signing in to the frontend) asks for it.
type OrgService struct {
	oauthApp *oauth.ClientApp
	sap      *sap.Sap
	store    *Store
}

func NewOrgService(oauthApp *oauth.ClientApp, s *sap.Sap, store *Store) *OrgService {
	return &OrgService{oauthApp: oauthApp, sap: s, store: store}
}

// RequestCrawl registers orgDID with sap so it starts syncing that org's
// group spaces, authenticating the org credential via the JWT Bearer grant if
// it hasn't already. Idempotent: if the org is already registered with sap,
// this is a no-op that reports "alreadyRegistered" rather than an error.
func (o *OrgService) RequestCrawl(
	ctx context.Context,
	orgDIDInput string,
) (habitat.NetworkHabitatOrgRequestCrawlOutput, error) {
	orgDID, err := syntax.ParseDID(orgDIDInput)
	if err != nil {
		return habitat.NetworkHabitatOrgRequestCrawlOutput{}, fmt.Errorf("parse org did: %w", err)
	}

	registered, err := o.sap.Sessions(ctx)
	if err != nil {
		return habitat.NetworkHabitatOrgRequestCrawlOutput{}, fmt.Errorf(
			"list registered orgs: %w",
			err,
		)
	}
	if slices.Contains(registered, orgDID) {
		return habitat.NetworkHabitatOrgRequestCrawlOutput{Status: "alreadyRegistered"}, nil
	}

	sess, err := oauthclient.NewJWTBearerClient(o.oauthApp).
		SendJWTTokenRequest(ctx, orgDID.String())
	if err != nil {
		return habitat.NetworkHabitatOrgRequestCrawlOutput{
			Status:  "authError",
			Message: err.Error(),
		}, nil
	}
	if err := o.sap.AddSession(ctx, sess.AccountDID, ""); err != nil {
		return habitat.NetworkHabitatOrgRequestCrawlOutput{}, fmt.Errorf(
			"add org session to sap: %w",
			err,
		)
	}
	// GroupService/CollectionService read the org credential used for writes
	// via store.OrgSession (the most-recently-crawled org), not sap's own
	// session store, so keep it in sync too.
	if err := o.store.SaveOrgSession(ctx, sess.AccountDID, ""); err != nil {
		return habitat.NetworkHabitatOrgRequestCrawlOutput{}, fmt.Errorf(
			"save org session: %w",
			err,
		)
	}

	return habitat.NetworkHabitatOrgRequestCrawlOutput{Status: "authenticated"}, nil
}
