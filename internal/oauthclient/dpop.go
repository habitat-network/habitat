package oauthclient

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/google/uuid"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/rs/zerolog/log"
)

// DpopNonceProvider provides access to nonce management
type DpopNonceProvider interface {
	GetDpopNonce() (string, bool, error)
	SetDpopNonce(nonce string) error
}

type dpopClaims struct {
	jwt.Claims

	// the `htm` (HTTP Method) claim. See https://datatracker.ietf.org/doc/html/draft-ietf-oauth-dpop#section-4.2
	Method string `json:"htm"`

	// the `ath` (Authorization Token Hash) claim. See https://datatracker.ietf.org/doc/html/draft-ietf-oauth-dpop#section-4.2
	AccessTokenHash string `json:"ath,omitempty"`

	// the `nonce` claim. See https://datatracker.ietf.org/doc/html/draft-ietf-oauth-dpop#section-4.2
	Nonce string `json:"nonce,omitempty"`
}

type DpopHttpClient struct {
	key           *ecdsa.PrivateKey
	nonceProvider DpopNonceProvider
}

// NewDpopHttpClient creates a new DPoP HTTP client. This constructor is used
// for authentication at login.
func NewDpopHttpClient(
	key *ecdsa.PrivateKey,
	nonceProvider DpopNonceProvider,
) *DpopHttpClient {
	return &DpopHttpClient{key: key, nonceProvider: nonceProvider}
}

func (s *DpopHttpClient) Do(req *http.Request) (*http.Response, error) {
	return doInternal(req, s.nonceProvider, s.key, "")
}

type AuthedDpopHttpClient struct {
	id            *identity.Identity
	credStore     pdscred.PDSCredentialStore
	oauthClient   OAuthClient
	nonceProvider DpopNonceProvider
}

func NewAuthedDpopHttpClient(
	id *identity.Identity,
	credStore pdscred.PDSCredentialStore,
	oauthClient OAuthClient,
	nonceProvider DpopNonceProvider,
) *AuthedDpopHttpClient {
	return &AuthedDpopHttpClient{
		id:            id,
		credStore:     credStore,
		oauthClient:   oauthClient,
		nonceProvider: nonceProvider,
	}
}

func (s *AuthedDpopHttpClient) Do(req *http.Request) (*http.Response, error) {
	cred, err := s.credStore.GetCredentials(s.id.DID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user credentials: %w", err)
	}
	token, err := jwt.ParseSigned(cred.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to parse access token: %w", err)
	}
	var claims jwt.Claims
	err = token.UnsafeClaimsWithoutVerification(&claims)
	if err != nil {
		return nil, fmt.Errorf("failed to get claims: %w", err)
	}
	var accessToken string
	if claims.Expiry.Time().Before(time.Now().Add(5 * time.Minute)) {
		log.Info().Msgf("refreshing token for %s", s.id.DID)
		tokenInfo, err := s.oauthClient.RefreshToken(
			NewDpopHttpClient(cred.DpopKey, s.nonceProvider),
			s.id,
			claims.Issuer,
			cred.RefreshToken,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}
		// Update credentials
		if err := s.credStore.UpsertCredentials(s.id.DID, &pdscred.Credentials{
			RefreshToken: tokenInfo.RefreshToken,
			AccessToken:  tokenInfo.AccessToken,
			DpopKey:      cred.DpopKey,
		}); err != nil {
			return nil, fmt.Errorf("failed to update credentials: %w", err)
		}
		accessToken = tokenInfo.AccessToken
	} else {
		accessToken = cred.AccessToken
	}

	pdsUrlString, ok := s.id.Services["atproto_pds"]
	if !ok {
		return nil, fmt.Errorf("no atproto_pds service found for %s", s.id.DID)
	}
	pdsUrl, err := url.Parse(pdsUrlString.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pds url: %w", err)
	}
	req.URL = pdsUrl.ResolveReference(req.URL)
	return doInternal(
		req,
		s.nonceProvider,
		cred.DpopKey,
		accessToken,
	)
}

func doInternal(
	req *http.Request,
	nonceProvider DpopNonceProvider,
	key *ecdsa.PrivateKey,
	accessToken string,
) (*http.Response, error) {
	if err := sign(req, nonceProvider, key, accessToken); err != nil {
		return nil, err
	}
	hasBody := req.Body != nil
	// Read out the body since we'll need it twice
	bodyBytes := []byte{}
	var err error
	if hasBody {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}
	req.Header.Set("Authorization", "DPoP "+accessToken)
	if hasBody {
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	// Check if new nonce is needed, and set it if so
	if !isUseDPopNonceError(resp) {
		return resp, nil
	}
	if resp.Header.Get("DPoP-Nonce") != "" {
		err := nonceProvider.SetDpopNonce(resp.Header.Get("DPoP-Nonce"))
		if err != nil {
			return nil, err
		}
	}
	// retry with new nonce
	req2 := req.Clone(req.Context())
	req2.RequestURI = ""
	if hasBody {
		req2.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}
	if err = sign(req2, nonceProvider, key, accessToken); err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req2)
}

func sign(
	req *http.Request,
	nonceProvider DpopNonceProvider,
	key *ecdsa.PrivateKey,
	accessToken string,
) error {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		&jose.SignerOptions{
			ExtraHeaders: map[jose.HeaderKey]any{
				jose.HeaderType: "dpop+jwt",
				"jwk": &jose.JSONWebKey{
					Key:       key.Public(),
					Use:       "sig",
					Algorithm: string(jose.ES256),
				},
			},
		},
	)
	if err != nil {
		return err
	}
	claims, err := generateClaims(req, nonceProvider, accessToken)
	if err != nil {
		return err
	}
	signedJWK, err := jwt.Signed(signer).Claims(claims).CompactSerialize()
	if err != nil {
		return err
	}
	req.Header.Set("DPoP", signedJWK)
	return nil
}

func generateClaims(
	req *http.Request,
	nonceProvider DpopNonceProvider,
	accessToken string,
) (*dpopClaims, error) {
	claims := &dpopClaims{
		Claims: jwt.Claims{
			ID:       base64.StdEncoding.EncodeToString([]byte(uuid.NewString())),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		Method: req.Method,
	}
	nonce, ok, err := nonceProvider.GetDpopNonce()
	if err != nil {
		return nil, err
	}
	if ok {
		claims.Nonce = nonce
	}
	if accessToken != "" {
		claims.AccessTokenHash = hashAccessToken(accessToken)
	}
	return claims, nil
}

func hashAccessToken(accessToken string) string {
	h := sha256.New()
	h.Write([]byte(accessToken))
	hash := h.Sum(nil)

	return base64.RawURLEncoding.EncodeToString(hash)
}

func isUseDPopNonceError(resp *http.Response) bool {
	// Resource server
	if resp.StatusCode == 401 {
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		if strings.HasPrefix(wwwAuth, "DPoP") {
			return strings.Contains(wwwAuth, "error=\"use_dpop_nonce\"")
		}
	}

	// Authorization server
	if resp.StatusCode == 400 {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			var respBody map[string]any
			err := json.NewDecoder(bytes.NewReader(body)).Decode(&respBody)
			if err == nil {
				if respBody["error"] == "use_dpop_nonce" {
					return true
				}
			}
			log.Error().Err(err).Msg("error decoding response body")
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}

	// TODO 200 OK responses can also specify a nonce to use as an optimization. So far it appears the Bluesky PDS doesn't do this.

	return false
}
