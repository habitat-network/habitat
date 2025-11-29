package bffauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// BFF Auth - Allow for Habitat node's to authenticate with each other.
type Provider struct {
	challengePersister ChallengeSessionPersister
	signingKey         []byte
}

func NewProvider(challengePersister ChallengeSessionPersister, signingKey []byte) *Provider {
	return &Provider{
		challengePersister: challengePersister,
		signingKey:         signingKey,
	}
}

// p.handleChallenge takes the following http request
type ChallengeRequest struct {
	DID                string `json:"did"`
	PublicKeyMultibase []byte `json:"public_key_bytes"`
}

// p.handleChallenge responds with the following response
type ChallengeResponse struct {
	Challenge string `json:"challenge"`
	Session   string `jsno:"session"`
}

func (p *Provider) handleChallenge(w http.ResponseWriter, r *http.Request) {
	var req ChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	challenge, err := GenerateChallenge()
	if err != nil {
		http.Error(w, "Failed to generate challenge", http.StatusInternalServerError)
		return
	}

	key, err := atcrypto.ParsePublicBytesP256([]byte(req.PublicKeyMultibase))
	if err != nil {
		http.Error(w, "Failed to parse public key", http.StatusInternalServerError)
		return
	}

	// Create session with UUID
	sessionID := uuid.New().String()
	session := &ChallengeSession{
		SessionID: sessionID,
		DID:       req.DID,
		PublicKey: key,
		Challenge: challenge,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	err = p.challengePersister.SaveSession(session)
	if err != nil {
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp, err := json.Marshal(ChallengeResponse{
		Challenge: challenge,
		Session:   sessionID,
	})
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(resp); err != nil {
		log.Err(err).Msgf("error sending response in handleChallenge")
	}
}

type AuthRequest struct {
	SessionID string `json:"session_id"`
	Proof     string `json:"proof"`
}

type AuthResponse struct {
	Token string `json:"token"`
}

func (p *Provider) handleAuth(w http.ResponseWriter, r *http.Request) {
	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := p.challengePersister.GetSession(req.SessionID)
	if err != nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	err = p.challengePersister.DeleteSession(req.SessionID)
	if err != nil {
		http.Error(w, "Failed to delete session", http.StatusInternalServerError)
		return
	}

	// Verify proof
	valid, err := VerifyProof(session.Challenge, req.Proof, session.PublicKey)
	if err != nil || !valid {
		http.Error(w, "Invalid proof", http.StatusUnauthorized)
		return
	}

	// Generate JWT
	token, err := GenerateJWT(p.signingKey, session.DID)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Sanity check by validating the token
	_, err = ValidateJWT(token, p.signingKey)
	if err != nil {
		http.Error(w, "Failed to validate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp, err := json.Marshal(AuthResponse{
		Token: token,
	})
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(resp); err != nil {
		log.Err(err).Msgf("error sending response in handleAuth")
	}
}

func (p *Provider) handleTest(w http.ResponseWriter, r *http.Request) {
	err := ValidateRequest(r, p.signingKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unauthorized: %s", err), http.StatusUnauthorized)
		return
	}

	bytes, err := json.Marshal(map[string]string{
		"message": "Hello, world!",
	})
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(bytes)
	if err != nil {
		log.Err(err).Msgf("error sending response in handleTest")
	}
}

func (p *Provider) ValidateToken(token string) (string, error) {
	claims, err := ValidateJWT(token, p.signingKey)
	if err != nil {
		return "", err
	}
	dids := []string(claims.Audience)
	if len(dids) > 1 || len(dids) <= 0 {
		return "", fmt.Errorf("invalidly formatted jwt claims audience")
	}
	return dids[0], nil
}
