package process_test

import (
	"context"
	"testing"

	"github.com/eagraf/habitat-new/internal/node/state"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/require"
)

func TestNoopDriver(t *testing.T) {
	driver := process.NewNoopDriver(state.DriverTypeNoop)
	require.Equal(t, state.DriverTypeNoop, driver.Type())
	err := driver.StartProcess(context.Background(), "my-id", nil)
	require.NoError(t, err)
	err = driver.StopProcess(context.Background(), "my-id")
	require.NoError(t, err)
	ok, err := driver.IsRunning(context.Background(), "any-id")
	require.NoError(t, err)
	require.False(t, ok, "always returns false")
	procs, err := driver.ListRunningProcesses(context.Background())
	require.NoError(t, err)
	require.Len(t, procs, 0, "doesn't keep track of anything, so no running processes")
}
