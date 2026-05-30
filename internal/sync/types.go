package sync

import (
	"context"
	"time"
)

type EventType string

const (
	EventSpaceRecord EventType = "space_record"
	EventSpaceMember EventType = "space_member"
	EventSpace       EventType = "space"
	EventIdentity    EventType = "identity"
)

type Event struct {
	Rev       string    `json:"rev"`
	Time      time.Time `json:"time"`
	Type      EventType `json:"type"`
	Space     string    `json:"space,omitempty"`
	SpaceType string    `json:"spaceType,omitempty"`

	// space_record
	Repo       string `json:"repo,omitempty"`
	RecordRev  string `json:"recordRev,omitempty"`
	Action     string `json:"action,omitempty"`
	Collection string `json:"collection,omitempty"`
	Rkey       string `json:"rkey,omitempty"`
	Record     []byte `json:"record,omitempty"`

	// space_member
	MemberRev string `json:"memberRev,omitempty"`
	Idx       int    `json:"idx,omitempty"`
	DID       string `json:"did,omitempty"`
	Access    string `json:"access,omitempty"`
}

type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

type NopPublisher struct{}

func (n *NopPublisher) Publish(_ context.Context, _ Event) error { return nil }
