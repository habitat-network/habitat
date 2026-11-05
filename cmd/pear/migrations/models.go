package migrations

import (
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/habitat-network/habitat/internal/llmgateway"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/opensocial"
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
		emaildomain.Models(),
		hive.Models(),
		instance.Models(),
		keyval.Models(),
		llmgateway.Models(),
		mcpgateway.Models(),
		notify.Models(),
		oauthserver.Models(),
		opensocial.Models(),
		spaces.Models(),
	} {
		models = append(models, set...)
	}
	return models
}
