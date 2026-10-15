package keyval

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/keyval/keyvaltest"
)

func TestPostgresStore(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) (Store, *gorm.DB) {
		d := keyvaltest.NewDB(t)
		require.NoError(t, db.Migrate(t.Context(), d, nil, Models()))
		s, err := New(t.Context(), d)
		require.NoError(t, err)
		return s, d
	})
}
