package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"time"
)

// SecondFactor is a way to prove the person after the password: enrol, verify, list,
// remove. TOTP is the first implementation; a passkey (WebAuthn) can be another without
// the login flow changing.
type SecondFactor interface {
	// Kind names the factor: "totp".
	Kind() string
	// Enrol adds a factor and returns what the person needs once to set it up.
	Enrol(ctx context.Context, label string) (Enrolment, error)
	// Verify checks a code against this kind's enrolled factors. ok is false for a code
	// that is wrong, or already used; err is only for a store or key failure.
	Verify(ctx context.Context, code string, now time.Time) (ok bool, err error)
	// Handles says whether a typed code looks like this factor's, so the login can try
	// recovery codes for anything else.
	Handles(code string) bool
	List(ctx context.Context) ([]FactorInfo, error)
	Remove(ctx context.Context, id string) error
}

// Enrolment is shown to the person once, when a factor is added.
type Enrolment struct {
	FactorID string `json:"factor_id"`
	// URI is the otpauth:// address an authenticator app takes (TOTP).
	URI string `json:"uri"`
	// Secret is the same key in the form people type, for an app without a camera.
	Secret string `json:"secret"`
}

// FactorInfo is a factor as listed: never its secret.
type FactorInfo struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Label   string    `json:"label"`
	Created time.Time `json:"created"`
}

// ErrNoFactor says Remove named a factor that is not enrolled.
var ErrNoFactor = errors.New("no such second factor")

// TOTP is the SecondFactor for time-based one-time codes.
type TOTP struct {
	Store  Store
	Key    *Key
	Issuer string
	Now    func() time.Time
}

func (t *TOTP) Kind() string { return "totp" }

func (t *TOTP) Handles(code string) bool {
	c := normCode(code)
	if len(c) != totpDigits {
		return false
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (t *TOTP) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *TOTP) Enrol(ctx context.Context, label string) (Enrolment, error) {
	secret, err := NewTOTPSecret()
	if err != nil {
		return Enrolment{}, err
	}
	id, err := randID()
	if err != nil {
		return Enrolment{}, err
	}
	sealed, err := t.Key.Seal(secret, id)
	if err != nil {
		return Enrolment{}, err
	}
	now := t.now()
	if err := t.Store.PutFactor(ctx, FactorRecord{ID: id, Kind: t.Kind(), Label: label, Secret: sealed, Created: now}); err != nil {
		return Enrolment{}, err
	}
	issuer := t.Issuer
	if issuer == "" {
		issuer = "uBixShepherd"
	}
	return Enrolment{FactorID: id, URI: TOTPURI(issuer, label, secret), Secret: b32.EncodeToString(secret)}, nil
}

func (t *TOTP) Verify(ctx context.Context, code string, now time.Time) (bool, error) {
	code = normCode(code)
	fs, err := t.Store.Factors(ctx)
	if err != nil {
		return false, err
	}
	ok := false
	for _, f := range fs {
		if f.Kind != t.Kind() {
			continue
		}
		secret, err := t.Key.Open(f.Secret, f.ID)
		if err != nil {
			return false, err
		}
		step, hit := matchTOTP(secret, code, now)
		if !hit || ok {
			continue
		}
		fresh, err := t.Store.AdvanceStep(ctx, f.ID, step)
		if err != nil {
			return false, err
		}
		ok = fresh
	}
	return ok, nil
}

func (t *TOTP) List(ctx context.Context) ([]FactorInfo, error) {
	fs, err := t.Store.Factors(ctx)
	if err != nil {
		return nil, err
	}
	var out []FactorInfo
	for _, f := range fs {
		if f.Kind == t.Kind() {
			out = append(out, FactorInfo{ID: f.ID, Kind: f.Kind, Label: f.Label, Created: f.Created})
		}
	}
	return out, nil
}

func (t *TOTP) Remove(ctx context.Context, id string) error {
	fs, err := t.Store.Factors(ctx)
	if err != nil {
		return err
	}
	for _, f := range fs {
		if f.ID == id && f.Kind == t.Kind() {
			return t.Store.DeleteFactor(ctx, id)
		}
	}
	return ErrNoFactor
}

func randID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func sortFactors(fs []FactorRecord) {
	sort.Slice(fs, func(i, j int) bool {
		return fs[i].Created.Before(fs[j].Created) || (fs[i].Created.Equal(fs[j].Created) && fs[i].ID < fs[j].ID)
	})
}

func sortDevices(ds []Device) {
	sort.Slice(ds, func(i, j int) bool {
		return ds[i].Created.Before(ds[j].Created) || (ds[i].Created.Equal(ds[j].Created) && ds[i].ID < ds[j].ID)
	})
}
