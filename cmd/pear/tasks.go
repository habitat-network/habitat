package main

import "github.com/urfave/cli/v3"

// taskCommands returns the tasks `pear run <name>` can run: one-off backfills
// and repeatable maintenance such as reindexing or switching the search index
// generation. To add one, write its command in its own file, wrap its action
// with taskAction to get a taskEnv, and add it to the list below. A task
// takes its own flags and args like any urfave subcommand.
func taskCommands() []*cli.Command {
	return []*cli.Command{
		// e.g. reindexCommand(), switchIndexGenerationCommand()
	}
}
