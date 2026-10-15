package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/urfave/cli/v3"
)

const fFresh = "fresh"

// reindexCommand builds a search generation with this pear's search settings
// and makes it current. Run it after a deploy that changes the settings or
// the embedder, or with --fresh to rebuild the index from scratch. A run
// stopped part way resumes where it left off when run again. Never run two at
// once: one can delete the other's build.
func reindexCommand() *cli.Command {
	return &cli.Command{
		Name:  "reindex",
		Usage: "Build a search index generation with this pear's search settings and switch searches to it",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  fFresh,
				Usage: "Rebuild from scratch even if the current generation already has these settings",
			},
		},
		Action: taskAction(reindex),
	}
}

func reindex(ctx context.Context, cmd *cli.Command, env taskEnv) error {
	if env.Search == nil {
		return errors.New("search is off: set --meilisearch_url")
	}
	n, err := env.Search.Build(
		ctx,
		env.Spaces,
		env.Perms,
		cmd.Bool(fFresh),
	)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "search: reindex done", "generation", n)
	return nil
}
