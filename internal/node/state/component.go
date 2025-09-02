package state

import (
	"context"
)

// TODO: this is really part of the controller, should be moved there.
// Node components must be able to carry out a few basic functions
type Component[T any] interface {
	RestoreFromState(context.Context, T) error
}
