package node

import (
	"context"

	"github.com/eagraf/habitat-new/internal/node/hdb"
)

// Node components must be able to carry out a few basic functions
type Component[T any] interface {
	RestoreFromState(context.Context, T) error
	// Supported types
	SupportedTransitionTypes() []hdb.TransitionType
}
