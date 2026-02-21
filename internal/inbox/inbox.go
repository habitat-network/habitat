package inbox

import (
	"context"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Inbox interface {
	Put(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection syntax.NSID, rkey string) error
	GetCollectionUpdatesByRecipient(ctx context.Context, recipient syntax.DID, collection syntax.NSID) ([]Notification, error)
	// Eventually we might have GetRecordUpdates() or GetBySender()
}

// Notification is a Gorm model for notifications.
// Notifications are unique by sender + receiver + collection + rkey
// Notifications generally live forever, there's no delete actions
type Notification struct {
	Sender     string `gorm:"primaryKey"`
	Recipient  string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type inbox struct {
	db *gorm.DB
}

// inbox implements Inbox
var _ Inbox = &inbox{}

func New(db *gorm.DB) (Inbox, error) {
	err := db.AutoMigrate(&Notification{})
	if err != nil {
		return nil, err
	}
	return &inbox{db}, nil
}

func (i *inbox) Put(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection syntax.NSID, rkey string) error {
	notification := &Notification{
		Sender:     sender.String(),
		Recipient:  recipient.String(),
		Collection: collection.String(),
		Rkey:       rkey,
	}
	notification.UpdatedAt = time.Now()

	return gorm.G[Notification](
		i.db,
		clause.OnConflict{
			Columns: []clause.Column{
				{Name: "sender"},
				{Name: "recipient"},
				{Name: "collection"},
				{Name: "rkey"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		},
	).Create(ctx, notification)
}

// GetCollectionUpdatesByRecipient implements Inbox.
func (i *inbox) GetCollectionUpdatesByRecipient(ctx context.Context, recipient syntax.DID, collection syntax.NSID) ([]Notification, error) {
	var notifs []Notification

	err := i.db.Where("recipient = ?", recipient).Where("collection = ?", collection).Find(&notifs).Error
	if err != nil {
		return nil, err
	}
	return notifs, nil
}
