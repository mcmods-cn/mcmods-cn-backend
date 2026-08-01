package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
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
	if strings.HasPrefix(encoded, "argon2id$") {
		return verifyArgon2ID(password, encoded)
	}
	return verifyPBKDF2(password, encoded)
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

func verifyPBKDF2(password string, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "pbkdf2" || parts[1] != "sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[2])
	if err != nil || iterations < 100000 || iterations > 2_000_000 {
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
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
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

func pbkdf2SHA256(password []byte, salt []byte, iterations int, keyLen int) []byte {
	hashLen := sha256.Size
	numBlocks := (keyLen + hashLen - 1) / hashLen
	output := make([]byte, 0, numBlocks*hashLen)

	for block := 1; block <= numBlocks; block++ {
		u := prf(password, appendInt(salt, uint32(block))) // #nosec G115 -- key length is bounded to 64 bytes.
		t := make([]byte, hashLen)
		copy(t, u)
		for i := 1; i < iterations; i++ {
			u = prf(password, u)
			for j := 0; j < hashLen; j++ {
				t[j] ^= u[j]
			}
		}
		output = append(output, t...)
	}
	return output[:keyLen]
}

func prf(key []byte, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func appendInt(salt []byte, block uint32) []byte {
	out := make([]byte, len(salt)+4)
	copy(out, salt)
	binary.BigEndian.PutUint32(out[len(salt):], block)
	return out
}
