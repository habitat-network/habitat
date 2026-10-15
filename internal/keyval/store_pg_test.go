package keyval

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/keyval/keyvaltest"
)

func TestPostgresStore(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) (Store, *gorm.DB) {
		d := keyvaltest.NewDB(t)
		s, err := New(t.Context(), d)
		require.NoError(t, err)
		return s, d
	})
}
