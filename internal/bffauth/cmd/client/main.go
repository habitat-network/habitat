package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/eagraf/habitat-new/internal/bffauth"
	"github.com/eagraf/habitat-new/util"
	"github.com/joho/godotenv"
)

func getChallenge() (string, string, error) {
	resp, err := http.Get("http://localhost:8080/challenge")
	if err != nil {
		return "", "", fmt.Errorf("error getting challenge: %w", err)
	}
	defer util.Close(resp.Body)

	var result struct {
		Challenge string `json:"challenge"`
		Session   string `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("error decoding challenge response: %w", err)
	}

	return result.Challenge, result.Session, nil
}

func submitProof(sessionID string, proof string) (string, error) {
	reqBody, err := json.Marshal(map[string]string{
		"session_id": sessionID,
		"proof":      proof,
	})
	if err != nil {
		return "", fmt.Errorf("error marshaling proof request: %w", err)
	}

	resp, err := http.Post(
		"http://localhost:8080/auth",
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return "", fmt.Errorf("error submitting proof: %w", err)
	}
	defer util.Close(resp.Body)

	// Read the entire response body first
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading auth response: %w", err)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("error decoding auth response: %w", err)
	}

	return result.Token, nil
}

func getHelloWorld(token string) (string, error) {
	req, err := http.NewRequest("GET", "http://localhost:8080/hello", nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error making authenticated request: %w", err)
	}
	defer util.Close(resp.Body)

	var result struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error decoding hello response: %w", err)
	}

	return result.Message, nil
}

func main() {
	err := godotenv.Load("test.env")
	if err != nil {
		log.Fatalf("error loading .env file: %v", err)
	}

	alicePrivateKey := os.Getenv("ALICE_PRIVATE_KEY_MULTIBASE")
	if alicePrivateKey == "" {
		log.Fatalf("ALICE_PRIVATE_KEY_MULTIBASE is not set")
	}

	privateKey, err := crypto.ParsePrivateMultibase(alicePrivateKey)
	if err != nil {
		log.Fatalf("error parsing alice private key: %v", err)
	}

	challenge, session, err := getChallenge()
	if err != nil {
		log.Fatalf("error getting challenge: %v", err)
	}

	log.Printf("Got challenge: %s for session: %s", challenge, session)

	proof, err := bffauth.GenerateProof(challenge, privateKey)
	if err != nil {
		log.Fatalf("error generating proof: %v", err)
	}

	token, err := submitProof(session, proof)
	if err != nil {
		log.Fatalf("error submitting proof: %v", err)
	}

	message, err := getHelloWorld(token)
	if err != nil {
		log.Fatalf("error getting hello world: %v", err)
	}

	if message != "Hello, world!" {
		log.Fatalf("unexpected message: %s", message)
	}

	fmt.Printf("Successfully authenticated and got message: %s\n", message)
}
