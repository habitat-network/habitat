package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upNotifyRegistrationAudience, downNotifyRegistrationAudience)
}

// registrations is the table of syncer registrations (internal/notify).
const registrations = "registrations"

// notifyRegistrationColumns is the target column layout: `audience` identifies
// a registration and is the audience of the service-auth JWT delivered to it,
// while `endpoint` is only the address the call goes to. `audience` is set to
// the endpoint URL for rows written before subscribers were identified by
// service, so those keep being delivered to exactly as before.
var notifyRegistrationColumns = map[string]string{
	"space":      "text",
	"repo":       "text",
	"audience":   "text",
	"endpoint":   "text",
	"expires_at": "datetime",
	"created_at": "datetime",
	"updated_at": "datetime",
}

// notifyRegistrationColumnOrder fixes the order columns are declared in, since
// ranging a map would otherwise pick an arbitrary order.
var notifyRegistrationColumnOrder = []string{
	"space", "repo", "audience", "endpoint",
	"expires_at", "created_at", "updated_at",
}

func upNotifyRegistrationAudience(ctx context.Context, tx *sql.Tx) error {
	return rebuildNotifyRegistrations(ctx, tx, true)
}

func downNotifyRegistrationAudience(ctx context.Context, tx *sql.Tx) error {
	return rebuildNotifyRegistrations(ctx, tx, false)
}

// rebuildNotifyRegistrations moves the registrations table's primary key from
// `endpoint` to `audience`, backfilling `audience` from `endpoint` on the way up
// and from `service` (or `endpoint`) on the way down.
//
// GORM's AutoMigrate cannot do this: it adds a missing column but leaves an
// existing primary key alone, so a table created before this migration would
// keep keying on `endpoint` and keep rejecting a subscriber that registers the
// same service twice. The table is therefore rebuilt by hand.
//
// The table is created by GORM's AutoMigrate after migrations run, so a missing
// table (fresh database) is skipped and left for AutoMigrate to create at the
// shape the model expects.
func rebuildNotifyRegistrations(
	ctx context.Context, tx *sql.Tx, toAudience bool,
) error {
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	exists, err := tableExists(ctx, tx, registrations, postgres)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	// Going up, a table may already have an `audience` column — from an earlier
	// run of this migration, or added by a newer binary's AutoMigrate, which
	// adds the column but leaves the key on `endpoint`. Either way the key still
	// has to be rebuilt, and an audience already recorded must be kept.
	// Going down, the key always goes back to `endpoint`.
	keyColumn := "endpoint"
	audienceExpr := "endpoint"
	if toAudience {
		keyColumn = "audience"
		hasAudience, err := tableHasColumn(ctx, tx, registrations, "audience", postgres)
		if err != nil {
			return err
		}
		if hasAudience {
			// Prefer whatever is already there, falling back to the endpoint for
			// rows written before the column existed.
			audienceExpr = "COALESCE(NULLIF(audience, ''), endpoint)"
		}
	} else if hasService, err := tableHasColumn(
		ctx, tx, registrations, "service", postgres,
	); err != nil {
		return err
	} else if hasService {
		// A deployment that ran the service-era schema recorded a service
		// identifier; keep it rather than collapsing it back to the endpoint.
		audienceExpr = "COALESCE(NULLIF(service, ''), endpoint)"
	}
	return rebuildNotifyTable(ctx, tx, postgres, keyColumn, audienceExpr)
}

// rebuildNotifyTable copies registrations into a table keyed on keyColumn,
// taking the key's value from audienceExpr for each row.
func rebuildNotifyTable(
	ctx context.Context, tx *sql.Tx, postgres bool, keyColumn, audienceExpr string,
) error {
	tmp := registrations + "_rebuild"
	if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS "+tmp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, createRegistrationsSQL(tmp, postgres, keyColumn)); err != nil {
		return err
	}
	copySQL := "INSERT INTO " + tmp +
		" (space, repo, " + keyColumn + ", endpoint, expires_at, created_at, updated_at)" +
		" SELECT space, repo, " + audienceExpr +
		", endpoint, expires_at, created_at, updated_at" +
		" FROM " + registrations
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("copy notify registrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE "+registrations); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "ALTER TABLE "+tmp+" RENAME TO "+registrations)
	return err
}

// createRegistrationsSQL builds the CREATE TABLE for the registrations table
// keyed on keyColumn, with the column types GORM's AutoMigrate produces for this
// model on each dialect.
func createRegistrationsSQL(name string, postgres bool, keyColumn string) string {
	var b []byte
	b = append(b, "CREATE TABLE "+name+" ("...)
	for i, col := range notifyRegistrationColumnOrder {
		if i > 0 {
			b = append(b, ", "...)
		}
		b = append(b, col...)
		b = append(b, " "+typeForPostgres(notifyRegistrationColumns[col], postgres)...)
	}
	b = append(b, ", PRIMARY KEY (space, repo, "+keyColumn+"))"...)
	return string(b)
}

// typeForPostgres maps a SQLite column type to the Postgres equivalent, since
// the rebuild has to produce a table both dialects accept.
func typeForPostgres(sqliteType string, postgres bool) string {
	if !postgres {
		return sqliteType
	}
	if sqliteType == "datetime" {
		return "timestamptz"
	}
	return "text"
}

// tableColumns reports which columns name currently has.
func tableColumns(
	ctx context.Context, tx *sql.Tx, name string, postgres bool,
) (map[string]bool, error) {
	var query string
	if postgres {
		query = "SELECT column_name FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = " + bind(postgres, 1)
	} else {
		query = "SELECT name FROM pragma_table_info(" + bind(postgres, 1) + ")"
	}
	rows, err := tx.QueryContext(ctx, query, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]bool{}
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		cols[col] = true
	}
	return cols, rows.Err()
}

// tableHasColumn reports whether name has a column called column.
func tableHasColumn(
	ctx context.Context, tx *sql.Tx, name, column string, postgres bool,
) (bool, error) {
	cols, err := tableColumns(ctx, tx, name, postgres)
	if err != nil {
		return false, err
	}
	return cols[column], nil
}
