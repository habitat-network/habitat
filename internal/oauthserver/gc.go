package oauthserver

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// Collector periodically deletes expired OAuth sessions from the database.
type Collector struct {
	db       *gorm.DB
	interval time.Duration
}

// NewCollector creates a new Collector. interval defaults to 5 minutes.
func NewCollector(db *gorm.DB, interval time.Duration) *Collector {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Collector{db: db, interval: interval}
}

// Run starts the collection loop. Stops when ctx is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	slog.InfoContext(ctx, "starting OAuth session garbage collector",
		"interval", c.interval)
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "stopping OAuth session garbage collector")
			return ctx.Err()
		case <-ticker.C:
			c.clean(ctx)
		}
	}
}

func (c *Collector) clean(ctx context.Context) {
	var deleted int64
	result := c.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&OAuthRequest{})
	if result.Error != nil {
		slog.WarnContext(ctx, "failed to clean expired OAuth requests", "err", result.Error)
	} else {
		deleted += result.RowsAffected
	}

	result = c.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&OAuthSession{})
	if result.Error != nil {
		slog.WarnContext(ctx, "failed to clean expired OAuth sessions", "err", result.Error)
	} else {
		deleted += result.RowsAffected
	}

	if deleted > 0 {
		slog.DebugContext(ctx, "cleaned expired OAuth sessions", "count", deleted)
	}
}
