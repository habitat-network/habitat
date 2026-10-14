package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry(t *testing.T) {
	var gotArgs []string
	boom := errors.New("boom")
	r := NewRegistry(
		Task{Name: "b", Run: func(_ context.Context, _ Env, args []string) error {
			gotArgs = args
			return nil
		}},
		Task{Name: "a", Run: func(context.Context, Env, []string) error { return boom }},
	)

	require.Equal(t, Name("a"), r.List()[0].Name)
	require.Equal(t, Name("b"), r.List()[1].Name)

	require.NoError(t, r.Run(t.Context(), "b", Env{}, []string{"--x", "1"}))
	require.Equal(t, []string{"--x", "1"}, gotArgs)

	require.ErrorIs(t, r.Run(t.Context(), "a", Env{}, nil), boom)
	require.ErrorIs(t, r.Run(t.Context(), "missing", Env{}, nil), ErrUnknownTask)
}

func TestRegistryDuplicatePanics(t *testing.T) {
	task := Task{Name: "a", Run: func(context.Context, Env, []string) error { return nil }}
	require.Panics(t, func() { NewRegistry(task, task) })
	require.NotPanics(t, func() { All() })
}
