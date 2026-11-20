package spacescope_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spacescope"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	alice = syntax.DID("did:plc:alice")
	org   = syntax.DID("did:web:org.example")
)

func space(t *testing.T, owner syntax.DID, tp, skey string) habitat_syntax.SpaceURI {
	t.Helper()
	return habitat_syntax.ConstructSpaceURI(owner, syntax.NSID(tp), habitat_syntax.SpaceKey(skey))
}

func TestParse(t *testing.T) {
	_, ok, err := spacescope.Parse("atproto", alice)
	require.NoError(t, err)
	require.False(t, ok)

	g, ok, err := spacescope.Parse("space:com.example.forum", alice)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, spacescope.Grant{
		SpaceType: "com.example.forum",
		Authority: alice.String(),
		Skey:      "*",
		Actions: []spacescope.Action{
			spacescope.ActionRead, spacescope.ActionCreate,
			spacescope.ActionUpdate, spacescope.ActionDelete,
		},
	}, g)

	g, ok, err = spacescope.Parse(
		"space:*?authority=*&collection=*&manage=create&manage=update", alice)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "*", g.Authority)
	require.Equal(t, []string{"*"}, g.Collections)
	require.Equal(t, []string{"create", "update"}, g.Manage)

	g, _, err = spacescope.Parse("space:*?authority=self&action=read_self", alice)
	require.NoError(t, err)
	require.Equal(t, alice.String(), g.Authority)
	require.Equal(t, []spacescope.Action{spacescope.ActionReadSelf}, g.Actions)

	for _, bad := range []string{
		"space:not an nsid",
		"space:*?authority=nope",
		"space:*?action=write",
	} {
		_, _, err := spacescope.Parse(bad, alice)
		require.Error(t, err, bad)
	}
}

func TestReadAccess(t *testing.T) {
	forum := space(t, org, "com.example.forum", "main")
	mine := space(t, alice, "com.example.forum", "main")
	notes := space(t, org, "com.example.notes", "main")

	cases := []struct {
		name   string
		scopes []string
		space  habitat_syntax.SpaceURI
		want   spacescope.Read
	}{
		{"no space scopes", []string{"atproto", "transition:generic"}, forum, spacescope.ReadNone},
		{
			"any authority read",
			[]string{"space:*?authority=*&action=read"},
			forum,
			spacescope.ReadAll,
		},
		{
			"default authority is self",
			[]string{"space:com.example.forum"},
			forum,
			spacescope.ReadNone,
		},
		{
			"default authority covers own",
			[]string{"space:com.example.forum"},
			mine,
			spacescope.ReadAll,
		},
		{
			"named authority",
			[]string{"space:*?authority=" + org.String()},
			notes,
			spacescope.ReadAll,
		},
		{
			"type mismatch",
			[]string{"space:com.example.forum?authority=*"},
			notes,
			spacescope.ReadNone,
		},
		{"skey mismatch", []string{"space:*?authority=*&skey=other"}, forum, spacescope.ReadNone},
		{"read_self", []string{"space:*?authority=*&action=read_self"}, forum, spacescope.ReadSelf},
		{
			"writes only",
			[]string{"space:*?authority=*&collection=*&action=create"},
			forum,
			spacescope.ReadNone,
		},
		{"read wins over read_self", []string{
			"space:*?authority=*&action=read_self",
			"space:com.example.forum?authority=*&action=read",
		}, forum, spacescope.ReadAll},
		{
			"malformed grants nothing",
			[]string{"space:*?authority=*&action=bogus"},
			forum,
			spacescope.ReadNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, spacescope.FromScopes(c.scopes, alice).ReadAccess(c.space))
		})
	}
}
