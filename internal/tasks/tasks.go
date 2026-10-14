// Package tasks is the registry of operator-run tasks behind `pear run`:
// one-off backfills and repeatable maintenance such as reindexing or
// switching the search index generation. Tasks run as Cloud Run jobs.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
)

// Name identifies a task on the command line.
type Name string

// ErrUnknownTask is returned when no task is registered under the requested name.
var ErrUnknownTask = errors.New("unknown task")

// Env holds the pear components a task can use. pear builds them once, so
// tasks don't construct their own. Add a field here when a task needs a
// component that isn't available yet.
type Env struct {
	DB     *gorm.DB
	Spaces spaces.Store
	// Search is nil where search isn't configured (no --meilisearch_url), so
	// tasks that use it must check.
	Search search.Index
}

// Task is a named unit of operator-run work.
type Task struct {
	Name Name
	// Description is a one-line summary shown by `pear run --list`.
	Description string
	// Run does the work. args are the command-line arguments after the task
	// name; a task that takes flags parses them from args with its own
	// flag.FlagSet. Tasks that can be run more than once must be idempotent
	// or safe to resume, since Cloud Run retries failed jobs.
	Run func(ctx context.Context, env Env, args []string) error
}

// Registry is a set of tasks by name.
type Registry struct {
	tasks map[Name]Task
}

// NewRegistry returns a registry of tasks. It panics on a duplicate or empty
// name, which is a programming error caught the first time pear starts.
func NewRegistry(tasks ...Task) *Registry {
	r := &Registry{tasks: make(map[Name]Task, len(tasks))}
	for _, t := range tasks {
		if t.Name == "" || t.Run == nil {
			panic("tasks: task needs a name and a Run func")
		}
		if _, dup := r.tasks[t.Name]; dup {
			panic(fmt.Sprintf("tasks: duplicate task %q", t.Name))
		}
		r.tasks[t.Name] = t
	}
	return r
}

// All returns pear's registry. To add a task, write it in its own file in this
// package and add it to the list below.
func All() *Registry {
	return NewRegistry(
	// e.g. reindexTask(), switchIndexGenerationTask()
	)
}

// List returns the registered tasks sorted by name.
func (r *Registry) List() []Task {
	out := make([]Task, 0, len(r.tasks))
	for _, t := range r.tasks {
		out = append(out, t)
	}
	slices.SortFunc(
		out,
		func(a, b Task) int { return strings.Compare(string(a.Name), string(b.Name)) },
	)
	return out
}

// Run runs the named task with args.
func (r *Registry) Run(ctx context.Context, name Name, env Env, args []string) error {
	t, ok := r.tasks[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownTask, name)
	}
	if err := t.Run(ctx, env, args); err != nil {
		return fmt.Errorf("task %q: %w", name, err)
	}
	return nil
}
