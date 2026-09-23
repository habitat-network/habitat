// Package emaildomain maps email domains to the opensocial orgs that own
// them, and records which DID each work email was provisioned as, backing
// email-based sign-in (see identity.EmailResolver).
package emaildomain

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var ErrInvalidEmail = errors.New("invalid email address")

// Email is a bare, lowercased email address, e.g. "alice@acme.com".
type Email string

// LoginMethod is how members of an email domain prove they own their email.
type LoginMethod string

const LoginMethodGoogle LoginMethod = "google"

// ParseEmail validates s as a bare email address ("local@domain", with no
// display name or angle brackets) whose domain is a valid DNS name, and
// lowercases it so lookups are case-insensitive.
func ParseEmail(s string) (Email, error) {
	lower := strings.ToLower(s)
	addr, err := mail.ParseAddress(lower)
	if err != nil || addr.Name != "" || addr.Address != lower {
		return "", fmt.Errorf("%w: %q", ErrInvalidEmail, s)
	}
	// Handle syntax is DNS-name syntax, and also rejects single-label hosts
	// like "localhost".
	if _, err := syntax.ParseHandle(lower[strings.LastIndex(lower, "@")+1:]); err != nil {
		return "", fmt.Errorf("%w: %q: invalid domain", ErrInvalidEmail, s)
	}
	return Email(lower), nil
}

// Domain returns the part after the last "@", e.g. "acme.com".
func (e Email) Domain() string {
	return string(e[strings.LastIndex(string(e), "@")+1:])
}

// LocalPart returns the part before the last "@", e.g. "alice".
func (e Email) LocalPart() string {
	return string(e[:strings.LastIndex(string(e), "@")])
}
