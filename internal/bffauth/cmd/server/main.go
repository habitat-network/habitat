package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/eagraf/habitat-new/internal/bffauth"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type TestServer struct {
	challengePersister bffauth.ChallengeSessionPersister

	// Used for HMAC signing of JWTs.
	signingKey []byte

	// This is a toy example. In the real world there would be a mapping of
	// DIDs to public keys.
	publicKey crypto.PublicKey
}

func (s *TestServer) challengeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	challenge, err := bffauth.GenerateChallenge()
	if err != nil {
		http.Error(w, "Failed to generate challenge", http.StatusInternalServerError)
		return
	}

	// Create session with UUID
	sessionID := uuid.New().String()
	session := &bffauth.ChallengeSession{
		SessionID: sessionID,
		Challenge: challenge,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	err = s.challengePersister.SaveSession(session)
	if err != nil {
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(map[string]string{
		"challenge": challenge,
		"session":   sessionID,
	})
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *TestServer) authHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req bffauth.AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.challengePersister.GetSession(req.SessionID)
	if err != nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	err = s.challengePersister.DeleteSession(req.SessionID)
	if err != nil {
		http.Error(w, "Failed to delete session", http.StatusInternalServerError)
		return
	}

	// Verify proof
	valid, err := bffauth.VerifyProof(session.Challenge, req.Proof, s.publicKey)
	if err != nil || !valid {
		http.Error(w, "Invalid proof", http.StatusUnauthorized)
		return
	}

	// Generate JWT
	token, err := bffauth.GenerateJWT(s.signingKey)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(map[string]string{
		"token": token,
	})
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *TestServer) HelloWorldAuthenticatedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := bffauth.ValidateRequest(r, s.signingKey)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(map[string]string{
		"message": "Hello, world!",
	})
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func main() {
	err := godotenv.Load("test.env")
	if err != nil {
		log.Fatalf("error loading .env file: %v", err)
	}
	fmt.Println(os.Getenv("ALICE_PUBLIC_KEY_MULTIBASE"))

	publicKeyMultibase := os.Getenv("ALICE_PUBLIC_KEY_MULTIBASE")

	publicKey, err := crypto.ParsePublicMultibase(publicKeyMultibase)
	if err != nil {
		log.Fatalf("failed to parse public key: %v", err)
	}

	// In-memory session storage for demo purposes
	sessionPersister := bffauth.NewInMemorySessionPersister()

	server := &TestServer{
		challengePersister: sessionPersister,
		signingKey:         []byte("test-signing-key"),
		publicKey:          publicKey,
	}

	http.HandleFunc("/challenge", server.challengeHandler)
	http.HandleFunc("/auth", server.authHandler)
	http.HandleFunc("/hello", server.HelloWorldAuthenticatedHandler)

	port := ":8080"
	fmt.Printf("Starting server on port %s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}
