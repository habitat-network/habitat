package util_test

import (
	"fmt"
	"testing"

	"github.com/habitat-network/habitat/util"
	"github.com/stretchr/testify/require"
)

type mockCloser struct{ err error }

func (m *mockCloser) Close() error { return m.err }

func TestClose(t *testing.T) {
	util.Close(&mockCloser{err: nil}, func(e error) {
		require.Fail(t, "should not be called with nil error")
	})

	util.Close(&mockCloser{err: fmt.Errorf("some error")}, func(e error) {
		require.Equal(t, "some error", e.Error())
	})
}
