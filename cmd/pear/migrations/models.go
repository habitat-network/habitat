package migrations

import (
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/spaces"
)

// Models returns the GORM models of every store pear persists to its database,
// which [github.com/habitat-network/habitat/internal/db.Migrate] creates tables
// for. pear passes it at startup; its tests pass it through
// cmd/pear/testutil.NewPearDB.
//
// It lives beside [FS] so that the server and the tests migrate the same set
// from one list, rather than each keeping their own.
func Models() []any {
	models := make([]any, 0)
	for _, set := range [][]any{
		clique.Models(),
		emaildomain.Models(),
		hive.Models(),
		instance.Models(),
		login.Models(),
		notify.Models(),
		oauthserver.Models(),
		opensocial.Models(),
		org.Models(),
		pdscred.Models(),
		permissions.Models(),
		repo.Models(),
		spaces.Models(),
	} {
		models = append(models, set...)
	}
	return models
}
