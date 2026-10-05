// Package auth holds password hashing and token handling. It has no database or HTTP code.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters from the spec: 64 MiB, 3 passes, 2 lanes.
const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16

	MinPasswordLen = 10
	MaxPasswordLen = 256 // bounds hashing cost on hostile input
)

var ErrMismatch = errors.New("password does not match")

// HashPassword returns a PHC-format argon2id string, e.g. "$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>".
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks password against a hash made by HashPassword. Parameters are read from the
// hash, so raising them later keeps old hashes valid.
func VerifyPassword(hash, password string) error {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("unsupported password hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return errors.New("unsupported argon2 version")
	}
	var mem uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return errors.New("malformed argon2 parameters")
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return errors.New("malformed salt")
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return errors.New("malformed hash")
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// dummyHash is verified against when a login names an unknown account, so a wrong email takes as
// long as a wrong password and the response time does not reveal which accounts exist.
var dummyHash, _ = HashPassword("mechon-timing-equaliser")

func BurnVerify(password string) { _ = VerifyPassword(dummyHash, password) }
