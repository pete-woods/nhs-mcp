// Package oauth implements the client side of NHS login's OIDC authorization
// code flow with PKCE (RFC 7636).
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// PKCE is a code verifier and its S256 challenge.
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE generates a random verifier, 43 characters as the NHS App uses.
func NewPKCE() PKCE {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	v := base64.RawURLEncoding.EncodeToString(b)
	return PKCE{Verifier: v, Challenge: S256(v)}
}

// S256 is the S256 code challenge for verifier.
func S256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
