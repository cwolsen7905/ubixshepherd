package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// ErrNotLoggedIn means no device token is kept for the host.
var ErrNotLoggedIn = errors.New("not logged in")

// ErrDamaged means the saved login exists but cannot be read as one.
var ErrDamaged = errors.New("the saved login is damaged")

// Credentials is what a login leaves in the client's home: the device token, with what
// is needed to show and revoke it. It never goes in hosts.yaml, output, logs or the
// environment.
type Credentials struct {
	URL        string    `json:"url"`
	Token      string    `json:"token"`
	DeviceID   string    `json:"device_id"`
	Name       string    `json:"name"`
	Expires    time.Time `json:"expires"`
	MaxExpires time.Time `json:"max_expires"`
}

// LoginRequest is what a person types: the password and a code (a TOTP code, or one
// recovery code), and the name the device token is listed under.
type LoginRequest struct {
	Password, Code, Name string
}

// CredentialsPath is the file for the host at url (normalised) in the tokens directory.
// The name is a hash of the address, so it is a safe file name for any host.
func CredentialsPath(dir, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".json")
}

// SaveCredentials writes the file with mode 0600 in a 0700 directory, whole or not at
// all: a temp file in the same directory, renamed into place.
func SaveCredentials(dir string, c Credentials) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700) // an older, laxer directory is tightened
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), CredentialsPath(dir, c.URL))
}

// LoadCredentials reads the device token kept for url, or ErrNotLoggedIn.
func LoadCredentials(dir, url string) (Credentials, error) {
	b, err := os.ReadFile(CredentialsPath(dir, url))
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrNotLoggedIn
	}
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil || c.Token == "" || c.URL != url {
		return Credentials{}, fmt.Errorf("%w for %s: log in again: shepherd login %s", ErrDamaged, url, url)
	}
	return c, nil
}

// DeleteCredentials removes the file for url; none is not an error.
func DeleteCredentials(dir, url string) error {
	if err := os.Remove(CredentialsPath(dir, url)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Login asks the hosted daemon at url (normalised) for a device token. hc nil is
// NewHTTPClient. The daemon answers every wrong password or code the same way, and a
// locked account with how long to wait; both come back as the daemon's words.
func Login(ctx context.Context, url string, hc *http.Client, r LoginRequest) (Credentials, error) {
	if hc == nil {
		hc = NewHTTPClient()
	}
	body, _ := json.Marshal(map[string]string{"password": r.Password, "code": r.Code, "name": r.Name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+api.PathDeviceLogin, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("cannot reach %s (%v): is the address right?", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		Error      string    `json:"error"`
		Token      string    `json:"token"`
		DeviceID   string    `json:"device_id"`
		DeviceName string    `json:"device_name"`
		Expires    time.Time `json:"expires"`
		MaxExpires time.Time `json:"max_expires"`
	}
	_ = json.Unmarshal(b, &out)
	if resp.StatusCode != http.StatusOK || out.Token == "" {
		if out.Error != "" {
			return Credentials{}, fmt.Errorf("login refused: %s", out.Error)
		}
		return Credentials{}, fmt.Errorf("login refused: %s", resp.Status)
	}
	return Credentials{URL: url, Token: out.Token, DeviceID: out.DeviceID, Name: out.DeviceName, Expires: out.Expires, MaxExpires: out.MaxExpires}, nil
}

// Revoke ends the device token on the daemon, so a logout does not leave a live token
// behind. It uses the token itself, which the daemon lets end its own row.
func Revoke(ctx context.Context, c Credentials, hc *http.Client) error {
	cl := NewHTTPS(c.URL, c.Token, hc)
	return cl.do(ctx, http.MethodPost, api.PathDevices+"/"+c.DeviceID+"/revoke", struct{}{}, &struct{}{})
}
