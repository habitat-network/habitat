# Atlas writes pear's schema migrations: it diffs the GORM models against the
# existing migrations and writes the difference as a new goose-format SQL file.
# goose applies them when pear starts; Atlas never connects to a real database.
# See migrations/README.md.
#
# The models' DDL is dumped to .atlas/<dialect>.sql by `go run ./schema` (the
# pear:schema-dump task) rather than loaded through an external_schema data
# source, which the community build of Atlas doesn't support.

# Scratch Postgres that Atlas replays the migrations on. Set
# ATLAS_POSTGRES_DEV_URL to use a local server instead of Docker, e.g.
# postgres://postgres@localhost:5432/dev?sslmode=disable&search_path=public
locals {
  postgres_dev_url = getenv("ATLAS_POSTGRES_DEV_URL") != "" ? getenv("ATLAS_POSTGRES_DEV_URL") : "docker://postgres/16/dev?search_path=public"
}

env "postgres" {
  src = "file://.atlas/postgres.sql"
  dev = local.postgres_dev_url
  migration {
    dir    = "file://../../internal/db/schema/postgres"
    format = goose
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}

env "sqlite" {
  src = "file://.atlas/sqlite.sql"
  dev = "sqlite://dev?mode=memory"
  migration {
    dir    = "file://../../internal/db/schema/sqlite"
    format = goose
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}
