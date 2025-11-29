package oauthclient

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/google/uuid"
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

// DpopOptions provide optional configuration for a DPoP HTTP client.
type DpopOptions struct {
	// Custom HTU claim in the DPoP token. If not provided, this will defualt to the
	// request URL.
	HTU string

	// Access token to be used for PDS requests. If not provided, the access token
	// hash will not be included in the DPoP token.
	AccessToken string
}

type DpopOption func(*DpopOptions)

func WithHTU(htu string) DpopOption {
	return func(opts *DpopOptions) {
		opts.HTU = htu
	}
}

func WithAccessToken(accessToken string) DpopOption {
	return func(opts *DpopOptions) {
		opts.AccessToken = accessToken
	}
}

type DpopHttpClient struct {
	key           *ecdsa.PrivateKey
	nonceProvider DpopNonceProvider
	opts          *DpopOptions
}

// NewDpopHttpClient creates a new DPoP HTTP client. This constructor is used
// for authentication at login.
func NewDpopHttpClient(
	key *ecdsa.PrivateKey,
	nonceProvider DpopNonceProvider,
	options ...DpopOption,
) *DpopHttpClient {
	opts := &DpopOptions{}
	for _, option := range options {
		option(opts)
	}
	return &DpopHttpClient{key: key, nonceProvider: nonceProvider, opts: opts}
}

func (s *DpopHttpClient) Do(req *http.Request) (*http.Response, error) {
	err := s.sign(req)
	if err != nil {
		return nil, err
	}
	// Read out the body since we'll need it twice
	bodyBytes := []byte{}
	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}

	req.Header.Set("Authorization", "DPoP "+s.opts.AccessToken)

	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	// Check if new nonce is needed, and set it if so
	if !isUseDPopNonceError(resp) {
		return resp, nil
	}
	if resp.Header.Get("DPoP-Nonce") != "" {
		err := s.nonceProvider.SetDpopNonce(resp.Header.Get("DPoP-Nonce"))
		if err != nil {
			return nil, err
		}
	}

	// retry with new nonce
	req2 := req.Clone(req.Context())
	req2.RequestURI = ""

	req2.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	err = s.sign(req2)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req2)
}

func (s *DpopHttpClient) sign(req *http.Request) error {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: s.key},
		&jose.SignerOptions{
			ExtraHeaders: map[jose.HeaderKey]any{
				jose.HeaderType: "dpop+jwt",
				"jwk": &jose.JSONWebKey{
					Key:       s.key.Public(),
					Use:       "sig",
					Algorithm: string(jose.ES256),
				},
			},
		},
	)
	if err != nil {
		return err
	}

	claims, err := s.generateClaims(req)
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

func (s *DpopHttpClient) generateClaims(req *http.Request) (*dpopClaims, error) {
	htu := req.URL.String()
	if s.opts.HTU != "" {
		htu = s.opts.HTU
	}

	claims := &dpopClaims{
		Claims: jwt.Claims{
			ID:       base64.StdEncoding.EncodeToString([]byte(uuid.NewString())),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		Method: req.Method,
		URL:    htu,
	}

	nonce, ok, err := s.nonceProvider.GetDpopNonce()
	if err != nil {
		return nil, err
	}
	if ok {
		claims.Nonce = nonce
	}

	if s.opts.AccessToken != "" {
		claims.AccessTokenHash = s.hashAccessToken(s.opts.AccessToken)
	}

	return claims, nil
}

func (s *DpopHttpClient) hashAccessToken(accessToken string) string {
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
