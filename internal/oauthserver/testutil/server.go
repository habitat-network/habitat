package testutil

import (
	"testing"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
	"gorm.io/gorm"
)

type TestServer struct {
	*oauthserver.OAuthServer
	DB       *gorm.DB
	OrgStore org.Store
}

const (
	TestSecret = "secret"
)

func NewTestServer(t *testing.T, opts ...utils.Opt[TestServer]) *TestServer {
	testServer := utils.ResolveOptions(TestServer{
		DB:       db_testutil.NewDB(t),
		OrgStore: login_testutil,
	}, opts)
	testServer.OAuthServer = oauthserver.NewOAuthServer(
		[]byte(TestSecret),
		org.LoginRouter{
			Pds:      login_testutil.NewPassthroughProvider(t),
			Google:   login_testutil.NewPassthroughProvider(t),
			Password: login_testutil.NewPassthroughProvider(t),
		},
	)
	return &testServer
}
