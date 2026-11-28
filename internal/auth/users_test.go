package auth

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

// userEnder records whose sessions were ended.
type userEnder struct {
	all   int
	users []int64
}

func (e *userEnder) EndAllSessions(context.Context) (int, error) { e.all++; return 0, nil }
func (e *userEnder) EndUserSessions(_ context.Context, id int64) (int, error) {
	e.users = append(e.users, id)
	return 1, nil
}

// multi is a service with a directory: the owner and alice, each with a password and a
// factor.
type multi struct {
	svc      *Service
	dir      *MemUsers
	clk      *clock
	key      *Key
	end      *userEnder
	alice    User
	ownerPw  string
	alicePw  string
	ownerRec []string
}

func newMulti(t *testing.T) *multi {
	t.Helper()
	key, err := LoadKey("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := &multi{dir: NewMemUsers(nil), clk: &clock{t0}, key: key, end: &userEnder{}, ownerPw: "owner password one", alicePw: "alice password two"}
	newFactor := func(st Store) SecondFactor { return &TOTP{Store: st, Key: key, Now: m.clk.now} }
	owner := m.dir.ForUser(OwnerID)
	m.svc = &Service{Store: owner, Factor: newFactor(owner), Users: m.dir, NewFactor: newFactor,
		Limiter: NewLimiter(Limits{}), Sessions: m.end, Now: m.clk.now}
	setup, err := m.svc.SetPassword(ctx, m.ownerPw)
	if err != nil {
		t.Fatal(err)
	}
	m.ownerRec = setup.RecoveryCodes
	if m.alice, err = m.dir.AddUser(ctx, "alice", "default", RoleMember, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.svc.ForUser(m.alice).SetPassword(ctx, m.alicePw); err != nil {
		t.Fatal(err)
	}
	return m
}

// code is a user's authenticator code at the clock's time.
func (m *multi) code(t *testing.T, id int64) string {
	t.Helper()
	fs, _ := m.dir.ForUser(id).Factors(ctx)
	secret, err := m.key.Open(fs[0].Secret, fs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(secret, stepOf(m.clk.t))
}

func (m *multi) step() { m.clk.t = m.clk.t.Add(31 * time.Second) }

// Each user signs in with their own name, password and code; the owner still signs in
// with no name.
func TestEachUserSignsInAsThemselves(t *testing.T) {
	m := newMulti(t)
	us, err := m.svc.LoginUser(ctx, Attempt{User: "alice", Password: m.alicePw, Code: m.code(t, m.alice.ID), Addr: addr1})
	if err != nil || us.User().ID != m.alice.ID {
		t.Fatalf("alice: %v %+v", err, us)
	}
	m.step()
	us, err = m.svc.LoginUser(ctx, Attempt{Password: m.ownerPw, Code: m.code(t, OwnerID), Addr: addr1})
	if err != nil || us.User().ID != OwnerID {
		t.Fatalf("owner with no name: %v", err)
	}
	m.step()
	// One user's password or code does not open another's account.
	for _, a := range []Attempt{
		{User: "alice", Password: m.ownerPw, Code: m.code(t, m.alice.ID)},
		{User: "alice", Password: m.alicePw, Code: m.code(t, OwnerID)},
		{User: "owner", Password: m.alicePw, Code: m.code(t, m.alice.ID)},
		{User: "alice", Password: m.alicePw, Code: m.ownerRec[0]},
	} {
		a.Addr = addr1
		if _, err := m.svc.LoginUser(ctx, a); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v, want ErrInvalid", a, err)
		}
	}
}

// An unknown user, and a user with no password yet, get the answer a wrong password gets.
func TestUnknownUserIsAWrongPassword(t *testing.T) {
	m := newMulti(t)
	if _, err := m.dir.AddUser(ctx, "nopass", "default", RoleViewer, t0); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mallory", "nopass"} {
		_, err := m.svc.LoginUser(ctx, Attempt{User: name, Password: "anything at all", Code: "123456", Addr: addr1})
		if !errors.Is(err, ErrInvalid) || err.Error() != "wrong password or code" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Failures lock the user they named, not every user: alice locked out does not lock the
// owner out from another address.
func TestLockoutsArePerUser(t *testing.T) {
	m := newMulti(t)
	other := netip.MustParseAddr("198.51.100.9")
	var locked *LockedError
	for i := 0; i < 20 && !errors.As(m.svc.Login(ctx, Attempt{User: "alice", Password: "wrong one", Code: "000000", Addr: netip.AddrFrom4([4]byte{192, 0, 2, byte(i)})}), &locked); i++ {
	}
	if locked == nil {
		t.Fatal("alice was never locked")
	}
	if err := m.svc.Login(ctx, Attempt{User: "alice", Password: m.alicePw, Code: m.code(t, m.alice.ID), Addr: other}); !errors.As(err, &locked) {
		t.Fatalf("locked alice with the right password: %v", err)
	}
	if err := m.svc.Login(ctx, Attempt{Password: m.ownerPw, Code: m.code(t, OwnerID), Addr: other}); err != nil {
		t.Fatalf("the owner, while alice is locked: %v", err)
	}
}

// Device tokens are their user's: a token names its user, each lists only their own,
// and signing out everywhere ends one user's sessions and tokens only. An operator can
// sign another user out.
func TestDevicesAndSignOutArePerUser(t *testing.T) {
	m := newMulti(t)
	alice := m.svc.ForUser(m.alice)
	owner := m.svc.ForUser(User{ID: OwnerID, Name: "owner"})
	aTok, _, err := alice.IssueDevice(ctx, "alice-laptop")
	if err != nil {
		t.Fatal(err)
	}
	oTok, _, _ := owner.IssueDevice(ctx, "owner-laptop")

	d, err := m.svc.CheckDevice(ctx, aTok)
	if err != nil || d.UserID != m.alice.ID || d.Name != "alice-laptop" {
		t.Fatalf("alice's token: %+v %v", d, err)
	}
	if d, err := m.svc.CheckDevice(ctx, oTok); err != nil || d.UserID != OwnerID {
		t.Fatalf("owner's token: %+v %v", d, err)
	}
	if ds, _ := alice.Devices(ctx); len(ds) != 1 || ds[0].Name != "alice-laptop" {
		t.Fatalf("alice lists %+v", ds)
	}
	od, _ := owner.Devices(ctx)
	if ok, _ := alice.RevokeDevice(ctx, od[0].ID); ok {
		t.Fatal("alice revoked the owner's device")
	}

	m.end.users = nil // the rig's two SetPassword calls ended sessions too
	sessions, devices, err := alice.SignOutEverywhere(ctx)
	if err != nil || sessions != 1 || devices != 1 || m.end.all != 0 || len(m.end.users) != 1 || m.end.users[0] != m.alice.ID {
		t.Fatalf("alice signed out: %d %d %v, ender %+v", sessions, devices, err, m.end)
	}
	if _, err := m.svc.CheckDevice(ctx, aTok); !errors.Is(err, ErrLoginAgain) {
		t.Fatalf("alice's token after her sign-out: %v", err)
	}
	if _, err := m.svc.CheckDevice(ctx, oTok); err != nil {
		t.Fatalf("the owner's token after alice's sign-out: %v", err)
	}

	// A removed user's token says log in again.
	bTok, _, _ := alice.IssueDevice(ctx, "again")
	m.dir.RemoveUser(ctx, m.alice.ID)
	if _, err := m.svc.CheckDevice(ctx, bTok); !errors.Is(err, ErrLoginAgain) {
		t.Fatalf("a removed user's token: %v", err)
	}
}

// A new password for one user signs only that user out and leaves the others' locks.
func TestOneUsersPasswordChangeIsTheirsAlone(t *testing.T) {
	m := newMulti(t)
	m.end.users = nil
	if _, err := m.svc.ForUser(m.alice).SetPassword(ctx, "a new password for alice"); err != nil {
		t.Fatal(err)
	}
	if len(m.end.users) != 1 || m.end.users[0] != m.alice.ID || m.end.all != 0 {
		t.Fatalf("ended %+v", m.end)
	}
	if err := m.svc.Login(ctx, Attempt{Password: m.ownerPw, Code: m.code(t, OwnerID), Addr: addr1}); err != nil {
		t.Fatalf("owner after alice's change: %v", err)
	}
}

func TestUserNamesAndRoles(t *testing.T) {
	for _, ok := range []string{"alice", "a.b-c_d", "7up"} {
		if err := CheckUserName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-lead", "has space", "slash/es", "é", string(make([]byte, 65))} {
		if CheckUserName(bad) == nil {
			t.Errorf("%q was taken", bad)
		}
	}
	if r, err := ParseRole(" Admin "); err != nil || r != RoleAdmin {
		t.Errorf("Admin: %v %v", r, err)
	}
	if _, err := ParseRole("root"); err == nil {
		t.Error("root is a role")
	}
}
