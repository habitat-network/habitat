package inbox

import (
	"context"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Inbox interface {
	Put(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string, clique *string) error
	GetCliqueItems(ctx context.Context, did string, clique string) ([]habitat_syntax.HabitatURI, error)
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
	Clique     string `gorm:"index"` // We want to be able to fetch notifications for a given clique
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

func (i *inbox) Put(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string, clique *string) error {
	notification := &Notification{
		Sender:     sender.String(),
		Recipient:  recipient.String(),
		Collection: collection,
		Rkey:       rkey,
	}
	if clique != nil {
		notification.Clique = *clique
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

func (i *inbox) GetCliqueItems(ctx context.Context, did string, clique string) ([]habitat_syntax.HabitatURI, error) {
	items, err := gorm.G[Notification](i.db).
		Select("sender", "collection", "rkey").
		Where("recipient = ?", did).
		Where("clique = ?", clique).
		Find(ctx)

	if err != nil {
		return nil, err
	}

	// Construct the habiatat URIs from did, collection, rkey fields.
	uris := make([]habitat_syntax.HabitatURI, len(items))
	for i, item := range items {
		uris[i] = habitat_syntax.ConstructHabitatUri(item.Sender, item.Collection, item.Rkey)
	}

	return uris, nil
}
