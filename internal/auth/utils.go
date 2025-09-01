package auth

import (
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

func generateNonce() string {
	return base64.StdEncoding.EncodeToString([]byte(uuid.NewString()))
}

func mapAuthServerURL(oldURL string) (string, error) {
	newURL, err := url.Parse(oldURL)
	if err != nil {
		return "", err
	}
	// Exception for PDS running in docker container
	if newURL.Host == "localhost:3000" {
		newURL.Host = "host.docker.internal:5001"
	}

	return newURL.String(), nil
}

func doesHandleBelongToDomain(handle string, domain string) bool {
	return strings.HasSuffix(handle, "."+domain) || handle == domain
}
