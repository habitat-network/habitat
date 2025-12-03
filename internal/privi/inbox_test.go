package privi

import (
	"context"
	"testing"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestProcessNotification(t *testing.T) {
	ctx := context.Background()

	t.Run("ignores non-commit events", func(t *testing.T) {
		event := &models.Event{
			Did:  "did:plc:test123",
			Kind: models.EventKindIdentity,
		}

		err := ProcessNotification(ctx, event)
		require.NoError(t, err)
	})

	t.Run("processes commit events", func(t *testing.T) {
		event := &models.Event{
			Did:  "did:plc:test456",
			Kind: models.EventKindCommit,
		}

		err := ProcessNotification(ctx, event)
		require.NoError(t, err)
	})
}
