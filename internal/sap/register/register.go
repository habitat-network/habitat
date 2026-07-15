// Package register keeps sap subscribed to each tracked space's push
// notifications: it calls network.habitat.space.registerNotify for every space
// a session can access and renews each registration before the host-side
// expiry lapses.
package register

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	// sweepInterval is how often the registrar looks for spaces needing a
	// (re-)registration.
	sweepInterval = 1 * time.Hour
	// renewBefore is how much of a registration's lifetime may remain before
	// it is renewed; it must exceed sweepInterval so a registration is renewed
	// before it can lapse between sweeps.
	renewBefore = 6 * time.Hour
)

// registration records one space's registerNotify subscription and when the
// host says it expires.
type registration struct {
	Space     habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Endpoint  string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

func (registration) TableName() string { return "sap_registrations" }

// AutoMigrate creates the registration tables.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&registration{})
}

// Clients supplies an HTTP client for a space. Satisfied by session.Store.
type Clients interface {
	ClientForSpace(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
	) (*http.Client, error)
}

// Spaces lists every space any session can access. Satisfied by session.Store.
type Spaces interface {
	Spaces(ctx context.Context) ([]habitat_syntax.SpaceURI, error)
}

// Registrar keeps registrations fresh: spaces are registered inline as the
// crawler discovers them (via Register), and the background sweep renews
// registrations before they expire and retries any the crawl-time attempt
// missed.
type Registrar struct {
	db       *gorm.DB
	clients  Clients
	spaces   Spaces
	endpoint string // sap's public base URL, registered as the notify endpoint
}

func New(db *gorm.DB, clients Clients, spaces Spaces, endpoint string) *Registrar {
	return &Registrar{db: db, clients: clients, spaces: spaces, endpoint: endpoint}
}

// WithTx returns a Registrar scoped to the given transaction.
func (r *Registrar) WithTx(tx *gorm.DB) *Registrar {
	c := *r
	c.db = tx
	return &c
}

// Run registers due spaces on start and then on every sweep interval until
// ctx ends.
func (r *Registrar) Run(ctx context.Context) {
	r.sweep(ctx)
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

func (r *Registrar) sweep(ctx context.Context) {
	due, err := r.dueSpaces(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "registrar: find due spaces", "err", err)
		return
	}
	for _, space := range due {
		if err := r.Register(ctx, space); err != nil {
			slog.ErrorContext(ctx, "registrar: register space", "space", space, "err", err)
		}
	}
}

// dueSpaces returns tracked spaces with no current registration or whose
// registration expires within the renewal window.
func (r *Registrar) dueSpaces(ctx context.Context) ([]habitat_syntax.SpaceURI, error) {
	spaces, err := r.spaces.Spaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list spaces: %w", err)
	}
	if len(spaces) == 0 {
		return nil, nil
	}

	var fresh []registration
	cutoff := time.Now().Add(renewBefore)
	if err := r.db.WithContext(ctx).
		Where("space IN ? AND expires_at >= ?", spaces, cutoff).
		Find(&fresh).Error; err != nil {
		return nil, fmt.Errorf("load registrations: %w", err)
	}
	freshSet := make(map[habitat_syntax.SpaceURI]struct{}, len(fresh))
	for _, reg := range fresh {
		freshSet[reg.Space] = struct{}{}
	}

	var due []habitat_syntax.SpaceURI
	for _, space := range spaces {
		if _, ok := freshSet[space]; !ok {
			due = append(due, space)
		}
	}
	return due, nil
}

// EnsureRegistered registers a space the registrar is not tracking yet. The
// crawler calls this for every space it lists, so each space is registered
// exactly once at discovery; renewing existing registrations is the sweep's
// job.
func (r *Registrar) EnsureRegistered(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) error {
	var reg registration
	err := r.db.WithContext(ctx).
		Where("space = ?", space).
		First(&reg).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.Register(ctx, space)
}

// Register calls registerNotify for the space as some session with access and
// records the returned expiry.
func (r *Registrar) Register(ctx context.Context, space habitat_syntax.SpaceURI) error {
	client, err := r.clients.ClientForSpace(ctx, space)
	if err != nil {
		return fmt.Errorf("client for space: %w", err)
	}

	body, err := json.Marshal(habitat.NetworkHabitatSpaceRegisterNotifyInput{
		Space:    space.String(),
		Endpoint: r.endpoint,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"/xrpc/network.habitat.space.registerNotify", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registerNotify: %s", resp.Status)
	}

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decode registerNotify output: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339, out.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse expiresAt %q: %w", out.ExpiresAt, err)
	}

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "space"}},
			DoUpdates: clause.AssignmentColumns([]string{"endpoint", "expires_at", "updated_at"}),
		}).
		Create(&registration{
			Space:     space,
			Endpoint:  r.endpoint,
			ExpiresAt: expiresAt,
		}).Error; err != nil {
		return fmt.Errorf("save registration: %w", err)
	}
	slog.InfoContext(ctx, "registered notify", "space", space, "expires_at", expiresAt)
	return nil
}

// DropSpace forgets the registration for a deleted space.
func (r *Registrar) DropSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return r.db.WithContext(ctx).
		Where("space = ?", space).
		Delete(&registration{}).Error
}
