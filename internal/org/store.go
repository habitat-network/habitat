package org

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/core"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type inviteTokenClaims struct {
	jwt.Claims
	Reusable bool `json:"r"` // Small keys
}

type Member struct {
	Org     core.Org
	DID     syntax.DID
	Role    Role
	LoginID string
}

// Store is the registry of all orgs on a pear instance.
// It routes DIDs to their org and provides cross-org membership checks.
type Store interface {
	GetOrg(ctx context.Context, orgID syntax.DID) (core.Org, error)
	GetOrgForDID(ctx context.Context, did syntax.DID) (o core.Org, isMember bool, err error)
	CreateOrg(
		ctx context.Context,
		name string,
		adminHandle string,
		adminPassword string,
		loginMethod string,
		loginID string,
		handleSubdomain string,
	) (orgId *identity.Identity, id *identity.Identity, err error)

	GetMember(ctx context.Context, did syntax.DID) (*Member, error)
	GetMemberByLoginID(ctx context.Context, loginID string) (*Member, error)

	ValidateAdminSignedToken(ctx context.Context, orgDID syntax.DID, token string) error
	IssueIdentityToken(
		ctx context.Context,
		orgDID syntax.DID,
		caller syntax.DID,
		reusable bool,
		expiresAt time.Time,
	) (string, error)
	CreateNewMemberIdentity(
		ctx context.Context,
		orgDID syntax.DID,
		token string,
		internalHandle string,
		password string,
		loginID string,
	) (*identity.Identity, error)

	// SyncFGAMemberships reconciles the membership table into the FGA store,
	// writing the member/admin tuple for every member. It is idempotent and
	// exists to backfill orgs whose members were created before memberships
	// were mirrored into FGA.
	SyncFGAMemberships(ctx context.Context) error
}

// storeImpl is the Store implementation backed by gorm and the identity directory.
type storeImpl struct {
	db               *gorm.DB
	hive             hive.Hive
	dir              identity.Directory
	pearDomain       string
	everyone         core.Org
	passwordProvider *login.PasswordLoginProvider
	fga              fgastore.Store
}

// NewStore creates a Store that manages multiple orgs on a pear instance.
func NewStore(
	db *gorm.DB,
	hve hive.Hive,
	dir identity.Directory,
	pearDomain string,
	passwordProvider *login.PasswordLoginProvider,
	fga fgastore.Store,
) (Store, error) {
	if err := db.AutoMigrate(&organization{}, &member{}, &spentToken{}); err != nil {
		return nil, err
	}
	return &storeImpl{
		db:               db,
		hive:             hve,
		dir:              dir,
		pearDomain:       pearDomain,
		everyone:         NewEveryoneOrg(),
		passwordProvider: passwordProvider,
		fga:              fga,
	}, nil
}

func (s *storeImpl) orgFromModel(org *organization) (*orgImpl, error) {
	return &orgImpl{
		orgID:           org.ID,
		db:              s.db,
		handleSubdomain: org.HandleSubdomain,
		name:            org.Name,
		method:          org.LoginMethod,
		fga:             s.fga,
	}, nil
}

// GetOrg returns the org with the given ID.
func (s *storeImpl) GetOrg(ctx context.Context, orgID syntax.DID) (core.Org, error) {
	var org organization
	if err := s.db.WithContext(ctx).Where("id = ?", orgID).First(&org).Error; err != nil {
		return nil, ErrOrgNotFound
	}
	return s.orgFromModel(&org)
}

// GetOrgForDID returns the org the given DID belongs to.
// First checks the member table. If the DID isn't in any org, checks if
// it's managed by our hive. Otherwise returns the everyone org for external DIDs.
func (s *storeImpl) GetOrgForDID(
	ctx context.Context,
	did syntax.DID,
) (core.Org, bool /* isMember */, error) {
	if o, err := s.GetOrg(ctx, did); err == nil {
		return o, false, nil
	}

	var m member
	if err := s.db.WithContext(ctx).Where("did = ?", did).First(&m).Error; err == nil {
		o, err := s.GetOrg(ctx, m.OrgID)
		if err != nil {
			return nil, false, err
		}
		return o, true, nil
	}

	id, err := s.dir.LookupDID(ctx, did)
	if err != nil {
		return nil, false, err
	}

	svc, hasHabitat := id.Services["habitat"]
	if hasHabitat && svc.URL == "https://"+s.pearDomain {
		return nil, false, ErrMemberNotFound
	}

	return s.everyone, true, nil
}

// CreateOrg creates a new org with a bootstrap admin member and returns the generated org ID and the admin identity.
func (s *storeImpl) CreateOrg(
	ctx context.Context,
	name string,
	adminHandle string,
	adminPassword string,
	method string,
	loginID string,
	handleSubdomain string,
) (*identity.Identity, *identity.Identity, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, nil, err
	}
	signingSecret := base64.StdEncoding.EncodeToString(secret)

	var orgId *identity.Identity
	var id *identity.Identity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		mintedOrgId, err := s.hive.WithTx(tx).MintOrgIdentity(ctx, handleSubdomain)
		if err != nil {
			return err
		}
		orgId = mintedOrgId
		if err := tx.Create(&organization{
			ID:              mintedOrgId.DID,
			Name:            name,
			LoginMethod:     core.LoginMethod(method),
			SigningSecret:   signingSecret,
			CreatedAt:       time.Now(),
			HandleSubdomain: handleSubdomain,
		}).Error; err != nil {
			if isDuplicateError(err) {
				return ErrOrgAlreadyExists
			}
			return err
		}
		// Mint identity for the admin
		mintedId, err := s.hive.WithTx(tx).MintIdentity(ctx, adminHandle, handleSubdomain)
		if err != nil {
			return err
		}
		id = mintedId
		// Determine the member's LoginID based on login method
		var memberLoginID string
		switch method {
		case "password":
			memberLoginID = mintedId.DID.String()

			if err := s.passwordProvider.WithTx(tx).
				AddLoginEntry(mintedId.DID, adminPassword); err != nil {
				return fmt.Errorf("failed to add login entry: %w", err)
			}
		case "atproto", "google":
			memberLoginID = loginID
		}

		if err := s.fga.Write(
			ctx,
			fgastore.MemberUserString(id.DID),
			fgastore.RelationAdmin,
			fgastore.OrgObjectKey(orgId.DID),
		); err != nil {
			return fmt.Errorf("failed to write fga: %w", err)
		}

		return tx.Create(&member{
			OrgID:     mintedOrgId.DID,
			Did:       id.DID,
			Role:      AdminRole,
			LoginID:   memberLoginID,
			CreatedAt: time.Now(),
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}

	return orgId, id, nil
}

func (s *storeImpl) GetMember(ctx context.Context, did syntax.DID) (*Member, error) {
	var m member
	if err := s.db.WithContext(ctx).
		Preload("Organization").
		Where("did = ?", did).
		First(&m).
		Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &Member{
				Org:     s.everyone,
				DID:     did,
				Role:    MemberRole,
				LoginID: did.String(),
			}, nil
		}
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	org, err := s.orgFromModel(&m.Organization)
	if err != nil {
		return nil, fmt.Errorf("failed to get org from model: %w", err)
	}
	return &Member{
		Org:     org,
		DID:     m.Did,
		Role:    m.Role,
		LoginID: m.LoginID,
	}, nil
}

func (s *storeImpl) GetMemberByLoginID(ctx context.Context, loginID string) (*Member, error) {
	var m member
	if err := s.db.WithContext(ctx).Where("login_id = ?", loginID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMemberNotFound
		}
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	org, err := s.orgFromModel(&m.Organization)
	if err != nil {
		return nil, fmt.Errorf("failed to get org from model: %w", err)
	}
	return &Member{
		Org:     org,
		DID:     m.Did,
		Role:    m.Role,
		LoginID: m.LoginID,
	}, nil
}

func (s *storeImpl) ValidateAdminSignedToken(
	ctx context.Context,
	orgDID syntax.DID,
	token string,
) error {
	var org organization
	if err := s.db.WithContext(ctx).Where("id = ?", orgDID).First(&org).Error; err != nil {
		return ErrOrgNotFound
	}

	signingSecret, err := base64.StdEncoding.DecodeString(org.SigningSecret)
	if err != nil {
		return err
	}

	parsed, err := jwt.ParseSigned(token)
	if err != nil {
		return ErrInvalidToken
	}
	var claims inviteTokenClaims
	if err := parsed.Claims(signingSecret, &claims); err != nil {
		return ErrInvalidToken
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return ErrInvalidToken
	}
	if !claims.Reusable {
		result := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&spentToken{OrgID: orgDID, JTI: claims.ID, ConsumedAt: time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrInvalidToken
		}
	}
	return nil
}

func (s *storeImpl) IssueIdentityToken(
	ctx context.Context,
	orgDID syntax.DID,
	caller syntax.DID,
	reusable bool,
	expiresAt time.Time,
) (string, error) {
	if expiresAt.After(time.Now().AddDate(0, 1, 0)) {
		return "", ErrInvalidTokenExpiry
	}

	var org organization
	if err := s.db.WithContext(ctx).Where("id = ?", orgDID).First(&org).Error; err != nil {
		return "", ErrOrgNotFound
	}

	signingSecret, err := base64.StdEncoding.DecodeString(org.SigningSecret)
	if err != nil {
		return "", err
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	jti := base64.RawURLEncoding.EncodeToString(b)

	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: signingSecret}, nil)
	if err != nil {
		return "", err
	}

	claims := inviteTokenClaims{
		Claims: jwt.Claims{
			ID:     jti,
			Issuer: caller.String(),
			Expiry: jwt.NewNumericDate(expiresAt),
		},
		Reusable: reusable,
	}
	return jwt.Signed(sig).Claims(claims).CompactSerialize()
}

func (s *storeImpl) CreateNewMemberIdentity(
	ctx context.Context,
	orgDID syntax.DID,
	token string,
	internalHandle string,
	password string,
	loginID string,
) (*identity.Identity, error) {
	var org organization
	if err := s.db.WithContext(ctx).Where("id = ?", orgDID).First(&org).Error; err != nil {
		return nil, ErrOrgNotFound
	}

	signingSecret, err := base64.StdEncoding.DecodeString(org.SigningSecret)
	if err != nil {
		return nil, err
	}

	// Validate the admin-signed token
	parsed, err := jwt.ParseSigned(token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims inviteTokenClaims
	if err := parsed.Claims(signingSecret, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return nil, ErrInvalidToken
	}
	if !claims.Reusable {
		result := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&spentToken{OrgID: orgDID, JTI: claims.ID, ConsumedAt: time.Now()})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, ErrInvalidToken
		}
	}

	var id *identity.Identity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		newID, err := s.hive.WithTx(tx).MintIdentity(ctx, internalHandle, org.HandleSubdomain)
		if err != nil {
			return fmt.Errorf("mint identity: %w", err)
		}
		id = newID

		var memberLoginID string
		switch org.LoginMethod {
		case core.LoginMethodPassword:
			memberLoginID = newID.DID.String()
			if err := s.passwordProvider.WithTx(tx).AddLoginEntry(newID.DID, password); err != nil {
				return fmt.Errorf("add login entry: %w", err)
			}
		default:
			memberLoginID = loginID
		}

		if err := s.fga.Write(
			ctx,
			fgastore.MemberUserString(id.DID),
			fgastore.RelationMember,
			fgastore.OrgObjectKey(orgDID),
		); err != nil {
			return fmt.Errorf("write fga: %w", err)
		}

		return tx.Create(&member{
			OrgID:   orgDID,
			Did:     newID.DID,
			Role:    MemberRole,
			LoginID: memberLoginID,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return id, nil
}

// SyncFGAMemberships implements [Store]. Writes are batched and duplicate
// tuples are ignored, so re-running against an already-synced store is a
// no-op.
func (s *storeImpl) SyncFGAMemberships(ctx context.Context) error {
	var members []member
	if err := s.db.WithContext(ctx).Find(&members).Error; err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	tuples := make([]*openfgav1.TupleKey, 0, len(members))
	for _, m := range members {
		relation := fgastore.RelationMember
		if m.Role == AdminRole {
			relation = fgastore.RelationAdmin
		}
		tuples = append(tuples, tuple.NewTupleKey(
			fgastore.OrgObjectKey(m.OrgID),
			relation,
			fgastore.MemberUserString(m.Did),
		))
	}

	// OpenFGA caps writes per request, so send in chunks.
	const chunkSize = 50
	for start := 0; start < len(tuples); start += chunkSize {
		chunk := tuples[start:min(start+chunkSize, len(tuples))]
		if err := s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys:   chunk,
				OnDuplicate: "ignore",
			},
		}); err != nil {
			return fmt.Errorf("write membership tuples: %w", err)
		}
	}
	return nil
}
