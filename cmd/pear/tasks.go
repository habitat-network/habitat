package main

import "github.com/urfave/cli/v3"

// taskCommands returns the tasks `pear run <name>` can run: one-off backfills
// and repeatable maintenance such as reindexing or switching the search index
// generation. Tasks that need a running Pear environment use taskAction.
// Snapshot-only commands bypass it so they cannot initialize live services.
func taskCommands() []*cli.Command {
	return []*cli.Command{
		groupPreflightCommand(),
		reindexCommand(),
	}
}
