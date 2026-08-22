package testutil_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

func TestQueryAuthenticatesAsActor(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	var out habitat.NetworkHabitatOrgGetMembersOutput
	resp := p.Query(admin, "network.habitat.org.getMembers", url.Values{}, &out)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	dids := make([]string, len(out.Members))
	for i, m := range out.Members {
		dids[i] = m.Did
	}
	require.Contains(t, dids, admin.DID.String())
}

func TestQueryRejectsAnonymous(t *testing.T) {
	p := testutil.New(t)
	p.NewOrg("acme")

	resp := p.Query(p.Anonymous(), "network.habitat.org.getMembers", url.Values{}, nil)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"the real validator must reject an unauthenticated request")
}
