package pdsclient

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

	return newURL.String(), nil
}

func doesHandleBelongToDomain(handle string, domain string) bool {
	return strings.HasSuffix(handle, "."+domain) || handle == domain
}
