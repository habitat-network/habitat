// Package schema lists the GORM models pkg/sap's stores persist, so that the
// server and its tests create the same tables from one list.
package schema

import (
	"github.com/habitat-network/habitat/pkg/sap/crawl"
	"github.com/habitat-network/habitat/pkg/sap/outbox"
	"github.com/habitat-network/habitat/pkg/sap/register"
	"github.com/habitat-network/habitat/pkg/sap/session"
	"github.com/habitat-network/habitat/pkg/sap/syncer"
)

// Models returns the GORM models of every store sap persists to its database,
// which [github.com/habitat-network/habitat/internal/db.Migrate] creates tables
// for. cmd/sap calls it at startup; pkg/sap/testutil calls it for tests.
//
// It is a package of its own, rather than a function in pkg/sap, because
// pkg/sap's own tests are in-package and use pkg/sap/testutil — which needs
// this list, so it cannot come from pkg/sap without a cycle.
//
// sap's tables are prefixed `sap_` in production, which keeps them clear of the
// tables a pear server would create on the same database. Pass the handle sap
// opened, so the migration inherits that naming strategy.
func Models() []any {
	models := make([]any, 0)
	for _, set := range [][]any{
		crawl.Models(),
		outbox.Models(),
		register.Models(),
		session.Models(),
		syncer.Models(),
	} {
		models = append(models, set...)
	}
	return models
}
