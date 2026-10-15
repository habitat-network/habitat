package main

import (
	"context"
	"errors"

	"github.com/urfave/cli/v3"

	"github.com/habitat-network/habitat/internal/search"
)

// reindexCommand rebuilds the search index with this pear's search settings
// and swaps it in. Run it after a deploy that changes the settings or the
// embedder. A run stopped part way resumes where it left off when run again.
// Never run two at once: one can delete the other's build.
func reindexCommand() *cli.Command {
	return &cli.Command{
		Name:   "reindex",
		Usage:  "Rebuild the search index with this pear's search settings and switch searches to it",
		Action: taskAction(reindex),
	}
}

func reindex(ctx context.Context, cmd *cli.Command, env taskEnv) error {
	if env.Search == nil {
		return errors.New("search is off: set --meilisearch_url")
	}
	r := search.NewReindexer(
		cmd.String(fMeilisearchURL),
		cmd.String(fMeilisearchAPIKey),
		searchIndexName,
		search.WithReindexIndexOptions(searchIndexOptions(cmd)...),
		search.WithReindexIndexerOptions(search.WithBlobs(env.Blobs)),
	)
	return r.Reindex(ctx, env.Spaces, env.Perms)
}
