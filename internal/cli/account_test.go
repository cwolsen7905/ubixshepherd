package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func hostedEnv(t *testing.T, stdin string, hosted bool) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	auth.UseCheapHashingForTests()
	home := t.TempDir()
	st, err := sqlite.Open(context.Background(), filepath.Join(home, "shepherd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Default()
	if hosted {
		cfg.Daemon.Public = &config.Public{Listen: "0.0.0.0:8443", URL: "https://shepherd.example.com", TrustedProxies: []string{"127.0.0.1"}}
	}
	srv, err := daemon.NewServer(st, cfg, "cfg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.PreparePublic(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(srv.Close)
	l := paths.Layout{Home: home}
	rt, _ := json.Marshal(api.Runtime{Addr: strings.TrimPrefix(ts.URL, "http://"), Token: srv.Token})
	os.WriteFile(l.Runtime(), rt, 0o600)
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	return Env{Stdin: strings.NewReader(stdin), Stdout: out, Stderr: errb, Layout: l, Cwd: home}, out, errb
}

func TestAccountSetShowsEnrolmentAndTenCodesOnce(t *testing.T) {
	env, out, errb := hostedEnv(t, "correct horse battery\n", true)
	if err := runAccount(context.Background(), env, []string{"set", "--password-stdin"}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "otpauth://totp/") || strings.Count(s, "\n  ") < 12 {
		t.Errorf("output: %s", s)
	}
	if strings.Contains(s+errb.String(), "correct horse battery") {
		t.Error("the password was printed")
	}
	var codes int
	for _, line := range strings.Split(s, "\n") {
		if f := strings.TrimSpace(line); len(f) == 11 && f[5] == '-' {
			codes++
		}
	}
	if codes != 10 {
		t.Errorf("%d recovery codes shown, want 10", codes)
	}
	if err := runAccount(context.Background(), env, []string{"sign-out-all"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "device token") {
		t.Errorf("sign-out-all: %s", out.String())
	}
}

func TestAccountSetRefusals(t *testing.T) {
	env, _, _ := hostedEnv(t, "short\n", true)
	if err := runAccount(context.Background(), env, []string{"set", "--password-stdin"}); err == nil || !strings.Contains(err.Error(), "at least") {
		t.Errorf("a short password: %v", err)
	}
	env, _, _ = hostedEnv(t, "", true)
	if err := runAccount(context.Background(), env, []string{"set"}); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Errorf("no terminal and no --password-stdin: %v", err)
	}
	env, _, _ = hostedEnv(t, "correct horse battery\n", false)
	if err := runAccount(context.Background(), env, []string{"set", "--password-stdin"}); err == nil || !strings.Contains(err.Error(), "hosted mode is not on") {
		t.Errorf("without daemon.public: %v", err)
	}
	if err := runAccount(context.Background(), env, []string{"bogus"}); err == nil {
		t.Error("an unknown subcommand")
	}
	if err := runAccount(context.Background(), env, nil); err == nil {
		t.Error("no subcommand")
	}
	env.Remote = &remoteHost{}
	if err := runAccount(context.Background(), env, []string{"sign-out-all"}); err == nil || !strings.Contains(err.Error(), "own machine") {
		t.Errorf("over --host: %v", err)
	}
}
