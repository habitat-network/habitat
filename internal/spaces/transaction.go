package spaces

import (
	"context"
	"errors"
	"sync"

	"gorm.io/gorm"
)

// ErrUnmanagedTransaction means the caller supplied a transaction whose outer
// commit this package cannot observe.
var ErrUnmanagedTransaction = errors.New("outer transaction must use spaces.Transaction")

// ErrSavepointsDisabled means a nested spaces transaction cannot roll back
// independently of its parent.
var ErrSavepointsDisabled = errors.New("nested spaces transaction requires savepoints")

const notificationsSetting = "habitat.spaces.transaction.notifications"

type notifications struct {
	mu    sync.Mutex
	calls []func()
}

func (q *notifications) append(calls ...func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, calls...)
}

func (q *notifications) take() []func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	calls := q.calls
	q.calls = nil
	return calls
}

func pendingNotifications(db *gorm.DB) *notifications {
	q, _ := db.Statement.Settings.Load(notificationsSetting)
	pending, _ := q.(*notifications)
	return pending
}

// Transaction runs f and publishes space notifications only after its outer
// transaction commits. Bind spaces stores to the supplied tx with [Store.WithTx].
// Nested calls create a savepoint and discard their notifications if it rolls
// back. Use Transaction, not GORM's Transaction, at savepoint boundaries whose
// rollback may be caught by the caller. Passing an external transaction is an
// error because its commit cannot be observed here.
func Transaction(ctx context.Context, db *gorm.DB, f func(tx *gorm.DB) error) error {
	parent := pendingNotifications(db)
	if _, inTx := db.Statement.ConnPool.(gorm.TxCommitter); inTx {
		if parent == nil {
			return ErrUnmanagedTransaction
		}
		if db.DisableNestedTransaction {
			return ErrSavepointsDisabled
		}
	}
	q := &notifications{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clone the statement before marking it, so the nested marker cannot
		// replace its parent's queue. WithContext and WithTx retain the setting.
		tx = tx.WithContext(ctx)
		tx.Statement.Settings.Store(notificationsSetting, q)
		return f(tx)
	})
	if err != nil {
		return err
	}
	if parent != nil {
		parent.append(q.take()...)
		return nil
	}
	for _, notify := range q.take() {
		notify()
	}
	return nil
}

// notify preserves immediate delivery for ordinary store operations, which
// submit notifications after their own transaction returns. An explicit
// Transaction defers delivery until any enclosing work has also committed.
func (s *store) notify(ctx context.Context, f func(context.Context, Notifier)) {
	if s.notifier == nil {
		return
	}
	if q := pendingNotifications(s.db); q != nil {
		ctx := context.WithoutCancel(ctx)
		q.append(func() { f(ctx, s.notifier) })
		return
	}
	f(ctx, s.notifier)
}
