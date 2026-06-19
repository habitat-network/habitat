package instance

import "time"

// instanceSettingsID is the fixed primary key for the single instance settings row.
const instanceSettingsID = 1

// instanceSettings holds the instance-wide settings configurable from the admin page.
type instanceSettings struct {
	ID                uint `gorm:"primaryKey"`
	InstanceName      string
	OrgCreationPolicy InvitePolicy
	SigningSecret     string
	CreatedAt         time.Time
}

// instanceInvite is a single-use token allowing one org to be created on this
// instance when orgCreationPolicy is invite_only. Token is the JWT's jti claim,
// not the JWT itself.
type instanceInvite struct {
	Token     string `gorm:"primaryKey"`
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}
