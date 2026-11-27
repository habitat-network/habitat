package atgrp_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/api/comatproto"
)

func TestGroupPolicyWireType(t *testing.T) {
	// The host embeds this object in the open union in com.atproto.space.policy.
	// The union discriminant must be the bare NSID, not a reference with #main.
	// This is the public meta policy in the spec's examples/records.json.
	policy := atgrp.OrgAtgrpPolicyMain{
		Grants:     []atgrp.OrgAtgrpPolicyGrant{},
		PublicRead: true,
	}
	body, err := json.Marshal(policy)
	require.NoError(t, err)
	require.JSONEq(t, `{"$type":"org.atgrp.policy","grants":[],"publicRead":true}`, string(body))
}

func TestRequestJoinAdmittedWireShape(t *testing.T) {
	const uri = "at:space/did:plc:abcdefghijklmnopqrstuvwx/org.atgrp.members/self/" +
		"did:plc:abcdefghijklmnopqrstuvwx/org.atgrp.membership/did:plc:zyxwvutsrqponmlkjihgfedcb"
	const cid = "bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"
	body, err := json.Marshal(atgrp.OrgAtgrpRequestJoinOutput{
		Status:     "admitted",
		Membership: &comatproto.ComAtprotoRepoStrongRefMain{Uri: uri, Cid: cid},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"status": "admitted",
		"membership": {"$type": "com.atproto.repo.strongRef", "uri": "`+uri+`", "cid": "`+cid+`"}
	}`, string(body))
}
