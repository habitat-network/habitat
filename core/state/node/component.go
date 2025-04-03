package node

import (
	"context"
)

// Node components must be able to carry out a few basic functions
type Component[T any] interface {
	RestoreFromState(context.Context, T) error
}
