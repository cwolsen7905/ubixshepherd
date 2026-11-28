package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// OwnerID is the first user: the owner account as it was before there were users. A login
// with no user name is this user's.
const OwnerID int64 = 1

// AliasLocal is the alias method of password sign-in; its subject is the user's name.
const AliasLocal = "local"

// Role is what a user may do in one tenant.
type Role string

// The roles, from most to least: an owner adds tenants, an admin changes a tenant's
// settings and users, a member works in it, a viewer reads.
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// ParseRole reads a role name.
func ParseRole(s string) (Role, error) {
	switch r := Role(strings.ToLower(strings.TrimSpace(s))); r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return r, nil
	}
	return "", fmt.Errorf("unknown role %q: owner, admin, member or viewer", s)
}

// Errors of the user directory.
var (
	ErrNoUser     = errors.New("no such user")
	ErrUserExists = errors.New("a user with that name already exists")
	ErrNoTenant   = errors.New("no such tenant")
	// ErrLastOwner refuses a change that would leave a tenant with no owner.
	ErrLastOwner = errors.New("that would leave the tenant with no owner")
)

// User is one person, the same in every tenant they belong to.
type User struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	Created time.Time    `json:"created"`
	Updated time.Time    `json:"updated"`
	Members []Membership `json:"memberships"`
	// HasPassword is false for a user added without one yet: they cannot sign in.
	HasPassword bool `json:"has_password"`
}

// Membership is a user's role in one tenant, named by its slug.
type Membership struct {
	Tenant string `json:"tenant"`
	Role   Role   `json:"role"`
}

// Users is the directory of users: who they are, their memberships, and the account store
// of each. Every method is safe for concurrent use.
type Users interface {
	// ForUser is one user's account store: their password, factors, recovery codes and
	// device tokens, and nobody else's.
	ForUser(id int64) Store
	// UserByAlias finds the user an alias names, or ErrNoUser.
	UserByAlias(ctx context.Context, method, subject string) (User, error)
	// User reads one user by id, or ErrNoUser.
	User(ctx context.Context, id int64) (User, error)
	// Users lists every user, by name.
	Users(ctx context.Context) ([]User, error)
	// AddUser makes a user with no password, its local alias, and a membership with the
	// role in the tenant. ErrUserExists when the name is taken, ErrNoTenant for a tenant
	// that does not exist.
	AddUser(ctx context.Context, name, tenant string, role Role, now time.Time) (User, error)
	// RemoveUser deletes the user and everything that is theirs: aliases, memberships,
	// factors, recovery codes, device tokens and browser sessions. ErrNoUser, or
	// ErrLastOwner when they are some tenant's only owner.
	RemoveUser(ctx context.Context, id int64) error
	// SetRole gives the user the role in the tenant; an empty role takes the membership
	// away. ErrLastOwner when it would leave the tenant with no owner.
	SetRole(ctx context.Context, id int64, tenant string, role Role) error
	// DeviceUser says whose device token has this hash.
	DeviceUser(ctx context.Context, hash string) (id int64, found bool, err error)
}

// CheckUserName says whether a name can be a user's: 1 to 64 letters, digits, '.', '-'
// or '_', starting with a letter or digit, so it reads the same in a log, a URL and an
// authenticator app.
func CheckUserName(name string) error {
	if name == "" || len(name) > 64 {
		return errors.New("a user name is 1 to 64 characters")
	}
	for i, r := range name {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || (i > 0 && strings.ContainsRune(".-_", r))
		if !ok || r > unicode.MaxASCII {
			return errors.New("a user name is letters, digits, '.', '-' and '_', starting with a letter or digit")
		}
	}
	return nil
}

type userKey struct{}

// WithUser carries the user a login was for to what runs after it (the session start).
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// UserFrom is the user WithUser carried, and whether there was one.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}
