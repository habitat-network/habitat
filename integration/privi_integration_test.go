package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	_ "github.com/bluesky-social/indigo/atproto/auth" // Register ES256K signing method
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/tebeka/selenium"
	"golang.org/x/net/publicsuffix"
)

// didKeyPair holds a secp256k1 key pair for did:web testing
type didKeyPair struct {
	privateKey         *atcrypto.PrivateKeyK256
	publicKeyMultibase string
	did                string
}

const integrationDID = "did:web:sashankg.github.io"

// loadDIDKeyPairFromEnv loads a DID keypair from environment variable
func loadDIDKeyPairFromEnv() (*didKeyPair, error) {
	privKeyHex := os.Getenv("INTEGRATION_DID_PRIVKEY")
	if privKeyHex == "" {
		return nil, fmt.Errorf("INTEGRATION_DID_PRIVKEY environment variable is required")
	}

	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	}

	privKey, err := atcrypto.ParsePrivateBytesK256(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	pubKey, err := privKey.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}

	return &didKeyPair{
		privateKey:         privKey,
		publicKeyMultibase: pubKey.(*atcrypto.PublicKeyK256).Multibase(),
		did:                integrationDID,
	}, nil
}

// createAccountJWT creates a JWT signed by the DID's private key for account creation
func createAccountJWT(privateKey *atcrypto.PrivateKeyK256, did string, pdsDID string) (string, error) {
	// The auth package init() function registers ES256K and ES256 signing methods
	// with the jwt library, so we can use them directly

	// Create JWT claims for account creation
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": did,                                // Issuer is the DID
		"aud": pdsDID,                             // Audience is the PDS DID (not URL!)
		"lxm": "com.atproto.server.createAccount", // Lexicon method
		"exp": now.Add(5 * time.Minute).Unix(),    // Expires in 5 minutes
		"iat": now.Unix(),                         // Issued at
	}

	// Create token with ES256K signing method (registered by indigo/atproto/auth)
	// The signing method works directly with atcrypto.PrivateKey types
	token := jwt.NewWithClaims(jwt.GetSigningMethod("ES256K"), claims)

	// Sign the token - the custom signing method will call privateKey.HashAndSign()
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return tokenString, nil
}

func TestPriviOAuthFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	env, err := NewTestEnvironment(ctx, t)
	require.NoError(t, err)
	defer env.Cleanup()

	t.Logf("Privi URL: %s", env.PriviURL)
	t.Logf("PDS URL (host): %s", env.PDSURL)
	t.Logf("PDS URL (Docker network): http://pds.example.com:3000")

	// Load DID keypair from environment
	// NOTE: The DID document at sashankg.github.io must have serviceEndpoint: "http://pds.example.com:3000"
	// for Privi (running in Docker) to reach the PDS container via Docker networking
	keyPair, err := loadDIDKeyPairFromEnv()
	require.NoError(t, err, "INTEGRATION_DID_PRIVKEY environment variable must be set")

	t.Logf("Using DID: %s", integrationDID)
	t.Logf("Public key: %s", keyPair.publicKeyMultibase)

	// Create an HTTP client that accepts self-signed certificates
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects automatically - we want to inspect them
			return http.ErrUseLastResponse
		},
	}

	// Step 1: Create a test user account on the PDS
	// Use simple alphanumeric handle without hyphens (invalid for atproto handles)
	testHandle := "testuser.pds.example.com"
	testPassword := "test-password-123"
	testDID := createPDSAccount(t, client, env.PDSURL, testHandle, testPassword, keyPair, env)
	t.Logf("Created test user with DID: %s", testDID)

	// Step 2: Start a mock OAuth client server with client metadata
	clientServer, clientMetadataURL, callbackURL := startOAuthClientServer(t)
	defer clientServer.Close()
	t.Logf("OAuth client metadata URL: %s", clientMetadataURL)
	t.Logf("OAuth client callback URL: %s", callbackURL)

	// Step 3: Initiate OAuth flow from Privi
	authParams := neturl.Values{
		"client_id":             {clientMetadataURL},
		"redirect_uri":          {callbackURL},
		"response_type":         {"code"},
		"state":                 {"test-state-123"},
		"code_challenge":        {"test-challenge"},
		"code_challenge_method": {"S256"},
		"scope":                 {"atproto"},
		"handle":                {integrationDID},
	}

	authorizeURL := fmt.Sprintf("%s/oauth/authorize?%s", env.PriviURL, authParams.Encode())
	t.Logf("Starting OAuth flow with: %s", authorizeURL)

	// Create the browser-accessible URL (using Docker network alias)
	browserAuthorizeURL := fmt.Sprintf("https://privi.local:8443/oauth/authorize?%s", authParams.Encode())
	t.Logf("Browser will navigate to: %s", browserAuthorizeURL)

	// Step 4: Use Selenium to perform the OAuth login flow through a browser
	authCode, redirectURL := performOAuthLoginWithBrowser(t, ctx, env.TestNetwork, browserAuthorizeURL, integrationDID, testPassword)
	t.Logf("OAuth flow completed. Auth code: %s", authCode[:10]+"...")
	t.Logf("Redirect URL: %s", redirectURL)

	// Step 4: Exchange the authorization code for tokens
	tokenResp := exchangeAuthCodeForTokens(t, client, env.PriviURL, authCode, authParams)
	t.Logf("Access token obtained: %s", tokenResp["access_token"].(string)[:20]+"...")

	// Step 5: Verify we can use the access token
	assert.NotEmpty(t, tokenResp["access_token"], "Expected access token")
	assert.NotEmpty(t, tokenResp["refresh_token"], "Expected refresh token")
	assert.Equal(t, "Bearer", tokenResp["token_type"], "Expected Bearer token type")
}

// func TestPriviClientMetadata(t *testing.T) {
// 	if testing.Short() {
// 		t.Skip("skipping integration test")
// 	}
//
// 	ctx := context.Background()
// 	env, err := NewTestEnvironment(ctx)
// 	require.NoError(t, err)
// 	defer env.Cleanup()
//
// 	client := &http.Client{
// 		Transport: &http.Transport{
// 			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
// 		},
// 	}
//
// 	// Test that the client metadata endpoint is accessible
// 	resp, err := client.Get(env.PriviURL + "/client-metadata.json")
// 	require.NoError(t, err)
// 	defer resp.Body.Close()
//
// 	assert.Equal(t, http.StatusOK, resp.StatusCode)
// 	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
//
// 	var metadata map[string]interface{}
// 	err = json.NewDecoder(resp.Body).Decode(&metadata)
// 	require.NoError(t, err)
//
// 	// Verify essential OAuth client metadata fields
// 	assert.NotEmpty(t, metadata["client_id"])
// 	assert.NotEmpty(t, metadata["client_name"])
// 	assert.NotEmpty(t, metadata["redirect_uris"])
// 	assert.Equal(t, "web", metadata["application_type"])
// 	assert.Contains(t, metadata["grant_types"], "authorization_code")
// 	assert.Contains(t, metadata["grant_types"], "refresh_token")
//
// 	t.Logf("Client metadata: %+v", metadata)
// }

// func TestPriviDIDDocument(t *testing.T) {
// 	if testing.Short() {
// 		t.Skip("skipping integration test")
// 	}
//
// 	ctx := context.Background()
// 	env, err := NewTestEnvironment(ctx)
// 	require.NoError(t, err)
// 	defer env.Cleanup()
//
// 	client := &http.Client{
// 		Transport: &http.Transport{
// 			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
// 		},
// 	}
//
// 	// Test the DID document endpoint
// 	resp, err := client.Get(env.PriviURL + "/.well-known/did.json")
// 	require.NoError(t, err)
// 	defer resp.Body.Close()
//
// 	assert.Equal(t, http.StatusOK, resp.StatusCode)
//
// 	var didDoc map[string]interface{}
// 	err = json.NewDecoder(resp.Body).Decode(&didDoc)
// 	require.NoError(t, err)
//
// 	// Verify DID document structure
// 	assert.Contains(t, didDoc["id"], "did:web:")
// 	assert.NotEmpty(t, didDoc["@context"])
// 	assert.NotEmpty(t, didDoc["service"])
//
// 	t.Logf("DID document: %+v", didDoc)
// }

// startOAuthClientServer starts a test HTTP server that serves OAuth client metadata
func startOAuthClientServer(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()

	var authCodeReceived string
	var metadataURL, callbackURL string
	mux := http.NewServeMux()

	// Serve OAuth client metadata
	mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		metadata := map[string]interface{}{
			"client_id":                  metadataURL, // Use the pre-computed URL
			"client_name":                "Test OAuth Client",
			"redirect_uris":              []string{callbackURL}, // Use the pre-computed URL
			"grant_types":                []string{"authorization_code", "refresh_token"},
			"response_types":             []string{"code"},
			"token_endpoint_auth_method": "none", // Public client
			"application_type":           "web",
			"scope":                      "atproto",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadata)
	})

	// Handle OAuth callback
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		authCodeReceived = r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		t.Logf("Received OAuth callback: code=%s, state=%s", authCodeReceived[:10]+"...", state)

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><h1>Authorization Successful!</h1><p>Code: %s</p></body></html>`, authCodeReceived[:10]+"...")
	})

	server := httptest.NewServer(mux)

	// Extract the port from the server URL
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)

	// Create URLs using host.docker.internal so Docker containers can access them
	dockerAccessibleURL := fmt.Sprintf("http://host.docker.internal:%s", port)
	metadataURL = dockerAccessibleURL + "/client-metadata.json"
	callbackURL = dockerAccessibleURL + "/callback"

	t.Logf("OAuth client server started on port %s", port)
	t.Logf("  - Docker-accessible metadata URL: %s", metadataURL)
	t.Logf("  - Localhost URL: %s", server.URL)

	return server, metadataURL, callbackURL
}

// performOAuthLoginWithBrowser uses Selenium in a Docker container to automate the OAuth login flow
func performOAuthLoginWithBrowser(t *testing.T, ctx context.Context, testNetwork *testcontainers.DockerNetwork, authorizeURL, did, password string) (authCode string, redirectURL string) {
	t.Helper()

	// Start Selenium Chrome container on the same network as PDS and Privi
	// Use seleniarm for ARM64 support (Apple Silicon)
	req := testcontainers.ContainerRequest{
		Image:        "seleniarm/standalone-chromium:latest",
		ExposedPorts: []string{"4444/tcp"},
		Networks:     []string{testNetwork.Name},
		WaitingFor:   wait.ForListeningPort("4444/tcp"),
	}

	seleniumContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := seleniumContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate Selenium container: %v", err)
		}
	}()

	// Get the Selenium WebDriver URL
	seleniumHost, err := seleniumContainer.Host(ctx)
	require.NoError(t, err)
	seleniumPort, err := seleniumContainer.MappedPort(ctx, "4444")
	require.NoError(t, err)
	seleniumURL := fmt.Sprintf("http://%s:%s/wd/hub", seleniumHost, seleniumPort.Port())

	t.Logf("Selenium WebDriver URL: %s", seleniumURL)

	// Connect to the Selenium WebDriver
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{
				"--no-sandbox",
				"--disable-dev-shm-usage",
				"--ignore-certificate-errors", // Ignore self-signed certificate errors
			},
		},
	}

	wd, err := selenium.NewRemote(caps, seleniumURL)
	require.NoError(t, err)
	defer wd.Quit()

	// Navigate to the OAuth authorization URL
	t.Logf("Navigating to: %s", authorizeURL)
	err = wd.Get(authorizeURL)
	require.NoError(t, err)

	// Wait for the redirect to PDS and the form to be ready
	// Try multiple times with increasing waits to account for redirects
	var identifierInput selenium.WebElement
	maxAttempts := 10
	for i := 0; i < maxAttempts; i++ {
		currentURL, _ := wd.CurrentURL()
		t.Logf("Attempt %d: Current URL: %s", i+1, currentURL)
		
		// Try to find the identifier input
		identifierInput, err = wd.FindElement(selenium.ByName, "identifier")
		if err == nil {
			break
		}
		
		// Log the page source for debugging
		if i == maxAttempts-1 {
			pageSource, _ := wd.PageSource()
			t.Logf("Page source:\n%s", pageSource[:min(len(pageSource), 1000)])
		}
		
		time.Sleep(time.Second)
	}
	require.NoError(t, err, "Failed to find identifier input after %d attempts", maxAttempts)
	err = identifierInput.SendKeys(did)
	require.NoError(t, err)

	passwordInput, err := wd.FindElement(selenium.ByName, "password")
	require.NoError(t, err)
	err = passwordInput.SendKeys(password)
	require.NoError(t, err)

	// Submit the form
	submitButton, err := wd.FindElement(selenium.ByCSSSelector, `button[type="submit"]`)
	require.NoError(t, err)
	err = submitButton.Click()
	require.NoError(t, err)

	// Wait for redirect back to the callback URL
	time.Sleep(2 * time.Second)

	// The final URL should be the redirect_uri with code parameter
	finalURL, err := wd.CurrentURL()
	require.NoError(t, err)
	t.Logf("Final URL after login: %s", finalURL)

	// Parse the authorization code from the URL
	parsedURL, err := neturl.Parse(finalURL)
	require.NoError(t, err)

	authCode = parsedURL.Query().Get("code")
	require.NotEmpty(t, authCode, "Expected authorization code in callback URL")

	return authCode, finalURL
}

// exchangeAuthCodeForTokens exchanges the authorization code for access and refresh tokens
func exchangeAuthCodeForTokens(t *testing.T, client *http.Client, priviURL, code string, originalParams neturl.Values) map[string]interface{} {
	t.Helper()

	tokenRequest := neturl.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {originalParams.Get("redirect_uri")},
		"client_id":     {originalParams.Get("client_id")},
		"code_verifier": {"test-verifier"}, // This should match the challenge
	}

	resp, err := client.PostForm(priviURL+"/oauth/token", tokenRequest)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "Token exchange should succeed")

	var tokenResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&tokenResp)
	require.NoError(t, err)

	return tokenResp
}

// createPDSAccount creates a test account on the PDS and returns the DID
func createPDSAccount(t *testing.T, client *http.Client, pdsURL, handle, password string, keyPair *didKeyPair, env *TestEnvironment) string {
	t.Helper()

	// For testing, the PDS DID is typically did:web:pds.example.com (based on PDS_HOSTNAME)
	pdsDID := "did:web:pds.example.com"

	// Create a signed JWT to prove we control the DID
	authJWT, err := createAccountJWT(keyPair.privateKey, keyPair.did, pdsDID)
	require.NoError(t, err)
	t.Logf("Created auth JWT for audience: %s", pdsDID)
	t.Logf("Created auth JWT: %s", authJWT[:50]+"...")

	// Create account request with did:web pointing to our test server
	createAccountReq := map[string]interface{}{
		"handle":   handle,
		"email":    handle + "@example.com",
		"password": password,
		"did":      keyPair.did,
	}

	reqBody, err := json.Marshal(createAccountReq)
	require.NoError(t, err)

	// Create the request with Authorization header
	req, err := http.NewRequest("POST", pdsURL+"/xrpc/com.atproto.server.createAccount", bytes.NewBuffer(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authJWT)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Check if account creation was successful
	if resp.StatusCode != http.StatusOK {
		t.Logf("Failed to create account. Status: %d, Body: %s", resp.StatusCode, string(body))

		// Get PDS logs to see what went wrong
		if logs, err := env.GetPDSLogs(); err == nil {
			t.Logf("PDS Container Logs:\n%s", logs)
		} else {
			t.Logf("Failed to get PDS logs: %v", err)
		}

		t.Fatalf("Account creation failed - cannot continue test")
		return ""
	}

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)

	did, ok := result["did"].(string)
	require.True(t, ok, "Expected 'did' field in create account response")

	return did
}
