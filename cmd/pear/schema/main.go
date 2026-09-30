// Command schema prints the DDL for pear's GORM models in the given dialect
// ("postgres" or "sqlite"). Atlas runs it as the desired state when generating
// schema migrations; see atlas.hcl.
package main

import (
	"fmt"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: schema <postgres|sqlite>")
		os.Exit(2)
	}
	stmts, err := gormschema.New(os.Args[1]).Load(migrations.Models()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load gorm schema: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.WriteString(stmts); err != nil {
		fmt.Fprintf(os.Stderr, "write schema: %v\n", err)
		os.Exit(1)
	}
}
