package db

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type StoreA struct {
	db *gorm.DB
}

type testModel struct {
	Name string
}

func (s *StoreA) WithTx(tx *gorm.DB) *StoreA {
	return &StoreA{db: tx}
}

func (s *StoreA) Write() error {
	return s.db.Create(&testModel{Name: "A record"}).Error
}

func TestWithTransaction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db.AutoMigrate(&testModel{})

	a := &StoreA{db: db}

	tx := db.Begin()
	err = WithTransaction(tx, a).Write()
	require.NoError(t, err)
	tx.Rollback()

	err = db.First(&testModel{}).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	tx = db.Begin()
	err = WithTransaction(tx, a).Write()
	require.NoError(t, err)
	tx.Commit()

	err = db.First(&testModel{}).Error
	require.NoError(t, err)
}
