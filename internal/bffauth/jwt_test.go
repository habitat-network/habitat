package bffauth

import (
	"net/http"
	"testing"
	"time"
)

func TestJWTFlow(t *testing.T) {
	// Create a test signing key
	signingKey := []byte("test-signing-key")

	// Generate JWT with the challenge
	token, err := GenerateJWT(signingKey)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	// Validate the JWT
	claims, err := ValidateJWT(token, signingKey)
	if err != nil {
		t.Fatalf("Failed to validate JWT: %v", err)
	}

	// Verify expiration time is set correctly (15 minutes from now)
	expectedExpiry := time.Now().Add(15 * time.Minute)
	if claims.ExpiresAt.Time.Sub(expectedExpiry) > time.Second {
		t.Errorf("Unexpected expiration time. Got %v, want approximately %v",
			claims.ExpiresAt.Time, expectedExpiry)
	}
}

func TestValidateRequest(t *testing.T) {
	signingKey := []byte("test-signing-key")

	// Generate a valid JWT token
	token, err := GenerateJWT(signingKey)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	// Test valid request
	validReq := &http.Request{
		Header: make(http.Header),
	}
	validReq.Header.Set("Authorization", "Bearer "+token)

	err = ValidateRequest(validReq, signingKey)
	if err != nil {
		t.Errorf("Expected valid request to pass validation, got error: %v", err)
	}

	// Test invalid request (bad token)
	invalidReq := &http.Request{
		Header: make(http.Header),
	}
	invalidReq.Header.Set("Authorization", "Bearer invalid-token")

	err = ValidateRequest(invalidReq, signingKey)
	if err == nil {
		t.Error("Expected invalid request to fail validation")
	}

	// Test missing Authorization header
	missingAuthReq := &http.Request{
		Header: make(http.Header),
	}

	err = ValidateRequest(missingAuthReq, signingKey)
	if err == nil {
		t.Error("Expected request with missing Authorization header to fail validation")
	}

	// Test malformed Authorization header
	malformedReq := &http.Request{
		Header: make(http.Header),
	}
	malformedReq.Header.Set("Authorization", "BadPrefix "+token)

	err = ValidateRequest(malformedReq, signingKey)
	if err == nil {
		t.Error("Expected request with malformed Authorization header to fail validation")
	}
}
