package spaces

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// SpaceURI identifies a space.
// Format: "habitat://spaceDID/spaceType/skey"
// See https://dholms.leaflet.pub/3mlegohgtps2k
type SpaceURI string

var spaceURIRegex = regexp.MustCompile(
	`^habitat:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey string) SpaceURI {
	return SpaceURI(fmt.Sprintf("habitat://%s/%s/%s", spaceDID, spaceType, skey))
}

func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("SpaceURI is too long (8192 chars max)")
	}
	parts := spaceURIRegex.FindStringSubmatch(raw)
	if len(parts) < 4 || parts[0] == "" {
		return "", errors.New("invalid space URI format")
	}
	_, err := syntax.ParseDID(parts[1])
	if err != nil {
		return "", fmt.Errorf("space URI DID is not valid: %s", parts[1])
	}
	_, err = syntax.ParseNSID(parts[2])
	if err != nil {
		return "", fmt.Errorf("space URI type is not a valid NSID: %s", parts[2])
	}
	return SpaceURI(raw), nil
}

func (s SpaceURI) SpaceDID() syntax.DID {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	did, err := syntax.ParseDID(parts[1])
	if err != nil {
		return ""
	}
	return did
}

func (s SpaceURI) SpaceType() syntax.NSID {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	nsid, err := syntax.ParseNSID(parts[2])
	if err != nil {
		return ""
	}
	return nsid
}

func (s SpaceURI) Skey() string {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

func (s SpaceURI) String() string {
	return string(s)
}

// GORM models

type space struct {
	Owner     string `gorm:"primaryKey"`
	Skey      string `gorm:"primaryKey"`
	Type      string `gorm:"index"`
	CreatedAt time.Time
}

// TODO: members table will be added when the permission store is built.
// For now, the owner is always the sole member of a space.

type spaceRecord struct {
	Owner      string `gorm:"primaryKey"`
	Skey       string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Value      []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SpaceView is the public representation of a space
type SpaceView struct {
	URI         SpaceURI
	Type        syntax.NSID
	Skey        string
	MemberCount int
}

// MemberInfo holds a member's DID and when they were added
type MemberInfo struct {
	Did     syntax.DID
	AddedAt time.Time
}

// RecordInSpace is a single record within a space
type RecordInSpace struct {
	Collection syntax.NSID
	Rkey       string
	Value      map[string]any
	UpdatedAt  time.Time
}

// Store defines the persistence interface for spaces
type Store interface {
	// Space operations
	CreateSpace(
		ctx context.Context,
		owner syntax.DID,
		spaceType syntax.NSID,
		skey string,
	) (SpaceURI, error)
	ListSpaces(ctx context.Context, actor syntax.DID, filterType *syntax.NSID, filterOwner *syntax.DID) ([]SpaceView, error)

	// Member operations
	// TODO: AddMember and RemoveMember will be added when the permission store is built.
	GetMembers(ctx context.Context, space SpaceURI) ([]MemberInfo, error)
	IsMember(ctx context.Context, space SpaceURI, did syntax.DID) (bool, error)

	// Record operations
	PutRecord(ctx context.Context, space SpaceURI, collection syntax.NSID, rkey string, value map[string]any) error
	GetRecord(ctx context.Context, space SpaceURI, collection syntax.NSID, rkey string) (*RecordInSpace, error)
	ListRecords(ctx context.Context, space SpaceURI, collection *syntax.NSID) ([]RecordInSpace, error)
	DeleteRecord(ctx context.Context, space SpaceURI, collection syntax.NSID, rkey string) error
}

var (
	ErrSpaceNotFound      = errors.New("space not found")
	ErrSpaceAlreadyExists = errors.New("space already exists")
	ErrRecordNotFound     = errors.New("record not found")
)
