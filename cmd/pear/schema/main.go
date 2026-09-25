// Command schema prints the DDL for pear's GORM models in the given dialect
// ("postgres" or "sqlite"). Atlas runs it as the desired state when generating
// schema migrations; see atlas.hcl.
package main

import (
	"fmt"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"

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

// models are the GORM models of every store pear persists to its database.
func models() []any {
	var all []any
	for _, m := range [][]any{
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
		all = append(all, m...)
	}
	return all
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: schema <postgres|sqlite>")
		os.Exit(2)
	}
	stmts, err := gormschema.New(os.Args[1]).Load(models()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load gorm schema: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.WriteString(stmts); err != nil {
		fmt.Fprintf(os.Stderr, "write schema: %v\n", err)
		os.Exit(1)
	}
}
