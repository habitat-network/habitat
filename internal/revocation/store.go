// Package revocation persists the space credentials a space authority has
// revoked (com.atproto.space.notifyCredentialRevoked), so a repo host can
// reject them until they would have expired anyway.
package revocation

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Retention is how long a revocation is kept. Credentials live at most 60
// minutes, and the spec requires hosts to retain a revocation for at least
// that long; credentials minted before the 10 minute default were valid for 1h,
// which this covers too.
const Retention = 60 * time.Minute

// revokedCredential is the GORM model for one revoked credential.
type revokedCredential struct {
	Space       habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Jti         string                  `gorm:"primaryKey"`
	RetainUntil time.Time               `gorm:"index"`
}

// Store persists revoked credentials.
type Store interface {
	// Revoke records the credentials identified by jtis as revoked for space.
	// It is idempotent: revoking again extends the retention.
	Revoke(ctx context.Context, space habitat_syntax.SpaceURI, jtis []string) error
	// IsRevoked reports whether the credential with jti was revoked for space.
	IsRevoked(ctx context.Context, space habitat_syntax.SpaceURI, jti string) (bool, error)
}

type store struct {
	db *gorm.DB
}

var _ Store = (*store)(nil)

func NewStore(db *gorm.DB) *store {
	return &store{db: db}
}

// Models returns the GORM models the store persists.
func Models() []any {
	return []any{&revokedCredential{}}
}

func (s *store) Revoke(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	jtis []string,
) error {
	db := s.db.WithContext(ctx)
	// Revocations past retention can no longer match a live credential.
	if err := db.Where("retain_until < ?", time.Now()).
		Delete(&revokedCredential{}).Error; err != nil {
		return err
	}
	retainUntil := time.Now().Add(Retention)
	rows := make([]revokedCredential, len(jtis))
	for i, jti := range jtis {
		rows[i] = revokedCredential{Space: space, Jti: jti, RetainUntil: retainUntil}
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "space"}, {Name: "jti"}},
		DoUpdates: clause.AssignmentColumns([]string{"retain_until"}),
	}).Create(&rows).Error
}

func (s *store) IsRevoked(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	jti string,
) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&revokedCredential{}).
		Where("space = ? AND jti = ? AND retain_until >= ?", space, jti, time.Now()).
		Count(&n).Error
	return n > 0, err
}
