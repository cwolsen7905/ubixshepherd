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

// MinPasswordLen is the shortest password `account set` accepts.
const MinPasswordLen = 12

// argonParams is the cost of one hash. A var so tests can lower it; the encoded hash
// carries its own parameters, so changing it never strands an old hash.
type argonParams struct {
	Time, Memory uint32
	Threads      uint8
}

var (
	// passwordCost is argon2id at 64 MiB, for the one password.
	passwordCost = argonParams{Time: 2, Memory: 64 * 1024, Threads: 2}
	// codeCost is lighter, for recovery codes, which carry 50 bits themselves and of
	// which ten are tried on a use.
	codeCost = argonParams{Time: 2, Memory: 19 * 1024, Threads: 1}
)

const argonKeyLen = 32

func hashSecret(p argonParams, secret string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(secret), salt, p.Time, p.Memory, p.Threads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var errBadHash = errors.New("malformed hash")

func verifySecret(encoded, secret string) (bool, error) {
	f := strings.Split(encoded, "$")
	if len(f) != 6 || f[1] != "argon2id" {
		return false, errBadHash
	}
	var ver int
	var p argonParams
	if _, err := fmt.Sscanf(f[2], "v=%d", &ver); err != nil || ver != argon2.Version {
		return false, errBadHash
	}
	var th uint
	if _, err := fmt.Sscanf(f[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &th); err != nil || th == 0 || th > 255 || p.Memory > 1<<20 || p.Time > 20 {
		return false, errBadHash
	}
	p.Threads = uint8(th)
	salt, err := base64.RawStdEncoding.DecodeString(f[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(f[5])
	if err != nil || len(want) == 0 {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(secret), salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// HashPassword is the argon2id hash a Store keeps in place of a password.
func HashPassword(pw string) (string, error) { return hashSecret(passwordCost, pw) }

// CheckPassword compares a password with a stored hash in constant time.
func CheckPassword(hash, pw string) bool {
	ok, err := verifySecret(hash, pw)
	return err == nil && ok
}

// UseCheapHashingForTests drops the hash cost so another package's tests do not spend
// seconds in argon2. It must never be called outside a test binary.
func UseCheapHashingForTests() {
	passwordCost = argonParams{Time: 1, Memory: 64, Threads: 1}
	codeCost = passwordCost
}
