package outbox

import (
	"testing"

	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

func TestStoreEmitPollAck(t *testing.T) {
	t.Parallel()
	db := db_testutil.NewDB(t)
	s, err := NewStore(db, utils.NewPollNotifier())
	require.NoError(t, err)

	uri := habitat_syntax.SpaceRecordURI(
		"at://did:plc:o/space/network.habitat.space/s1/did:plc:a/network.habitat.test/k1",
	)
	require.NoError(t, s.Emit(t.Context(), uri, []byte(`{"n":1}`)))
	require.NoError(t, s.Emit(t.Context(), uri, []byte(`{"n":2}`)))

	msgs, err := s.Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, uri, msgs[0].URI)
	require.JSONEq(t, `{"n":1}`, string(msgs[0].Value))

	// Acked messages are not redelivered; unacked ones are.
	require.NoError(t, s.Ack(t.Context(), msgs[0].ID))
	msgs, err = s.Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.JSONEq(t, `{"n":2}`, string(msgs[0].Value))
}
