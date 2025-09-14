package types

import "context"

// TODO: this is really part of the controller, should be moved there, but a bunch of packages have stuff that implement it.
// Node components must be able to carry out a few basic functions
type Component[T any] interface {
	RestoreFromState(context.Context, T) error
}
