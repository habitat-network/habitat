package hive

import (
	"gorm.io/gorm"
)

type store struct {
	db *gorm.DB
}

func newStore(db *gorm.DB) (*store, error) {
	err := db.AutoMigrate(&member{})
	if err != nil {
		return nil, err
	}
	return &store{db: db}, nil
}

// createMember generates all the necessary keys / ids for a DID identity + doc with this handle
func (s *store) createMember(handle string) {

}

// getMemberByHandle fetches the member via handle (with member namespace stripped already) from the store
func (s *store) getMemberByHandle(handle string) {

}

// getMemberByDID fetches the member via opaque ID from the store
func (s *store) getMemberByID(opaqueID string) {

}
