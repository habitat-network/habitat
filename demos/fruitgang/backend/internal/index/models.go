package index

import "time"

type Member struct {
	URI           string    `gorm:"primaryKey"`
	DID           string    `gorm:"index"`
	DisplayName   string
	AvatarCID     string
	FunFact       string
	FavoriteFruit string
	CreatedAt     time.Time
	IndexedAt     time.Time
}

type Chat struct {
	URI       string    `gorm:"primaryKey"`
	AuthorDID string    `gorm:"index"`
	Text      string
	CreatedAt time.Time
	IndexedAt time.Time
}

type ChatReply struct {
	URI       string    `gorm:"primaryKey"`
	AuthorDID string    `gorm:"index"`
	ReplyTo   string    `gorm:"index"`
	Text      string
	CreatedAt time.Time
	IndexedAt time.Time
}

type Log struct {
	URI       string    `gorm:"primaryKey"`
	AuthorDID string    `gorm:"index"`
	Fruit     string
	Count     int
	CreatedAt time.Time
	IndexedAt time.Time
}
