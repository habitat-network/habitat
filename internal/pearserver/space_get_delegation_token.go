package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
)

func (p *PearServer) GetDelegationToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, r.URL.Query().Get("space"), "space")
	if !ok {
		return
	}
	kid := "#atproto"
	privKey, err := p.hive.PrivateKeyForDID(ctx, credInfo.Subject)
	if errors.Is(err, identity.ErrDIDNotFound) {
		privKey = p.hostKey
		kid = "#habitat"
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get private key: %w", err))
		return
	}
	token, err := utils.DelegationToken(privKey, credInfo.Subject, kid, space)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("sign token: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetDelegationTokenOutput{
		Token: token,
	})
}
