package pdsclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bradenaw/juniper/xmaps"
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

	// the `htu` (HTTP URL) claim. See https://datatracker.ietf.org/doc/html/draft-ietf-oauth-dpop#section-4.2
	URL string `json:"htu"`

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

// This can be shared by multiple concurrent requests happening by the same user (did) caller
type authedDpopHttpClient struct {
	id            *identity.Identity
	credStore     pdscred.PDSCredentialStore
	oauthClient   PdsOAuthClient
	nonceProvider DpopNonceProvider

	mu                 *sync.Mutex
	inflightCredGetter bool
	credGetters        xmaps.Set[getter]
}

func newAuthedDpopHttpClient(
	id *identity.Identity,
	credStore pdscred.PDSCredentialStore,
	oauthClient PdsOAuthClient,
	nonceProvider DpopNonceProvider,
) *authedDpopHttpClient {
	return &authedDpopHttpClient{
		id:            id,
		credStore:     credStore,
		oauthClient:   oauthClient,
		nonceProvider: nonceProvider,
		mu:            &sync.Mutex{},
		credGetters:   make(xmaps.Set[getter]),
	}
}

type accessTokenResult struct {
	creds *pdscred.Credentials
	err   error
}

type getter struct {
	ch  chan *accessTokenResult
	ctx context.Context
}

func (s *authedDpopHttpClient) getAccessTokenToUse(ctx context.Context) (*pdscred.Credentials, error) {
	s.mu.Lock()

	// Another caller is getting the access token and may race -- coalesce with them.
	if s.inflightCredGetter {
		ch := make(chan *accessTokenResult)
		s.credGetters.Add(getter{
			ch:  ch,
			ctx: ctx,
		})
		// Don't hold the lock while waiting.
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			// If the request is cancelled, return.
			// The cred fetcher will see ctx.Done() and ignore this getter.
			return nil, ctx.Err()
		case res := <-ch:
			return res.creds, res.err
		}
	}

	// First caller: set inflight and be responsible for fetching the token and returning to all coalescers.
	s.inflightCredGetter = true

	// Don't hold the lock while fetching access token; allow other getters to join
	s.mu.Unlock()

	getOrRefreshToken := func() (*pdscred.Credentials, error) {
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

		if claims.Expiry.Time().Before(time.Now().Add(5 * time.Minute)) {
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
			cred = &pdscred.Credentials{
				RefreshToken: tokenInfo.RefreshToken,
				AccessToken:  tokenInfo.AccessToken,
				DpopKey:      cred.DpopKey,
			}
			if err := s.credStore.UpsertCredentials(s.id.DID, cred); err != nil {
				return nil, fmt.Errorf("failed to update credentials: %w", err)
			}
		}
		return cred, nil
	}

	// The cred is up to date now -- let any coalescers know and return
	cred, err := getOrRefreshToken()
	res := &accessTokenResult{
		creds: cred,
		err:   err,
	}

	s.mu.Lock()
	// Reset the inflight state
	s.inflightCredGetter = false
	getters := maps.Clone(s.credGetters)
	s.credGetters = make(xmaps.Set[getter])
	s.mu.Unlock()

	// Send to concurrent callers outside of lock
	for g := range getters {
		select {
		case <-g.ctx.Done():
			// Ignore
			continue
		case g.ch <- res:
		}
	}
	return cred, err
}

func (s *authedDpopHttpClient) Do(req *http.Request) (*http.Response, error) {
	cred, err := s.getAccessTokenToUse(req.Context())
	if err != nil {
		return nil, err
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
		cred.AccessToken,
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
	htu := req.URL.String()
	claims := &dpopClaims{
		Claims: jwt.Claims{
			ID:       base64.StdEncoding.EncodeToString([]byte(uuid.NewString())),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		Method: req.Method,
		URL:    htu,
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
