package db

import "gorm.io/gorm"

type DBStore[T any] interface {
	WithTx(tx *gorm.DB) T
}

func WithTransaction[T any](tx *gorm.DB, store T) T {
	if dbStore, ok := any(store).(DBStore[T]); ok {
		return dbStore.WithTx(tx)
	}
	return store
}
