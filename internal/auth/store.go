package auth

import (
	"context"
	"errors"
	"time"
)

// ErrNoAccount says no owner account has been set.
var ErrNoAccount = errors.New("no account is set: run `shepherd account set` on the daemon's machine")

// Account is the one owner account. The hash is argon2id; the password is never kept.
type Account struct {
	PasswordHash string
	Created      time.Time
	Updated      time.Time
}

// FactorRecord is one enrolled second factor. Secret is sealed with the Key.
type FactorRecord struct {
	ID      string
	Kind    string
	Label   string
	Secret  []byte
	Created time.Time
	// LastStep is the newest TOTP step accepted; a code at or before it is refused.
	LastStep int64
}

// RecoveryCode is the hash of one single-use recovery code.
type RecoveryCode struct {
	ID   string
	Hash string
	Used bool
}

// Device is a CLI device token's row. The token itself is never kept, only its hash.
type Device struct {
	ID   string
	Name string
	Hash string
	// Created is the login that made the token; the 90-day cap counts from it.
	Created  time.Time
	LastUsed time.Time
	Expires  time.Time
}

// Store is what the daemon keeps for the account. The sqlite store implements it in
// its own tables (the account, factors, recovery codes and device_tokens); sessions stay
// in web_sessions, ended through SessionEnder. Every method is safe for concurrent use.
type Store interface {
	// Account returns ErrNoAccount before one is set.
	Account(ctx context.Context) (Account, error)
	PutAccount(ctx context.Context, a Account) error

	Factors(ctx context.Context) ([]FactorRecord, error)
	PutFactor(ctx context.Context, f FactorRecord) error
	DeleteFactor(ctx context.Context, id string) error
	// AdvanceStep moves a factor's LastStep to step only if step is newer, and says
	// whether it did. It is the one atomic check that stops a code being used twice.
	AdvanceStep(ctx context.Context, id string, step int64) (bool, error)

	// ReplaceRecovery swaps every recovery code for a new set.
	ReplaceRecovery(ctx context.Context, codes []RecoveryCode) error
	RecoveryCodes(ctx context.Context) ([]RecoveryCode, error)
	// UseRecovery marks one unused code used and says whether it was unused.
	UseRecovery(ctx context.Context, id string) (bool, error)

	PutDevice(ctx context.Context, d Device) error
	// DeviceByHash returns the device and true, or false when none matches.
	DeviceByHash(ctx context.Context, hash string) (Device, bool, error)
	// TouchDevice records a use and the new expiry.
	TouchDevice(ctx context.Context, id string, used, expires time.Time) error
	Devices(ctx context.Context) ([]Device, error)
	DeleteDevice(ctx context.Context, id string) (bool, error)
	DeleteDevices(ctx context.Context) (int, error)
	// PruneDevices deletes devices expired before now.
	PruneDevices(ctx context.Context, now time.Time) error
}

// SessionEnder ends the daemon's browser sessions, which live outside this package.
type SessionEnder interface {
	EndAllSessions(ctx context.Context) (int, error)
}
