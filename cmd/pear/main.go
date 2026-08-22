package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexedwards/argon2id"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/pearsetup"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Flags:  getFlags(),
		Action: run,
	}
	ctx := context.Background()
	if err := cmd.Run(ctx, os.Args); err != nil {
		slog.ErrorContext(ctx, "error running command", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	notifyCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup OpenTelemetry
	// This needs to happen at the beginning so components use the global logger initialized below
	// by slog.
	otelClose, err := telemetry.SetupOpenTelemetry(notifyCtx, "pear")
	defer func() { _ = otelClose(context.Background()) }()
	if err != nil {
		return fmt.Errorf("setup open telemetry for metric/trace/log collection: %w", err)
	}
	slog.InfoContext(notifyCtx, "successfully set up open telemetry")

	tracer := otel.Tracer("pear/main")
	startupCtx, startupSpan := tracer.Start(notifyCtx, "startup")

	meter := otel.Meter("habitat-meter")

	gauge, err := meter.Int64Gauge("habitat.running", metric.WithUnit("item"))
	if err != nil {
		slog.ErrorContext(startupCtx, "failed to create gauge", "err", err)
	} else {
		gauge.Record(startupCtx, 1)
		defer gauge.Record(context.Background(), 0)
	}

	slog.InfoContext(startupCtx, "running with flags", "flags", cmd.FlagNames())

	oauthSecret, err := encrypt.ParseKey(cmd.String(fOauthServerSecret))
	if err != nil {
		return fmt.Errorf("parse oauth server secret: %w", err)
	}
	credKey, err := encrypt.ParseKey(cmd.String(fPdsCredEncryptKey))
	if err != nil {
		return fmt.Errorf("load PDS encryption key: %w", err)
	}
	hostKey, err := atcrypto.ParsePrivateMultibase(cmd.String(fSpaceSigningKey))
	if err != nil {
		return fmt.Errorf("parse space-host signing key: %w", err)
	}
	passwordHash, err := setupInstanceAdminPassword(startupCtx, cmd)
	if err != nil {
		return fmt.Errorf("setup instance admin password: %w", err)
	}

	cfg := pearsetup.Config{
		Domain:             cmd.String(fDomain),
		HiveDomain:         cmd.String(fHiveDomain),
		DB:                 cmd.String(fDB),
		Port:               cmd.String(fPort),
		HTTPSCerts:         cmd.String(fHttpsCerts),
		Debug:              cmd.Bool(fDebug),
		OAuthServerSecret:  oauthSecret,
		PDSCredEncryptKey:  credKey,
		OAuthClientSecret:  cmd.String(fOauthClientSecret),
		PDSOAuthClientURI:  cmd.String(fPdsOauthClientUri),
		SpaceSigningKey:    hostKey,
		AdminPasswordHash:  passwordHash,
		GoogleClientID:     cmd.String(fGoogleClientID),
		GoogleClientSecret: cmd.String(fGoogleClientSecret),
		UIDevProxy:         cmd.String(fUiDevProxy),
		BuiltinApps:        cmd.StringSlice(fBuiltinApps),
		BlobBucket:         cmd.String(fBlobBucket),
	}

	p, err := pearsetup.New(startupCtx, cfg)
	if err != nil {
		return fmt.Errorf("setup pear: %w", err)
	}
	defer func() { _ = p.Close() }()

	startupSpan.End()
	slog.SetDefault(log.New(log.WithStdout(cmd.Bool(fDebug))))

	err = p.Run(notifyCtx)
	if !errors.Is(err, context.Canceled) {
		slog.ErrorContext(startupCtx, "server shut down returned an error", "err", err)
	}
	return err
}

func setupInstanceAdminPassword(ctx context.Context, cmd *cli.Command) (string, error) {
	pass := cmd.String(fAdminPassword)

	// Generate a password on startup if not given
	generate := pass == ""
	if generate {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		pass = string(b)
		slog.WarnContext(
			ctx,
			"generated instance admin password; save it now, it will not be shown again until next restart. password changes on restart if not added to environment variables via HABITAT_ADMIN_PASSWORD",
			"username",
			"admin",
			"password",
			pass,
		)
	}
	passwordHash, err := argon2id.CreateHash(pass, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}
	return passwordHash, nil
}
