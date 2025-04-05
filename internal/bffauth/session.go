package bffauth

import (
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
)

type ChallengeSessionPersister interface {
	SaveSession(session *ChallengeSession) error
	GetSession(sessionID string) (*ChallengeSession, error)
	DeleteSession(sessionID string) error
}

type InMemoryChallengeSessionPersister struct {
	store          map[string]*ChallengeSession
	usedChallenges map[string]bool
}

func NewInMemorySessionPersister() *InMemoryChallengeSessionPersister {
	return &InMemoryChallengeSessionPersister{
		store:          make(map[string]*ChallengeSession),
		usedChallenges: make(map[string]bool),
	}
}

func (p *InMemoryChallengeSessionPersister) SaveSession(session *ChallengeSession) error {
	p.store[session.SessionID] = session
	p.usedChallenges[session.Challenge] = true
	return nil
}

func (p *InMemoryChallengeSessionPersister) GetSession(sessionID string) (*ChallengeSession, error) {
	session, ok := p.store[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	return session, nil
}

func (p *InMemoryChallengeSessionPersister) DeleteSession(sessionID string) error {
	delete(p.store, sessionID)
	return nil
}

// ChallengeSession represents a session for a challenge.
// This session is used to keep track of challenges that
// have not yet been used, and to prevent replay attacks.
type ChallengeSession struct {
	SessionID string
	DID       string
	PublicKey crypto.PublicKey
	Challenge string
	ExpiresAt time.Time
}
