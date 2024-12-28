package process_test

import (
	"testing"

	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/require"
)

func TestNoopDriver(t *testing.T) {
	driver := process.NewNoopDriver("my_type")
	require.Equal(t, "my_type", driver.Type())
	proc, err := driver.StartProcess(nil, nil)
	require.Equal(t, "", proc)
	require.NoError(t, err)
	require.NoError(t, driver.StopProcess(proc))
}
