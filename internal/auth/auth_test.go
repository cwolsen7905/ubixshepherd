package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	passwordCost = argonParams{Time: 1, Memory: 64, Threads: 1}
	codeCost = passwordCost
	os.Exit(m.Run())
}

var (
	ctx   = context.Background()
	t0    = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	addr1 = netip.MustParseAddr("203.0.113.7")
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type ender struct{ n int }

func (e *ender) EndAllSessions(context.Context) (int, error) { e.n++; return 3, nil }

type rig struct {
	svc   *Service
	st    *MemStore
	clk   *clock
	key   *Key
	end   *ender
	pw    string
	enr   Enrolment
	rec   []string
	locks int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	key, err := LoadKey("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{st: NewMemStore(), clk: &clock{t0}, key: key, end: &ender{}, pw: "correct horse battery"}
	f := &TOTP{Store: r.st, Key: key, Now: r.clk.now}
	r.svc = &Service{Store: r.st, Factor: f, Limiter: NewLimiter(Limits{}), Sessions: r.end, Now: r.clk.now,
		OnLock: func(netip.Addr, time.Time) { r.locks++ }}
	setup, err := r.svc.SetPassword(ctx, r.pw)
	if err != nil {
		t.Fatal(err)
	}
	r.enr, r.rec = setup.Enrolment, setup.RecoveryCodes
	return r
}

// code is the authenticator's code for the account at the rig's time.
func (r *rig) code(t *testing.T, at time.Time) string {
	t.Helper()
	fs, _ := r.st.Factors(ctx)
	secret, err := r.key.Open(fs[0].Secret, fs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(secret, stepOf(at))
}

func (r *rig) login(pw, code string, a netip.Addr) error {
	return r.svc.Login(ctx, Attempt{Password: pw, Code: code, Addr: a})
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("a long enough password")
	if err != nil || !strings.HasPrefix(h, "$argon2id$") || strings.Contains(h, "long enough") {
		t.Fatalf("%q %v", h, err)
	}
	if !CheckPassword(h, "a long enough password") || CheckPassword(h, "a long enough passworD") || CheckPassword("junk", "x") {
		t.Error("check")
	}
	h2, _ := HashPassword("a long enough password")
	if h == h2 {
		t.Error("two hashes of one password are equal: no salt")
	}
	if _, err := (&Service{Store: NewMemStore()}).SetPassword(ctx, "short"); err == nil {
		t.Error("a short password was accepted")
	}
}

func TestTOTPMatchesRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924"} {
		if got := totpCode(secret, ts/30); got != want {
			t.Errorf("t=%d: %s, want %s", ts, got, want)
		}
	}
}

func TestLoginWithPasswordAndTOTP(t *testing.T) {
	r := newRig(t)
	if err := r.login(r.pw, r.code(t, t0), addr1); err != nil {
		t.Fatalf("a right login failed: %v", err)
	}
	if !strings.HasPrefix(r.enr.URI, "otpauth://totp/") || r.enr.Secret == "" || len(r.rec) != RecoveryCodeCount {
		t.Errorf("setup: %+v %v", r.enr, r.rec)
	}
}

func TestWrongPasswordOrCodeRefused(t *testing.T) {
	r := newRig(t)
	good := r.code(t, t0)
	for name, c := range map[string][2]string{
		"wrong code":     {r.pw, "000000"},
		"wrong password": {"not the password!!", good},
		"no code":        {r.pw, ""},
		"junk code":      {r.pw, "zzzzzzzzzzzzzzzzz"},
	} {
		if err := r.login(c[0], c[1], addr1); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestReusedTOTPCodeRefused(t *testing.T) {
	r := newRig(t)
	c := r.code(t, t0)
	if err := r.login(r.pw, c, addr1); err != nil {
		t.Fatal(err)
	}
	if err := r.login(r.pw, c, addr1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a code was used twice: %v", err)
	}
	// An older code in the skew window is not a way round it either.
	if err := r.login(r.pw, r.code(t, t0.Add(-30*time.Second)), addr1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an earlier step after a later one: %v", err)
	}
	// The next step works.
	r.clk.t = t0.Add(30 * time.Second)
	if err := r.login(r.pw, r.code(t, r.clk.t), addr1); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryCodeWorksOnce(t *testing.T) {
	r := newRig(t)
	rc := r.rec[3]
	if err := r.login(r.pw, strings.ToUpper(rc), addr1); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if err := r.login(r.pw, rc, addr1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a recovery code worked twice: %v", err)
	}
	if err := r.login(r.pw, r.rec[4], addr1); err != nil {
		t.Fatalf("another code: %v", err)
	}
	if err := r.login("wrong password here", r.rec[5], addr1); !errors.Is(err, ErrInvalid) {
		t.Fatal("a recovery code replaced the password")
	}
	rows, _ := r.st.RecoveryCodes(ctx)
	for _, row := range rows {
		if strings.Contains(row.Hash, strings.ReplaceAll(rc, "-", "")) || !strings.HasPrefix(row.Hash, "$argon2id$") {
			t.Errorf("recovery code kept in the clear: %q", row.Hash)
		}
	}
}

func TestBurstPerAccountAndPerAddress(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 5; i++ {
		r.login(r.pw, "000000", netip.MustParseAddr(fmt.Sprintf("198.51.100.%d", i+1)))
	}
	if r.locks != 1 {
		t.Fatalf("account lock filed %d feed items, want 1", r.locks)
	}
	// Locked even for the right password and code, from a fresh address.
	var le *LockedError
	if err := r.login(r.pw, r.code(t, t0), netip.MustParseAddr("192.0.2.1")); !errors.As(err, &le) {
		t.Fatalf("account not locked after a burst: %v", err)
	}
	r.login(r.pw, "000000", addr1) // locked: not counted again
	if r.locks != 1 {
		t.Errorf("a refused try filed another item: %d", r.locks)
	}
	r.clk.t = t0.Add(16 * time.Minute)
	if err := r.login(r.pw, r.code(t, r.clk.t), addr1); err != nil {
		t.Fatalf("the lock never lifted: %v", err)
	}

	// One address: failures spread over wrong passwords never reach the account limit
	// because the success resets it, but the address still locks.
	r2 := newRig(t)
	bad := netip.MustParseAddr("203.0.113.99")
	for i := 0; i < 10; i++ {
		r2.login(r2.pw, "000000", bad)
		if i%3 == 2 {
			r2.svc.Limiter.Succeed(AccountName)
		}
	}
	if err := r2.login(r2.pw, r2.code(t, t0), bad); !errors.As(err, &le) {
		t.Fatalf("address not locked: %v", err)
	}
	if err := r2.login(r2.pw, r2.code(t, t0), addr1); err != nil {
		t.Fatalf("another address was locked with it: %v", err)
	}
}

func TestClientAddrIgnoresUntrustedForwarding(t *testing.T) {
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"direct, header ignored", "203.0.113.7:5000", []string{"1.2.3.4"}, "203.0.113.7"},
		{"proxy, one hop", "10.0.0.5:4000", []string{"198.51.100.2"}, "198.51.100.2"},
		{"proxy, client forged a first hop", "10.0.0.5:4000", []string{"9.9.9.9, 198.51.100.2"}, "198.51.100.2"},
		{"two proxies", "10.0.0.5:4000", []string{"198.51.100.2", "10.0.0.9"}, "198.51.100.2"},
		{"proxy, no header", "10.0.0.5:4000", nil, "10.0.0.5"},
		{"proxy, malformed", "10.0.0.5:4000", []string{"nonsense"}, "10.0.0.5"},
		{"v4-mapped connection", "[::ffff:10.0.0.5]:4000", []string{"198.51.100.2"}, "198.51.100.2"},
	}
	for _, c := range cases {
		if got := ClientAddr(c.remote, c.xff, proxy); got.String() != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// A forged header cannot dodge the limit: the same connection counts the same.
	if a, b := ClientAddr("203.0.113.7:1", []string{"1.1.1.1"}, proxy), ClientAddr("203.0.113.7:2", []string{"2.2.2.2"}, proxy); a != b {
		t.Error("untrusted header changed the counted address")
	}
}

func TestDeviceTokenSlidingAndCap(t *testing.T) {
	r := newRig(t)
	tok, d, err := r.svc.IssueDevice(ctx, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "shd_") || d.Expires != t0.Add(DeviceIdle) {
		t.Fatalf("%q %+v", tok, d)
	}
	rows, _ := r.st.Devices(ctx)
	if strings.Contains(fmt.Sprint(rows), tok) || rows[0].Hash != HashToken(tok) {
		t.Error("the token is kept, not its hash")
	}

	// Used every day: still good at day 89, refused at day 90.
	for day := 1; day <= 89; day++ {
		r.clk.t = t0.Add(time.Duration(day) * 24 * time.Hour)
		got, err := r.svc.CheckDevice(ctx, tok)
		if err != nil {
			t.Fatalf("day %d: %v", day, err)
		}
		if want := minTime(r.clk.t.Add(DeviceIdle), t0.Add(DeviceCap)); !got.Expires.Equal(want) {
			t.Fatalf("day %d: expires %v, want %v", day, got.Expires, want)
		}
	}
	r.clk.t = t0.Add(DeviceCap)
	if _, err := r.svc.CheckDevice(ctx, tok); !errors.Is(err, ErrLoginAgain) {
		t.Fatalf("a token used daily outlived its 90 days: %v", err)
	}
	if rows, _ := r.st.Devices(ctx); len(rows) != 0 {
		t.Error("the expired token's row stays")
	}

	// Left unused: stops after 30 days, even though it is far from 90.
	tok2, _, _ := r.svc.IssueDevice(ctx, "idle")
	r.clk.t = r.clk.t.Add(DeviceIdle - time.Second)
	if _, err := r.svc.CheckDevice(ctx, tok2); err != nil {
		t.Fatalf("29 days 23h: %v", err)
	}
	r.clk.t = r.clk.t.Add(DeviceIdle + time.Second)
	if _, err := r.svc.CheckDevice(ctx, tok2); !errors.Is(err, ErrLoginAgain) {
		t.Fatalf("an unused token outlived 30 days: %v", err)
	}
	if _, err := r.svc.CheckDevice(ctx, "shd_unknown"); !errors.Is(err, ErrLoginAgain) {
		t.Error("an unknown token got in")
	}
	if _, err := r.svc.CheckDevice(ctx, ""); !errors.Is(err, ErrLoginAgain) {
		t.Error("an empty token got in")
	}
	if ErrLoginAgain.Error() != "log in again" {
		t.Errorf("the message names more than it should: %q", ErrLoginAgain)
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func TestRevokeAndSignOutEverywhere(t *testing.T) {
	r := newRig(t)
	t1, d1, _ := r.svc.IssueDevice(ctx, "one")
	t2, _, _ := r.svc.IssueDevice(ctx, "two")
	if ok, _ := r.svc.RevokeDevice(ctx, d1.ID); !ok {
		t.Fatal("revoke")
	}
	if _, err := r.svc.CheckDevice(ctx, t1); !errors.Is(err, ErrLoginAgain) {
		t.Error("a revoked token works")
	}
	if _, err := r.svc.CheckDevice(ctx, t2); err != nil {
		t.Error(err)
	}
	ds, _ := r.svc.Devices(ctx)
	if len(ds) != 1 || ds[0].Hash != "" {
		t.Errorf("device list: %+v", ds)
	}
	r.end.n = 0
	s, d, err := r.svc.SignOutEverywhere(ctx)
	if err != nil || s != 3 || d != 1 || r.end.n != 1 {
		t.Fatalf("%d %d %v", s, d, err)
	}
	if _, err := r.svc.CheckDevice(ctx, t2); !errors.Is(err, ErrLoginAgain) {
		t.Error("a token survived sign out everywhere")
	}
}

func TestChangingPasswordOrFactorSignsEverythingOut(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.svc.IssueDevice(ctx, "one")
	oldCode := r.code(t, t0)
	r.end.n = 0
	setup, err := r.svc.SetPassword(ctx, "a different password!")
	if err != nil || r.end.n != 1 {
		t.Fatalf("%v %d", err, r.end.n)
	}
	if _, err := r.svc.CheckDevice(ctx, tok); !errors.Is(err, ErrLoginAgain) {
		t.Error("device token survived a password change")
	}
	if err := r.login(r.pw, oldCode, addr1); !errors.Is(err, ErrInvalid) {
		t.Error("old password still works")
	}
	if fs, _ := r.svc.Factor.List(ctx); len(fs) != 1 || fs[0].ID != setup.Enrolment.FactorID {
		t.Errorf("old factor stays: %+v", fs)
	}
	tok2, _, _ := r.svc.IssueDevice(ctx, "two")
	if _, err := r.svc.ResetSecondFactor(ctx); err != nil || r.end.n != 2 {
		t.Fatalf("%v %d", err, r.end.n)
	}
	if _, err := r.svc.CheckDevice(ctx, tok2); !errors.Is(err, ErrLoginAgain) {
		t.Error("device token survived a second factor change")
	}
}

func TestSecretNotReadableWithoutKey(t *testing.T) {
	r := newRig(t)
	fs, _ := r.st.Factors(ctx)
	secret, _ := r.key.Open(fs[0].Secret, fs[0].ID)
	if bytes.Contains(fs[0].Secret, secret) || strings.Contains(fmt.Sprintf("%x", fs[0].Secret), fmt.Sprintf("%x", secret)) {
		t.Fatal("the TOTP secret sits in the store in the clear")
	}
	other, err := LoadKey("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(fs[0].Secret, fs[0].ID); err == nil {
		t.Error("another key opened the secret")
	}
	if _, err := r.key.Open(fs[0].Secret, "another-row"); err == nil {
		t.Error("the secret opened under another row's id")
	}
	// A TOTP built on the wrong key cannot verify, and reports it as an error, not a pass.
	bad := &TOTP{Store: r.st, Key: other, Now: r.clk.now}
	if ok, err := bad.Verify(ctx, r.code(t, t0), t0); ok || err == nil {
		t.Errorf("verify without the key: %v %v", ok, err)
	}
}

func TestKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes")
	}
	dir := t.TempDir()
	write := func(name, body string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		os.Chmod(p, mode)
		return p
	}
	good := write("good.key", "0123456789abcdef0123456789abcdef\n", 0o600)
	k, err := LoadKey(good, t.TempDir())
	if err != nil || k.Fallback {
		t.Fatalf("a configured key file: %v %+v", err, k)
	}
	same, _ := LoadKey(good, t.TempDir())
	sealed, _ := k.Seal([]byte("x"), "a")
	if _, err := same.Open(sealed, "a"); err != nil {
		t.Error("the same file gave a different key")
	}
	for name, p := range map[string]string{
		"missing":        filepath.Join(dir, "absent.key"),
		"group readable": write("g.key", "0123456789abcdef0123456789abcdef", 0o640),
		"world readable": write("w.key", "0123456789abcdef0123456789abcdef", 0o604),
		"too short":      write("s.key", "short", 0o600),
		"a directory":    dir,
	} {
		home := t.TempDir()
		if _, err := LoadKey(p, home); err == nil {
			t.Errorf("%s: the start would go on", name)
		}
		if _, err := os.Stat(filepath.Join(home, KeyFileName)); err == nil {
			t.Errorf("%s: fell back to totp.key", name)
		}
	}
}

func TestKeyFallbackIsCreated0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes")
	}
	home := filepath.Join(t.TempDir(), "home")
	k, err := LoadKey("", home)
	if err != nil || !k.Fallback {
		t.Fatalf("%v %+v", err, k)
	}
	fi, err := os.Stat(filepath.Join(home, KeyFileName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("totp.key: %v %v", err, fi)
	}
	again, err := LoadKey("", home)
	if err != nil || !again.Fallback {
		t.Fatal(err)
	}
	sealed, _ := k.Seal([]byte("x"), "a")
	if _, err := again.Open(sealed, "a"); err != nil {
		t.Error("the fallback key changed between starts")
	}
	os.Chmod(filepath.Join(home, KeyFileName), 0o644)
	if _, err := LoadKey("", home); err == nil {
		t.Error("a too-open totp.key was used")
	}
	if !strings.Contains(KeyFallbackWarning, "beside the store") {
		t.Error("the warning does not say where the key sits")
	}
}

// TestNothingSecretIsLoggedOrReturned drives every path, including the failures, and
// checks that no secret reaches an error string or a Service's output other than the
// one-time setup value.
func TestNothingSecretIsLoggedOrReturned(t *testing.T) {
	r := newRig(t)
	secret, _ := func() ([]byte, error) { fs, _ := r.st.Factors(ctx); return r.key.Open(fs[0].Secret, fs[0].ID) }()
	acct, _ := r.st.Account(ctx)
	rows, _ := r.st.RecoveryCodes(ctx)
	tok, _, _ := r.svc.IssueDevice(ctx, "laptop")
	secrets := []string{r.pw, b32.EncodeToString(secret), acct.PasswordHash, tok, HashToken(tok)}
	for _, c := range r.rec {
		secrets = append(secrets, c, strings.ReplaceAll(c, "-", ""))
	}
	for _, row := range rows {
		secrets = append(secrets, row.Hash)
	}
	var out strings.Builder
	note := func(v any) { fmt.Fprint(&out, v, "\n") }
	note(r.login(r.pw, "000000", addr1))
	note(r.login("wrong password again", r.code(t, t0), addr1))
	for i := 0; i < 6; i++ {
		note(r.login(r.pw, "000000", addr1))
	}
	_, err := r.svc.CheckDevice(ctx, tok+"x")
	note(err)
	_, err = r.svc.CheckDevice(ctx, "shd_"+r.pw)
	note(err)
	ds, _ := r.svc.Devices(ctx)
	note(fmt.Sprintf("%+v", ds))
	fi, _ := r.svc.Factor.List(ctx)
	note(fmt.Sprintf("%+v", fi))
	note(&LockedError{Until: t0})
	for _, s := range secrets {
		if strings.Contains(out.String(), s) {
			t.Errorf("a secret reached output: %q", s)
		}
	}
}
