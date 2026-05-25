package db

import "gorm.io/gorm"

type Store[T any] interface {
	WithTx(tx *gorm.DB) T
}
