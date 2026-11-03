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

// spaceMAC is the MAC a response owes its caller: the shared secret the
// caller's Atproto-Space-Want-Mac header negotiated, and the type and version
// it asked for. A nil spaceMAC means none was requested.
type spaceMAC struct {
	typ    string
	secret []byte
}

// requestMAC reads the request's Atproto-Space-Want-Mac header for a response
// about author's repo and derives its shared secret. A request authorized by a
// space credential must send one; on OAuth and service-auth requests for the
// caller's own data it is optional, and a MAC is returned only if requested.
// On a missing (when required) or malformed header, or an invalid key share,
// it answers BadSpaceMac and returns false.
func (p *PearServer) requestMAC(
	w http.ResponseWriter,
	r *http.Request,
	cred *authn.CredentialInfo,
	author syntax.DID,
	typ string,
) (*spaceMAC, bool) {
	ctx := r.Context()
	keyShare, requested, err := spacecommit.ParseWantMAC(r.Header, typ)
	if err == nil && !requested && cred.Method == authn.ValidatorMethodSpaceCredential {
		err = fmt.Errorf("%w: missing %s header", spacecommit.ErrBadSpaceMAC, spacecommit.WantMACHeader)
	}
	if err == nil && !requested {
		return nil, true
	}
	var secret []byte
	if err == nil {
		secret, err = p.kemKeys.Decapsulate(ctx, author, keyShare)
	}
	if errors.Is(err, spacecommit.ErrBadSpaceMAC) {
		httpx.WriteError(ctx, w, "BadSpaceMac", err.Error(), http.StatusBadRequest)
		return nil, false
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("space mac: %w", err))
		return nil, false
	}
	return &spaceMAC{typ: typ, secret: secret}, true
}

// setCommit sets the response's MAC over commit, if one was requested.
func (m *spaceMAC) setCommit(w http.ResponseWriter, commit spacecommit.Commit) error {
	if m == nil {
		return nil
	}
	return spacecommit.SetMAC(w.Header(), m.typ, commit.MAC(m.secret))
}

// setRecord sets the response's MAC over a record's URI and CID, if one was
// requested.
func (m *spaceMAC) setRecord(w http.ResponseWriter, uri string, c cid.Cid) error {
	if m == nil {
		return nil
	}
	return spacecommit.SetMAC(w.Header(), m.typ, spacecommit.RecordMAC(m.secret, uri, c))
}
