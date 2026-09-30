# Pear migrations

pear applies its migrations with [goose](https://github.com/pressly/goose) at
startup (`migrations.Run`), in version order. There are two kinds:

- **Schema migrations** are SQL files in `cmd/pear/migrations/<dialect>`, one
  directory each for Postgres and SQLite. [Atlas](https://atlasgo.io) writes
  them by diffing the stores' GORM models (each store package's `Models()`,
  gathered by `migrations.Models` and printed by `cmd/pear/schema`) against the
  existing migrations. Tests get the same schema from
  `cmd/pear/testutil.NewPearDB`.
- **Go migrations** live in this package and change data rather than schema.

## Changing the schema

Edit the GORM model, then generate a migration for both dialects:

```sh
moon run pear:migration-diff -- <name>
```

Replaying the Postgres migrations needs Docker. To use a local server instead,
set `ATLAS_POSTGRES_DEV_URL` to a scratch database, e.g.
`postgres://postgres@localhost:5432/dev?sslmode=disable&search_path=public`.

If you edit a generated migration by hand, rehash it with
`moon run pear:migration-hash`. CI runs `moon run pear:migration-check`, which
fails when the migrations were edited without rehashing or don't match the
models.

### Objects Atlas can't model

Some schema objects can't be expressed on a GORM model, such as the search
index's Postgres GIN expression index (`postgres/*_search_fts.sql`). Write
those migrations by hand, rehash, and add the objects to the `exclude` list of
the matching env in `atlas.hcl` so the drift check and `migration-diff` leave
them alone.

The SQLite built into Atlas lacks FTS5, so it can't replay a migration that
creates an FTS5 table. The search index's SQLite FTS5 table and triggers are
therefore a Go migration (`*_search_fts_sqlite.go`) that `dialectGoMigrations`
returns for SQLite only.

## Go migrations

```sh
moon run pear:migration-create -- <name> go
```

Atlas ignores Go migrations, so they must not change the schema. A Go migration
that needs pear's components (for example the spaces store) gets them with
`GetPearMigrationContext(ctx, tx)` instead of constructing its own. It returns
the components pear built, already scoped to the migration's transaction.
