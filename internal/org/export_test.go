package org

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/fgastore"
)

// This file exposes org's unexported store internals to org's external tests.
// It is compiled only into the test binary, so none of it is part of the
// package's API; the production types stay unexported.

type (
	// StoreImpl is the concrete type NewStore returns behind the Store
	// interface, so tests can assert on it.
	StoreImpl = storeImpl
	// OrgImpl is the concrete type an org handle resolves to.
	OrgImpl = orgImpl
	// Organization is the orgs table row, so tests can seed one directly.
	Organization = organization
	// MemberRow is the members table row, so tests can assert on one.
	MemberRow = member
)

// DB returns the store's gorm handle, for tests that seed or inspect rows
// through it rather than through the store's API.
func (s *storeImpl) DB() *gorm.DB { return s.db }

// OrgDB returns the org handle's gorm handle, for the same reason.
func (o *orgImpl) OrgDB() *gorm.DB { return o.db }

// OrgID returns the org's DID, which the tests assert against.
func (o *orgImpl) OrgID() syntax.DID { return o.orgID }

// FGA returns the org's fga store, which the tests assert tuples against.
func (o *orgImpl) FGA() fgastore.Store { return o.fga }
