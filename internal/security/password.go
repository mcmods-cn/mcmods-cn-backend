package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordSaltBytes    = 16
	passwordKeyBytes     = 32
	argon2Time           = uint32(3)
	argon2MemoryKiB      = uint32(64 * 1024)
	argon2Threads        = uint8(2)
	argon2Version        = argon2.Version
	maxArgon2MemoryKiB   = uint32(256 * 1024)
	maxArgon2Iterations  = uint32(10)
	maxArgon2Parallelism = uint8(16)
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2MemoryKiB, argon2Threads, passwordKeyBytes)
	return fmt.Sprintf(
		"argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version,
		argon2MemoryKiB,
		argon2Time,
		argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(password string, encoded string) bool {
	if !strings.HasPrefix(encoded, "argon2id$") {
		return false
	}
	return verifyArgon2ID(password, encoded)
}

func verifyArgon2ID(password string, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	var version int
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil || version != argon2Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil ||
		memory < 8*1024 || memory > maxArgon2MemoryKiB ||
		iterations == 0 || iterations > maxArgon2Iterations ||
		threads == 0 || threads > maxArgon2Parallelism {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	keyLength := uint32(len(expected)) // #nosec G115 -- decoded key length is validated to the range 16..64 above.
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, threads, keyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func HashCode(code string) (string, error) {
	if strings.TrimSpace(code) == "" {
		return "", errors.New("empty code")
	}
	return HashPassword(code)
}

func VerifyCode(code string, encoded string) bool {
	return VerifyPassword(code, encoded)
}
