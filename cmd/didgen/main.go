package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:  "didgen",
		Usage: "Generate a DID keypair and DID document for testing",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "pds-url",
				Usage:    "PDS service endpoint URL",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "did-host",
				Usage:    "Hostname (with optional port) for the did:web identifier (e.g., 'example.com' or 'example.com:8443')",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "output",
				Usage: "Path to the output DID document file",
				Value: "did.json",
			},
		},
		Action: run,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	pdsURL := cmd.String("pds-url")
	didHost := cmd.String("did-host")
	outputPath := cmd.String("output")

	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Generate a new secp256k1 private key
	privKey, err := atcrypto.GeneratePrivateKeyK256()
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Get the public key
	pubKey, err := privKey.PublicKey()
	if err != nil {
		return fmt.Errorf("failed to get public key: %w", err)
	}

	// Get multibase-encoded public key
	// This uses the "Multikey" format: compressed key bytes with multicodec prefix, base58btc encoded, 'z' prefix
	multibaseKey := pubKey.(*atcrypto.PublicKeyK256).Multibase()

	// Create the DID identifier (did:web format)
	// Per did:web spec: ports must be percent-encoded (:8443 becomes %3A8443)
	// and paths are colon-separated (example.com/user/alice becomes example.com:user:alice)
	did := formatDidWeb(didHost)

	// Create the DID document
	didDoc := map[string]interface{}{
		"@context": []string{
			"https://www.w3.org/ns/did/v1",
			"https://w3id.org/security/suites/secp256k1-2019/v1",
		},
		"id": did,
		"verificationMethod": []map[string]interface{}{
			{
				"id":                 fmt.Sprintf("%s#atproto", did),
				"type":               "Multikey", // Current/preferred format per atproto spec
				"controller":         did,
				"publicKeyMultibase": multibaseKey,
			},
		},
		"service": []map[string]interface{}{
			{
				"id":              "#atproto_pds",
				"type":            "AtprotoPersonalDataServer",
				"serviceEndpoint": pdsURL,
			},
		},
	}

	// Marshal the DID document to JSON
	didDocJSON, err := json.MarshalIndent(didDoc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal DID document: %w", err)
	}

	// Check if output path is a directory
	if info, err := os.Stat(outputPath); err == nil && info.IsDir() {
		return fmt.Errorf("output path is a directory, not a file: %s", outputPath)
	}

	// Write DID document to file
	if err := os.WriteFile(outputPath, didDocJSON, 0644); err != nil {
		return fmt.Errorf("failed to write DID document to %s: %w", outputPath, err)
	}

	// Export the private key as hex-encoded string
	privKeyBytes := privKey.Bytes()
	privKeyHex := fmt.Sprintf("%x", privKeyBytes)

	// Get absolute path for display
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		absPath = outputPath
	}

	// Pretty print all the information
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  DID Keypair Generated")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Printf("DID:\n  %s\n\n", did)
	fmt.Printf("Public Key (Multibase):\n  %s\n\n", multibaseKey)
	fmt.Printf("Private Key (Hex):\n  %s\n\n", privKeyHex)
	fmt.Printf("PDS Service Endpoint:\n  %s\n\n", pdsURL)
	fmt.Printf("DID Document Location:\n  %s\n\n", absPath)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("⚠️  Keep the private key secure - do not commit to version control!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return nil
}

// formatDidWeb converts a hostname (with optional port) into a did:web identifier
// Per did:web specification, ports are percent-encoded: example.com:8443 -> did:web:example.com%3A8443
func formatDidWeb(host string) string {
	// Check if host contains a port
	if strings.Contains(host, ":") {
		parts := strings.SplitN(host, ":", 2)
		if len(parts) == 2 {
			// Percent-encode the port separator
			return fmt.Sprintf("did:web:%s%%3A%s", parts[0], parts[1])
		}
	}

	return fmt.Sprintf("did:web:%s", host)
}
