package instanceadmin

import "time"

// instanceAdminAccountID is the fixed primary key for the single instance admin row.
const instanceAdminAccountID = 1

// instanceAdminAccount is the single instance-wide administrator account.
// It is not an AT Protocol identity (no DID/handle) - it's a local operational account.
type instanceAdminAccount struct {
	ID           uint `gorm:"primaryKey"`
	PasswordHash string
	CreatedAt    time.Time
}

// instanceAdminSession is a server-side session for the instance admin, referenced
// by an opaque token stored in the admin's session cookie.
type instanceAdminSession struct {
	Token     string `gorm:"primaryKey"`
	ExpiresAt time.Time
	CreatedAt time.Time
}
