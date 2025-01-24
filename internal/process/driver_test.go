package process_test

import (
	"context"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/require"
)

func TestNoopDriver(t *testing.T) {
	driver := process.NewNoopDriver("my_type")
	require.Equal(t, "my_type", driver.Type())
	err := driver.StartProcess(context.Background(), &node.Process{
		ID: "my-id",
	}, nil)
	require.NoError(t, err)
	require.NoError(t, driver.StopProcess(context.Background(), "my-id"))
}
