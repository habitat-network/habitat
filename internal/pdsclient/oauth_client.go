package pdsclient

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
)

type ClientAppConfig struct {
	ClientID    string
	CallbackURL string
	UserAgent   string
	Scopes      []string
	PrivateKey  atcrypto.PrivateKey
	KeyID       string
	Store       oauth.ClientAuthStore
}

func NewClientApp(cfg ClientAppConfig) (*oauth.ClientApp, error) {
	config := oauth.NewPublicConfig(cfg.ClientID, cfg.CallbackURL, cfg.Scopes)
	config.UserAgent = cfg.UserAgent

	if cfg.PrivateKey != nil && cfg.KeyID != "" {
		if err := config.SetClientSecret(cfg.PrivateKey, cfg.KeyID); err != nil {
			return nil, fmt.Errorf("set client secret: %w", err)
		}
	}

	app := oauth.NewClientApp(&config, cfg.Store)
	return app, nil
}
