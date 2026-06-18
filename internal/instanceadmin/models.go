package instanceadmin

import "time"

// instanceSettingsID is the fixed primary key for the single instance settings row.
const instanceSettingsID = 1

// instanceSettings holds the instance-wide settings configurable from the admin page.
type instanceSettings struct {
	ID                uint `gorm:"primaryKey"`
	InstanceName      string
	OrgCreationPolicy string
	SigningSecret     string
	CreatedAt         time.Time
}
