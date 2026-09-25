# Pear migrations

pear applies its migrations with [goose](https://github.com/pressly/goose) at
startup (`migrations.Run`), in version order. There are two kinds:

- **Schema migrations** are SQL files in `internal/db/schema/<dialect>`, one
  directory each for Postgres and SQLite. [Atlas](https://atlasgo.io) writes
  them by diffing the stores' GORM models (each store package's `Models()`,
  gathered in `cmd/pear/schema`) against the existing migrations. Tests get the
  same schema from `internal/db/testutil.NewDB`.
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

## Go migrations

```sh
moon run pear:migration-create -- <name> go
```

Atlas ignores Go migrations, so they must not change the schema. A Go migration
that needs pear's components (for example the spaces store) should be added to
`goMigrations` in `migrations.go` with `goose.NewGoMigration`, and use the
components from `Deps` instead of constructing its own. Scope a component to the
migration's transaction with its `WithTx` method and `db.WrapTx`.
