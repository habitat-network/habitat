package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	_ "github.com/bluesky-social/indigo/atproto/auth" // Register ES256K signing method
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/tebeka/selenium"
	"github.com/testcontainers/testcontainers-go/network"
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
func createAccountJWT(
	privateKey *atcrypto.PrivateKeyK256,
	did string,
	pdsDID string,
) (string, error) {
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

	certDir := t.TempDir()
	generateSelfSignedCert(t, certDir)

	testNetwork, err := network.New(ctx, network.WithDriver("bridge"))

	require.NoError(t, err, "failed to create network")
	t.Cleanup(func() {
		if err := testNetwork.Remove(ctx); err != nil {
			t.Logf("Failed to remove network: %v", err)
		}
	})

	// Start PostgreSQL container
	pgURL := NewPostgresContainer(ctx, t, testNetwork.Name)
	t.Logf("Postgres URL (Docker network): %s", pgURL)

	env := NewTestEnvironment(
		ctx,
		t,
		StandardIntegrationRequests(t, testNetwork.Name, certDir, pgURL),
	)

	// Extract URLs from named containers
	pdsProxyHost, err := env.Get("pds-proxy").Host(ctx)
	require.NoError(t, err)
	pdsProxyPort, err := env.Get("pds-proxy").MappedPort(ctx, "443")
	require.NoError(t, err)
	pdsURL := fmt.Sprintf("https://%s:%s", pdsProxyHost, pdsProxyPort.Port())

	priviHost, err := env.Get("privi").Host(ctx)
	require.NoError(t, err)
	priviPort, err := env.Get("privi").MappedPort(ctx, "443")
	require.NoError(t, err)
	priviURL := fmt.Sprintf("https://%s:%s", priviHost, priviPort.Port())

	seleniumHost, err := env.Get("selenium").Host(ctx)
	require.NoError(t, err)
	seleniumPort, err := env.Get("selenium").MappedPort(ctx, "4444")
	require.NoError(t, err)
	seleniumURL := fmt.Sprintf("http://%s:%s/wd/hub", seleniumHost, seleniumPort.Port())

	t.Logf("Privi URL: %s", priviURL)
	t.Logf("PDS URL (host): %s", pdsURL)
	t.Logf("PDS URL (Docker network): https://pds.example.com")
	t.Logf("Selenium URL: %s", seleniumURL)

	// Load DID keypair from environment
	// NOTE: The DID document at sashankg.github.io must have serviceEndpoint: "https://pds.example.com"
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
	testDID := createPDSAccount(t, client, pdsURL, testHandle, testPassword, keyPair)
	t.Logf("Created test user with DID: %s", testDID)

	// Step 2: Get frontend container URL (using Docker network alias)
	frontendHost, err := env.Get("frontend").Host(ctx)
	require.NoError(t, err)
	frontendPort, err := env.Get("frontend").MappedPort(ctx, "443")
	require.NoError(t, err)
	frontendURL := fmt.Sprintf("https://%s:%s", frontendHost, frontendPort.Port())

	t.Logf("Frontend URL (host): %s", frontendURL)
	t.Logf("Frontend URL (Docker network): https://frontend.habitat")

	// Step 3: Use Selenium to perform the OAuth login flow through the frontend
	t.Logf("Starting OAuth flow via frontend")
	performOAuthLoginWithBrowser(
		t,
		seleniumURL,
		integrationDID,
		testPassword,
	)
	t.Logf("OAuth flow completed successfully")
}

// performOAuthLoginWithBrowser uses Selenium to automate the OAuth login flow via the frontend
func performOAuthLoginWithBrowser(
	t *testing.T,
	seleniumURL string,
	handle string,
	password string,
) {
	t.Helper()

	// Connect to the Selenium container
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{
				"--ignore-certificate-errors",
				"--disable-web-security",
			},
		},
		"acceptInsecureCerts": true,
	}

	wd, err := selenium.NewRemote(caps, seleniumURL)
	require.NoError(t, err, "Failed to connect to Selenium")
	defer wd.Quit()
	wd.SetImplicitWaitTimeout(5 * time.Second)

	// Navigate to the frontend's OAuth login endpoint (using Docker network address)
	frontendOAuthURL := "https://frontend.habitat/oauth-login"
	t.Logf("Navigating to: %s", frontendOAuthURL)
	err = wd.Get(frontendOAuthURL)
	require.NoError(t, err, "Failed to navigate to frontend OAuth login")

	// Get current URL after initial load
	currentURL, err := wd.CurrentURL()
	require.NoError(t, err)
	t.Logf("Current URL after initial load: %s", currentURL)

	// Get page source to debug
	pageSource, err := wd.PageSource()
	require.NoError(t, err)
	t.Logf("Page source:\n%s", pageSource)

	// Enter the handle/DID
	handleInput, err := wd.FindElement(selenium.ByName, "handle")
	require.NoError(t, err, "Failed to find handle input")
	err = handleInput.Clear()
	require.NoError(t, err, "Failed to clear handle input")
	err = handleInput.SendKeys(handle)
	require.NoError(t, err, "Failed to enter handle")

	// Click the "Login" button to initiate OAuth flow
	loginButton, err := wd.FindElement(selenium.ByCSSSelector, `button[type="submit"]`)
	require.NoError(t, err, "Failed to find login button")
	err = loginButton.Click()
	require.NoError(t, err, "Failed to click login button")

	// Get current URL after redirect (should be at Privi's authorize endpoint)
	currentURL, err = wd.CurrentURL()
	require.NoError(t, err)
	t.Logf("Current URL after clicking login: %s", currentURL)

	// Get page source to debug
	pageSource, err = wd.PageSource()
	require.NoError(t, err)
	t.Logf("Page source after clicking login:\n%s", pageSource)

	// Enter password
	passwordInput, err := wd.FindElement(selenium.ByName, "password")
	require.NoError(t, err, "Failed to find password input")
	err = passwordInput.SendKeys(password)
	require.NoError(t, err, "Failed to enter password")

	// Submit the password form
	submitButton, err := wd.FindElement(selenium.ByCSSSelector, `button[type="submit"]`)
	require.NoError(t, err, "Failed to find submit button")

	err = submitButton.Click()
	require.NoError(t, err, "Failed to click submit button")

	wd.Wait(func(wd selenium.WebDriver) (bool, error) {
		title, err := wd.Title()
		if err != nil {
			return false, err
		}
		return title == "Authorize", nil
	})

	// Check URL and page source after password submission
	urlAfterPassword, err := wd.CurrentURL()
	require.NoError(t, err)
	t.Logf("URL after password submission: %s", urlAfterPassword)

	pageAfterPassword, err := wd.PageSource()
	require.NoError(t, err)
	t.Logf("Page source after password submission:\n%s", pageAfterPassword)

	// If still on the same URL, the page might be showing an error or consent inline
	// Try to find a submit button for the consent screen
	consentButton, err := wd.FindElement(selenium.ByCSSSelector, `button[type="submit"]`)
	require.NoError(t, err, "Failed to find consent button")
	t.Logf("Found consent button, clicking it")
	err = consentButton.Click()
	require.NoError(t, err, "Failed to click consent button")

	wd.Wait(func(wd selenium.WebDriver) (bool, error) {
		currentURL, err := wd.CurrentURL()
		if err != nil {
			return false, err
		}
		return strings.HasPrefix(currentURL, "https://frontend.habitat"), nil
	})

	// The final URL should be back at the frontend (after it receives the auth code)
	finalURL, err := wd.CurrentURL()
	require.NoError(t, err)
	t.Logf("Final URL after login: %s", finalURL)

	// Get final page source to verify success
	finalPageSource, err := wd.PageSource()
	require.NoError(t, err)
	t.Logf("Final page source:\n%s", finalPageSource)

	// Verify we're back at the frontend (not at an error page)
	require.Contains(t, finalURL, "frontend.habitat", "Expected to be redirected back to frontend")
}

// createPDSAccount creates a test account on the PDS and returns the DID
func createPDSAccount(
	t *testing.T,
	client *http.Client,
	pdsURL, handle, password string,
	keyPair *didKeyPair,
) string {
	t.Helper()

	// For testing, the PDS DID is standard did:web (HTTPS on port 443 via nginx proxy)
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
	req, err := http.NewRequest(
		"POST",
		pdsURL+"/xrpc/com.atproto.server.createAccount",
		bytes.NewBuffer(reqBody),
	)
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
		require.Equal(
			t,
			http.StatusOK,
			resp.StatusCode,
			"Expected status code 200, got %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)

	did, ok := result["did"].(string)
	require.True(t, ok, "Expected 'did' field in create account response")

	return did
}
