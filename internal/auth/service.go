package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode"
)

// Device token lifetimes: a token lasts DeviceIdle past its last use, and never past
// DeviceCap from the login that made it.
const (
	DeviceIdle = 30 * 24 * time.Hour
	DeviceCap  = 90 * 24 * time.Hour
)

const (
	// RecoveryCodeCount codes are made with the account.
	RecoveryCodeCount = 10
	// AccountName is the one account's name in limits and in the authenticator app.
	AccountName = "owner"
	deviceToken = "shd_"
)

// Errors a login can give. The first is the same for a wrong password, a wrong code and
// a reused code, so an attacker learns nothing from which it was.
var (
	ErrInvalid = errors.New("wrong password or code")
	// ErrLoginAgain is a device token that is unknown, revoked or expired: one answer
	// for all three, which says nothing of the token's age.
	ErrLoginAgain = errors.New("log in again")
)

// LockedError is a refusal because of too many failures.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	return "too many attempts: try again after " + e.Until.UTC().Format("15:04")
}

// Service is the login. Build it with its fields; Store and Factor are required.
type Service struct {
	Store   Store
	Factor  SecondFactor
	Limiter *Limiter
	// Sessions ends browser sessions on a sign-out everywhere; nil when there are none.
	Sessions SessionEnder
	// OnLock is called once each time repeated failure locks the account (one feed item).
	OnLock func(addr netip.Addr, until time.Time)
	Now    func() time.Time
	// Users is the directory of users. Nil is the owner alone, with Store and Factor; set,
	// Store and Factor are the owner's, and NewFactor builds another user's factor.
	Users     Users
	NewFactor func(Store) SecondFactor

	// user is set on a service ForUser made: Store and Factor are this user's.
	user User
}

// ForUser is the service for one user: their store, their factor, their sessions. Without
// a directory there is only the owner, and that is s.
func (s *Service) ForUser(u User) *Service {
	if s.Users == nil {
		c := *s
		c.user = u
		return &c
	}
	c := *s
	c.Store = s.Users.ForUser(u.ID)
	c.Factor = s.NewFactor(c.Store)
	c.user = u
	return &c
}

// User is the user this service is for: the owner on a service ForUser did not make.
func (s *Service) User() User {
	if s.user.ID != 0 {
		return s.user
	}
	return User{ID: OwnerID, Name: AccountName}
}

// Lookup finds a user by the name they sign in with; an empty name is the owner.
// ErrNoUser when there is none.
func (s *Service) Lookup(ctx context.Context, name string) (User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = AccountName
	}
	if s.Users == nil {
		if name != AccountName {
			return User{}, ErrNoUser
		}
		return User{ID: OwnerID, Name: AccountName}, nil
	}
	return s.Users.UserByAlias(ctx, AliasLocal, name)
}

// LookupID finds a user by id.
func (s *Service) LookupID(ctx context.Context, id int64) (User, error) {
	if s.Users == nil {
		if id != OwnerID {
			return User{}, ErrNoUser
		}
		return User{ID: OwnerID, Name: AccountName}, nil
	}
	return s.Users.User(ctx, id)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Setup is what `account set` shows once: the enrolment and the recovery codes.
type Setup struct {
	Enrolment     Enrolment `json:"enrolment"`
	RecoveryCodes []string  `json:"recovery_codes"`
}

// SetPassword sets the owner's password, enrols a fresh second factor in place of any
// before it, makes ten new recovery codes, and signs everything out: every session and
// device token ends, because the thing that proved the person has changed.
func (s *Service) SetPassword(ctx context.Context, pw string) (Setup, error) {
	if len([]rune(pw)) < MinPasswordLen {
		return Setup{}, fmt.Errorf("the password needs at least %d characters", MinPasswordLen)
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return Setup{}, err
	}
	now := s.now()
	created := now
	if old, err := s.Store.Account(ctx); err == nil {
		created = old.Created
	}
	if err := s.Store.PutAccount(ctx, Account{PasswordHash: hash, Created: created, Updated: now}); err != nil {
		return Setup{}, err
	}
	infos, err := s.Factor.List(ctx)
	if err != nil {
		return Setup{}, err
	}
	for _, f := range infos {
		if err := s.Factor.Remove(ctx, f.ID); err != nil {
			return Setup{}, err
		}
	}
	enrol, err := s.Factor.Enrol(ctx, s.User().Name)
	if err != nil {
		return Setup{}, err
	}
	codes, err := s.newRecoveryCodes(ctx)
	if err != nil {
		return Setup{}, err
	}
	if s.Limiter != nil {
		if s.Users == nil {
			s.Limiter.Reset()
		} else {
			s.Limiter.Succeed(s.User().Name) // other users' locks, and addresses', stay
		}
	}
	if _, _, err := s.SignOutEverywhere(ctx); err != nil {
		return Setup{}, err
	}
	return Setup{Enrolment: enrol, RecoveryCodes: codes}, nil
}

// ResetSecondFactor enrols a new factor and new recovery codes, and signs everything out.
func (s *Service) ResetSecondFactor(ctx context.Context) (Setup, error) {
	if _, err := s.Store.Account(ctx); err != nil {
		return Setup{}, err
	}
	infos, err := s.Factor.List(ctx)
	if err != nil {
		return Setup{}, err
	}
	for _, f := range infos {
		if err := s.Factor.Remove(ctx, f.ID); err != nil {
			return Setup{}, err
		}
	}
	enrol, err := s.Factor.Enrol(ctx, s.User().Name)
	if err != nil {
		return Setup{}, err
	}
	codes, err := s.newRecoveryCodes(ctx)
	if err != nil {
		return Setup{}, err
	}
	if _, _, err := s.SignOutEverywhere(ctx); err != nil {
		return Setup{}, err
	}
	return Setup{Enrolment: enrol, RecoveryCodes: codes}, nil
}

func (s *Service) newRecoveryCodes(ctx context.Context) ([]string, error) {
	var plain []string
	var rows []RecoveryCode
	for range RecoveryCodeCount {
		raw := make([]byte, 7)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		c := strings.ToLower(b32.EncodeToString(raw))[:10]
		h, err := hashSecret(codeCost, c)
		if err != nil {
			return nil, err
		}
		id, err := randID()
		if err != nil {
			return nil, err
		}
		plain = append(plain, c[:5]+"-"+c[5:])
		rows = append(rows, RecoveryCode{ID: id, Hash: h})
	}
	return plain, s.Store.ReplaceRecovery(ctx, rows)
}

// Attempt is one login: the user, the password and either a TOTP code or a recovery
// code, from the client address the request counts against (see ClientAddr). An empty
// User is the owner.
type Attempt struct {
	User           string
	Password, Code string
	Addr           netip.Addr
}

// Login checks an attempt. It returns nil only when the password and a code both hold;
// ErrInvalid for any wrong, reused or missing part; *LockedError when limits say stop.
// Every failure counts against the user and the address.
func (s *Service) Login(ctx context.Context, a Attempt) error {
	_, err := s.LoginUser(ctx, a)
	return err
}

// LoginUser is Login that also gives the service of the user who signed in. An unknown
// user, or one with no password yet, is ErrInvalid like a wrong password, after the same
// work, so the answer says nothing of who exists.
func (s *Service) LoginUser(ctx context.Context, a Attempt) (*Service, error) {
	now := s.now()
	name := strings.TrimSpace(a.User)
	if name == "" {
		name = AccountName
	}
	if s.Limiter != nil {
		if until, blocked := s.Limiter.Blocked(name, a.Addr, now); blocked {
			return nil, &LockedError{Until: until}
		}
	}
	fail := func() (*Service, error) {
		if s.Limiter != nil && s.Limiter.Fail(name, a.Addr, now) && s.OnLock != nil {
			s.OnLock(a.Addr, now.Add(s.Limiter.L.Lock))
		}
		return nil, ErrInvalid
	}
	u, err := s.Lookup(ctx, name)
	if errors.Is(err, ErrNoUser) {
		CheckPassword(dummyHash(), a.Password)
		return fail()
	}
	if err != nil {
		return nil, err
	}
	us := s.ForUser(u)
	acct, err := us.Store.Account(ctx)
	if errors.Is(err, ErrNoAccount) && u.ID != OwnerID {
		CheckPassword(dummyHash(), a.Password)
		return fail()
	}
	if err != nil {
		return nil, err
	}
	ok := CheckPassword(acct.PasswordHash, a.Password)
	if ok {
		ok, err = us.checkCode(ctx, a.Code, now)
		if err != nil {
			return nil, err
		}
	}
	if !ok {
		return fail()
	}
	if s.Limiter != nil {
		s.Limiter.Succeed(name)
	}
	return us, nil
}

// checkCode takes a TOTP code from the factor, or else a recovery code, which works once.
func (s *Service) checkCode(ctx context.Context, code string, now time.Time) (bool, error) {
	if s.Factor.Handles(code) {
		return s.Factor.Verify(ctx, code, now)
	}
	c := strings.ToLower(normCode(code))
	if len(c) != 10 {
		return false, nil
	}
	rows, err := s.Store.RecoveryCodes(ctx)
	if err != nil {
		return false, err
	}
	var hit string
	for _, r := range rows { // every unused row is hashed: timing does not say which
		if r.Used {
			continue
		}
		if good, _ := verifySecret(r.Hash, c); good {
			hit = r.ID
		}
	}
	if hit == "" {
		return false, nil
	}
	return s.Store.UseRecovery(ctx, hit)
}

// IssueDevice makes a named device token for a login that just succeeded. The token is
// returned once; only its hash is kept.
func (s *Service) IssueDevice(ctx context.Context, name string) (string, Device, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "cli"
	}
	if len([]rune(name)) > 64 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", Device{}, errors.New("a device name is at most 64 printable characters")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Device{}, err
	}
	id, err := randID()
	if err != nil {
		return "", Device{}, err
	}
	tok := deviceToken + base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	d := Device{ID: id, Name: name, Hash: HashToken(tok), Created: now, LastUsed: now, Expires: now.Add(DeviceIdle)}
	if err := s.Store.PruneDevices(ctx, now); err != nil {
		return "", Device{}, err
	}
	return tok, d, s.Store.PutDevice(ctx, d)
}

// HashToken is the SHA-256 a token is kept as: tokens are 256 random bits, so a fast
// hash is enough, and it makes a lookup by hash possible.
func HashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// CheckDevice finds the live device a token names and pushes its expiry out: 30 days
// from now, but never past 90 days from the login that made it. An unknown, revoked or
// expired token is ErrLoginAgain, and an expired row is deleted.
func (s *Service) CheckDevice(ctx context.Context, tok string) (Device, error) {
	if !strings.HasPrefix(tok, deviceToken) {
		return Device{}, ErrLoginAgain
	}
	if s.Users != nil && s.user.ID == 0 {
		id, found, err := s.Users.DeviceUser(ctx, HashToken(tok))
		if err != nil {
			return Device{}, err
		}
		if !found {
			return Device{}, ErrLoginAgain
		}
		u, err := s.Users.User(ctx, id)
		if errors.Is(err, ErrNoUser) {
			return Device{}, ErrLoginAgain
		}
		if err != nil {
			return Device{}, err
		}
		return s.ForUser(u).CheckDevice(ctx, tok)
	}
	d, found, err := s.Store.DeviceByHash(ctx, HashToken(tok))
	if err != nil {
		return Device{}, err
	}
	now := s.now()
	if !found {
		return Device{}, ErrLoginAgain
	}
	hard := d.Created.Add(DeviceCap)
	if !now.Before(d.Expires) || !now.Before(hard) {
		_, err := s.Store.DeleteDevice(ctx, d.ID)
		if err != nil {
			return Device{}, err
		}
		return Device{}, ErrLoginAgain
	}
	next := now.Add(DeviceIdle)
	if next.After(hard) {
		next = hard
	}
	if err := s.Store.TouchDevice(ctx, d.ID, now, next); err != nil {
		return Device{}, err
	}
	d.LastUsed, d.Expires = now, next
	d.UserID = s.User().ID
	return d, nil
}

// Devices lists device tokens, without their hashes.
func (s *Service) Devices(ctx context.Context) ([]Device, error) {
	ds, err := s.Store.Devices(ctx)
	for i := range ds {
		ds[i].Hash = ""
	}
	return ds, err
}

// RevokeDevice ends one device token; false if there was none.
func (s *Service) RevokeDevice(ctx context.Context, id string) (bool, error) {
	return s.Store.DeleteDevice(ctx, id)
}

// SignOutEverywhere ends every browser session and every device token. Both are rows in
// the store, so it holds across a restart.
func (s *Service) SignOutEverywhere(ctx context.Context) (sessions, devices int, err error) {
	if s.Sessions != nil {
		if ue, ok := s.Sessions.(UserSessionEnder); ok && s.Users != nil {
			sessions, err = ue.EndUserSessions(ctx, s.User().ID)
		} else {
			sessions, err = s.Sessions.EndAllSessions(ctx)
		}
		if err != nil {
			return 0, 0, err
		}
	}
	devices, err = s.Store.DeleteDevices(ctx)
	return sessions, devices, err
}
