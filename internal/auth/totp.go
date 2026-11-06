package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238's default, and what authenticator apps speak
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpDigits = 6
	// totpSkew is how many periods either side of now a code is accepted, for clock drift.
	totpSkew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret is a fresh 160-bit secret.
func NewTOTPSecret() ([]byte, error) {
	s := make([]byte, 20)
	_, err := rand.Read(s)
	return s, err
}

// totpCode is RFC 6238's code for one step.
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000)
}

func stepOf(t time.Time) int64 { return t.Unix() / totpPeriod }

// matchTOTP returns the step a code is valid for near now, or 0 and false. The caller
// refuses a step at or before the last one used, so a code cannot be used twice.
func matchTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	var hit int64
	cur := stepOf(now)
	for s := cur - totpSkew; s <= cur+totpSkew; s++ {
		// No early exit: every step is compared, so timing does not say which matched.
		if hmac.Equal([]byte(totpCode(secret, s)), []byte(code)) {
			hit = s
		}
	}
	return hit, hit != 0
}

// TOTPURI is the otpauth:// URI an authenticator app enrols from.
func TOTPURI(issuer, account string, secret []byte) string {
	v := url.Values{}
	v.Set("secret", b32.EncodeToString(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(totpDigits))
	v.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

// normCode strips what people type into a code field: spaces and dashes.
func normCode(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}

// TOTPCodeAt is the code an authenticator holding the enrolment's typed secret (base32)
// shows at t. It is for tests and for a person's own tooling.
func TOTPCodeAt(secretBase32 string, t time.Time) (string, error) {
	s, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secretBase32)))
	if err != nil {
		return "", err
	}
	return totpCode(s, stepOf(t)), nil
}
