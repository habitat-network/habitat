package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spacecommit"
)

// spaceMAC is the MAC a response owes its caller: the request its
// Accept-Signature header made and the shared secret derived from its key
// share. A nil spaceMAC means none was requested.
type spaceMAC struct {
	req    spacecommit.MACRequest
	secret []byte
}

// requestMAC reads the request's Accept-Signature header for the MAC of
// profile, for a response about author's repo, and derives its shared secret.
// A request authorized by a space credential must ask for one; on OAuth and
// service-auth requests for the caller's own data it is optional, and a MAC is
// returned only if requested. On a missing (when required) or malformed
// request, or an invalid key share, it answers InvalidSpaceMac and returns
// false. Either way the response varies on Accept-Signature.
func (p *PearServer) requestMAC(
	w http.ResponseWriter,
	r *http.Request,
	cred *authn.CredentialInfo,
	author syntax.DID,
	profile string,
) (*spaceMAC, bool) {
	ctx := r.Context()
	w.Header().Add("Vary", spacecommit.AcceptSignatureHeader)
	req, err := spacecommit.ParseMACRequest(r.Header, profile)
	if err == nil && req == nil && cred.Method == authn.ValidatorMethodSpaceCredential {
		err = fmt.Errorf(
			"%w: a MAC must be requested with tag %q",
			spacecommit.ErrInvalidSpaceMAC,
			profile,
		)
	}
	if err == nil && req == nil {
		return nil, true
	}
	var secret []byte
	if err == nil {
		secret, err = p.kemKeys.Decapsulate(ctx, author, req.KeyShare)
	}
	if errors.Is(err, spacecommit.ErrInvalidSpaceMAC) {
		httpx.WriteError(ctx, w, "InvalidSpaceMac", err.Error(), http.StatusBadRequest)
		return nil, false
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("space mac: %w", err))
		return nil, false
	}
	return &spaceMAC{req: *req, secret: secret}, true
}

// setCommit sets the response's MAC over commit, if one was requested.
func (m *spaceMAC) setCommit(w http.ResponseWriter, commit spacecommit.Commit) error {
	if m == nil {
		return nil
	}
	return spacecommit.SetCommitMAC(w.Header(), commit, m.req, m.secret)
}

// setRecord sets the response's MAC over a record's URI and CID, if one was
// requested.
func (m *spaceMAC) setRecord(w http.ResponseWriter, uri string, c cid.Cid) error {
	if m == nil {
		return nil
	}
	return spacecommit.SetRecordMAC(w.Header(), uri, c, m.req, m.secret)
}
