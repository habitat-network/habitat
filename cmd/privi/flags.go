package main

import (
	"fmt"
	"strings"

	altsrc "github.com/urfave/cli-altsrc/v3"
	yaml "github.com/urfave/cli-altsrc/v3/yaml"
	"github.com/urfave/cli/v3"
)

var (
	fDebug      = "debug"
	fDomain     = "domain"
	fDb         = "db"
	fPort       = "port"
	fHttpsCerts = "httpscerts"
	fKeyFile    = "keyfile"
)
var profiles []string

func getFlags() ([]cli.Flag, []cli.MutuallyExclusiveFlags) {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:    fDebug,
			Usage:   "Enable debug mode",
			Sources: getSources(fDebug),
		},
		&cli.StringSliceFlag{
			Name:        "profile",
			Usage:       "YAML profile files that specify flags. Can be stacked from highest precedence to lowest.",
			TakesFile:   true,
			Destination: &profiles,
		},
		&cli.StringFlag{
			Name:        fDomain,
			Usage:       "The publicly available domain at which the server can be found",
			Sources:     getSources(fDomain),
			DefaultText: "localhost",
			Required:    true,
		},
		&cli.StringFlag{
			Name:    fDb,
			Usage:   "The path to the sqlite file to use as the backing database for this server",
			Value:   "./repo.db",
			Sources: getSources(fDb),
		},
		&cli.StringFlag{
			Name:    fPort,
			Usage:   "The port on which to run the server",
			Value:   "8000",
			Sources: getSources(fPort),
		},
		&cli.StringFlag{
			Name:    fHttpsCerts,
			Usage:   "The directory in which TLS certs can be found. Should contain fullchain.pem and privkey.pem",
			Sources: getSources(fHttpsCerts),
		},
		&cli.StringFlag{
			Name:      fKeyFile,
			Usage:     "The path to the key file to use for OAuth client metadata",
			Value:     "./key.jwk",
			TakesFile: true,
			Sources:   getSources(fKeyFile),
		},
	}, []cli.MutuallyExclusiveFlags{}
}

func getSources(name string) cli.ValueSourceChain {
	return cli.NewValueSourceChain(
		cli.EnvVar("HABITAT_"+strings.ToUpper(name)),
		&profilesSource{name: name},
	)
}

type profilesSource struct {
	name string
}

// GoString implements cli.ValueSource.
func (ps *profilesSource) GoString() string {
	return fmt.Sprintf("&profilesSource{name:%[1]q}", ps.name)
}

func (ps *profilesSource) String() string {
	return strings.Join(profiles, ",")
}

func (ps *profilesSource) Lookup() (string, bool) {
	sources := cli.ValueSourceChain{
		Chain: []cli.ValueSource{},
	}
	for i := range profiles {
		sources.Chain = append(
			sources.Chain,
			yaml.YAML(ps.name, altsrc.NewStringPtrSourcer(&profiles[i])),
		)
	}
	return sources.Lookup()
}
