package sap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	// registrarInterval is how often the registrar sweeps for spaces needing a
	// (re-)registration.
	registrarInterval = 1 * time.Hour
	// registrarRenewBefore is how much of a registration's lifetime may remain
	// before it is renewed; it must exceed registrarInterval so a registration
	// is renewed before it can lapse between sweeps.
	registrarRenewBefore = 6 * time.Hour
)

// registrar keeps sap subscribed to each tracked space's notifyWrite /
// notifySpaceDeleted events by calling network.habitat.space.registerNotify and
// renewing before the registration expires. It authenticates as a managed
// member of the space via OAuth.
type registrar struct {
	db          *gorm.DB
	oauthClient *oauthclient.App
	endpoint    string // sap's public base URL, registered as the notify endpoint
	interval    time.Duration
	renewBefore time.Duration
}

func newRegistrar(db *gorm.DB, oauthClient *oauthclient.App, endpoint string) *registrar {
	return &registrar{
		db:          db,
		oauthClient: oauthClient,
		endpoint:    endpoint,
		interval:    registrarInterval,
		renewBefore: registrarRenewBefore,
	}
}

// run registers due spaces on start and then on every interval until ctx ends.
func (r *registrar) run(ctx context.Context) {
	r.registerDue(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.registerDue(ctx)
		}
	}
}

// registerDue (re-)registers every tracked space whose registration is missing
// or close to expiring.
func (r *registrar) registerDue(ctx context.Context) {
	spaces, err := r.dueSpaces(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "registrar: find due spaces", "err", err)
		return
	}
	for _, space := range spaces {
		if err := r.register(ctx, space); err != nil {
			slog.ErrorContext(ctx, "registrar: register space", "space", space, "err", err)
		}
	}
}

// dueSpaces returns tracked spaces that have no current registration or whose
// registration expires within the renewal window.
func (r *registrar) dueSpaces(ctx context.Context) ([]habitat_syntax.SpaceURI, error) {
	cutoff := time.Now().Add(r.renewBefore)
	var spaces []habitat_syntax.SpaceURI
	if err := r.db.WithContext(ctx).
		Model(&managedSpace{}).
		Distinct("managed_spaces.space").
		Joins("LEFT JOIN notify_registrations ON notify_registrations.space = managed_spaces.space").
		Where("notify_registrations.space IS NULL OR notify_registrations.expires_at < ?", cutoff).
		Pluck("managed_spaces.space", &spaces).Error; err != nil {
		return nil, fmt.Errorf("query due spaces: %w", err)
	}
	return spaces, nil
}

// register calls registerNotify for the space as a managed member and records
// the returned expiry.
func (r *registrar) register(ctx context.Context, space habitat_syntax.SpaceURI) error {
	client, err := clientForSpace(ctx, r.db, r.oauthClient, space)
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
		Create(&notifyRegistration{
			Space:     space,
			Endpoint:  r.endpoint,
			ExpiresAt: expiresAt,
		}).Error; err != nil {
		return fmt.Errorf("save registration: %w", err)
	}
	slog.InfoContext(ctx, "registered notify", "space", space, "expires_at", expiresAt)
	return nil
}
