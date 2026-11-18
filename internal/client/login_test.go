package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCredentialsRoundTripWith0600File(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tokens")
	c := Credentials{URL: "https://shepherd.example.com", Token: "shd_secret", DeviceID: "d1", Name: "laptop", Expires: time.Now().Add(time.Hour).UTC()}
	if _, err := LoadCredentials(dir, c.URL); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("before save: %v", err)
	}
	if err := SaveCredentials(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCredentials(dir, c.URL)
	if err != nil || got.Token != c.Token || got.DeviceID != "d1" {
		t.Fatalf("%+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(CredentialsPath(dir, c.URL))
		di, _ := os.Stat(dir)
		if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
			t.Errorf("modes: file %v dir %v", fi.Mode().Perm(), di.Mode().Perm())
		}
	}
	// A second login replaces the first, and no temp file is left behind.
	c.Token = "shd_second"
	SaveCredentials(dir, c)
	ents, _ := os.ReadDir(dir)
	if got, _ := LoadCredentials(dir, c.URL); got.Token != "shd_second" || len(ents) != 1 {
		t.Errorf("replace: %+v, %d files", got, len(ents))
	}
	if err := DeleteCredentials(dir, c.URL); err != nil || DeleteCredentials(dir, c.URL) != nil {
		t.Error("delete")
	}
	if _, err := LoadCredentials(dir, c.URL); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("after delete: %v", err)
	}
}

func TestDamagedCredentialsSayLogInAgain(t *testing.T) {
	dir := t.TempDir()
	url := "https://shepherd.example.com"
	os.WriteFile(CredentialsPath(dir, url), []byte("{nope"), 0o600)
	if _, err := LoadCredentials(dir, url); err == nil || !strings.Contains(err.Error(), "log in again: shepherd login "+url) {
		t.Errorf("damaged: %v", err)
	}
}

func TestHTTPSClientMapsEvery401ToLogInAgainAndSendsTheToken(t *testing.T) {
	var auth string
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"anything at all"}`))
	}))
	defer ts.Close()
	c := NewHTTPS(ts.URL, "shd_tok", ts.Client())
	_, err := c.Status(context.Background())
	var again *LoginAgain
	if !errors.As(err, &again) || err.Error() != "log in again: shepherd login "+ts.URL {
		t.Fatalf("%v", err)
	}
	if auth != "Bearer shd_tok" || errors.Is(err, ErrNoDaemon) {
		t.Errorf("auth %q, ErrNoDaemon %v", auth, errors.Is(err, ErrNoDaemon))
	}
}

func TestHTTPSClientNeverFollowsARedirect(t *testing.T) {
	var hit bool
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer other.Close()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer ts.Close()
	hc := NewHTTPClient()
	hc.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true // test servers only
	if _, err := NewHTTPS(ts.URL, "t", hc).Status(context.Background()); err == nil || hit {
		t.Errorf("redirect: err %v, followed %v", err, hit)
	}
}

func TestLoginReportsTheDaemonsWordsAndNeverTheToken(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"wrong password or code"}`))
	}))
	defer ts.Close()
	_, err := Login(context.Background(), ts.URL, ts.Client(), LoginRequest{Password: "pw-secret", Code: "123456"})
	if err == nil || !strings.Contains(err.Error(), "wrong password or code") || strings.Contains(err.Error(), "pw-secret") {
		t.Errorf("%v", err)
	}
}
