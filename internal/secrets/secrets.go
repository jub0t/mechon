// Package secrets encrypts values at rest (bot environment variables) with AES-256-GCM.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

type Box struct{ aead cipher.AEAD }

// NewKey returns a fresh key, base64 encoded, for MECHON_SECRET_KEY.
func NewKey() string {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

// Open parses a base64 32-byte key.
func Open(key string) (*Box, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("MECHON_SECRET_KEY must be 32 random bytes, base64 encoded (generate one with `mechon keygen`)")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. The additional data binds the ciphertext to its owner (e.g. bot id and
// key name), so a value copied to another row fails to decrypt.
func (b *Box) Seal(plaintext, ad []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return b.aead.Seal(nonce, nonce, plaintext, ad)
}

func (b *Box) Open(ciphertext, ad []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(ciphertext) < n {
		return nil, errors.New("ciphertext too short")
	}
	pt, err := b.aead.Open(nil, ciphertext[:n], ciphertext[n:], ad)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return pt, nil
}
