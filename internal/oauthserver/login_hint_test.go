package oauthserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"

	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/encrypt"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
)

// fakeEmailResolver resolves emails already provisioned to a DID, reports
// other emails at acme.com as not provisioned, and provisions an email by
// assigning it newDID, recording each email it provisioned.
type fakeEmailResolver struct {
	dids        map[emaildomain.Email]syntax.DID
	newDID      syntax.DID
	provisioned []emaildomain.Email
}

func (f *fakeEmailResolver) ResolveEmailIdentity(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, error) {
	if did, ok := f.dids[email]; ok {
		return &identity.Identity{DID: did}, nil
	}
	if email.Domain() == "acme.com" {
		return nil, emaildomain.ErrEmailNotProvisioned
	}
	return nil, identity.ErrDIDNotFound
}

func (f *fakeEmailResolver) ProvisionEmailIdentity(
	ctx context.Context,
	email emaildomain.Email,
) (syntax.DID, error) {
	f.provisioned = append(f.provisioned, email)
	f.dids[email] = f.newDID
	return f.newDID, nil
}

func TestResolveLoginHintEmail(t *testing.T) {
	alice := syntax.DID("did:web:alice.example.com")
	resolver := &fakeEmailResolver{dids: map[emaildomain.Email]syntax.DID{"alice@acme.com": alice}}
	o := &OAuthServer{
		directory:     identity.NewMockDirectory(),
		emailResolver: resolver,
	}

	subject, err := o.resolveLoginHint(t.Context(), "Alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, alice.String(), subject)

	// An email at a mapped domain that has never signed in resolves to a
	// pending subject, without provisioning anything.
	subject, err = o.resolveLoginHint(t.Context(), "Bob@acme.com")
	require.NoError(t, err)
	require.Equal(t, "email:bob@acme.com", subject)
	email, ok := parsePendingEmailSubject(subject)
	require.True(t, ok)
	require.Equal(t, emaildomain.Email("bob@acme.com"), email)
	require.Empty(t, resolver.provisioned)

	_, err = o.resolveLoginHint(t.Context(), "bob@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)

	// Without a resolver, emails keep resolving to no subject, as before.
	o.emailResolver = nil
	subject, err = o.resolveLoginHint(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Empty(t, subject)
}

func TestParsePendingEmailSubject(t *testing.T) {
	for _, subject := range []string{"", "did:web:alice.example.com", "email:not-an-email"} {
		_, ok := parsePendingEmailSubject(subject)
		require.False(t, ok, subject)
	}
}

// TestNewEmailSignInE2E drives the full authorization code flow for a work
// email that has never signed in: its identity must only be provisioned once
// the Google sign-in completes with that email, and the issued token's
// subject is the newly provisioned DID. A Google sign-in with a different
// email must provision nothing.
func TestNewEmailSignInE2E(t *testing.T) {
	const bob = syntax.DID("did:web:bob.acme.example.com")
	for _, tt := range []struct {
		name         string
		googleEmail  string
		wantMintedAs syntax.DID
	}{
		{name: "verified email", googleEmail: "bob@acme.com", wantMintedAs: bob},
		{name: "different google email", googleEmail: "mallory@acme.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := dbtestutil.NewDB(t)
			emailStore, err := emaildomain.NewStore(db)
			require.NoError(t, err)
			require.NoError(t, emailStore.CreateDomainMapping(
				t.Context(), "acme.com", "did:web:acme.example.com", emaildomain.LoginMethodGoogle,
			))
			resolver := &fakeEmailResolver{dids: map[emaildomain.Email]syntax.DID{}, newDID: bob}
			google := login_testutil.NewPassthroughProvider(t)
			google.LoginID = tt.googleEmail
			oauthServer, err := NewOAuthServer(
				encrypt.TestKey,
				&org.LoginRouter{
					Google:           google,
					EmailStore:       emailStore,
					EmailProvisioner: resolver,
				},
				pdsclient.NewDummyDirectory("http://pds.url"),
				db,
				noop.Meter{},
				testStore(t),
				"https://habitat.example",
				NewJWTBearerStore(),
				testOpensocialStore(t),
				resolver,
			)
			require.NoError(t, err)

			server := httptest.NewTLSServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/oauth/authorize":
						oauthServer.HandleAuthorize(w, r)
					case "/oauth-callback":
						// Nothing is provisioned until sign-in completes.
						require.Empty(t, resolver.provisioned)
						oauthServer.HandleCallback(w, r)
					case "/oauth/token":
						oauthServer.HandleToken(w, r)
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}),
			)
			t.Cleanup(server.Close)
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			server.Client().Jar = jar
			google.RedirectURI = server.URL + "/oauth-callback"

			verifier := oauth2.GenerateVerifier()
			config := &oauth2.Config{
				Endpoint: oauth2.Endpoint{
					AuthURL:  server.URL + "/oauth/authorize",
					TokenURL: server.URL + "/oauth/token",
				},
			}
			var sub string
			clientApp := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/client-metadata.json":
						w.Header().Set("Content-Type", "application/json")
						require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
							ClientId:      "http://" + r.Host + "/client-metadata.json",
							RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
							ResponseTypes: []string{"code"},
							GrantTypes:    []string{"authorization_code", "refresh_token"},
						}))
					case "/oauth-callback":
						ctx := context.WithValue(r.Context(), oauth2.HTTPClient, server.Client())
						token, err := config.Exchange(
							ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier),
						)
						require.NoError(t, err)
						sub, _ = token.Extra("sub").(string)
						w.WriteHeader(http.StatusOK)
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}),
			)
			t.Cleanup(clientApp.Close)
			config.ClientID = clientApp.URL + "/client-metadata.json"
			config.RedirectURL = clientApp.URL + "/oauth-callback"

			authReq, err := http.NewRequest(
				http.MethodGet,
				config.AuthCodeURL(
					"test-state",
					oauth2.S256ChallengeOption(verifier),
					oauth2.SetAuthURLParam("login_hint", "bob@acme.com"),
				),
				http.NoBody,
			)
			require.NoError(t, err)
			resp, err := server.Client().Do(authReq)
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			if tt.wantMintedAs == "" {
				require.Equal(t, http.StatusInternalServerError, resp.StatusCode, "%s", body)
				require.Empty(t, resolver.provisioned)
				require.Empty(t, sub)
				return
			}
			require.Equal(t, http.StatusOK, resp.StatusCode, "authorize request failed: %s", body)
			require.Equal(t, []emaildomain.Email{"bob@acme.com"}, resolver.provisioned)
			require.Equal(t, tt.wantMintedAs.String(), sub)
		})
	}
}
