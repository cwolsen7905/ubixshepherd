package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// KeyFileName is the fallback key's file in the Shepherd home.
const KeyFileName = "totp.key"

// minKeyBytes is the least a configured key file may hold.
const minKeyBytes = 16

// Key encrypts the TOTP secret at rest. It lives in a file apart from the store, so a
// copy of the database alone does not hold the secret.
type Key struct {
	aead cipher.AEAD
	// Fallback is true when the key is the one kept beside the store (no key file was
	// configured): a reader of the whole home has both.
	Fallback bool
}

// KeyFallbackWarning is what the daemon logs at every start when the key sits beside the
// store, and puts in the feed the first time.
const KeyFallbackWarning = "the TOTP key is kept in the Shepherd home beside the store, so a copy of the whole home holds both; set daemon.public.totp_key_file to a file from your secret store to keep it apart"

// LoadKey picks the key as the design says. With file set, that file is the key, and a
// missing, unreadable, too-short or group- or world-accessible one is an error that
// stops the start. Otherwise totp.key in home is used, created with mode 0600 on first
// need, and Fallback is set so the caller can warn.
func LoadKey(file, home string) (*Key, error) {
	if file != "" {
		raw, err := readKeyFile(file)
		if err != nil {
			return nil, fmt.Errorf("daemon.public.totp_key_file: %w", err)
		}
		return newKey(raw, false)
	}
	path := filepath.Join(home, KeyFileName)
	raw, err := readKeyFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		_, werr := f.WriteString(hex.EncodeToString(buf) + "\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return nil, werr
		}
		raw = []byte(hex.EncodeToString(buf))
	} else if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return newKey(raw, true)
}

func readKeyFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	// Windows has no group and world bits to read.
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is mode %04o, readable by others; chmod 600 it", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) < minKeyBytes {
		return nil, fmt.Errorf("%s holds fewer than %d bytes", path, minKeyBytes)
	}
	return b, nil
}

func newKey(raw []byte, fallback bool) (*Key, error) {
	sum := sha256.Sum256(append([]byte("shepherd totp key v1\x00"), raw...))
	blk, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return &Key{aead: g, Fallback: fallback}, nil
}

// Seal encrypts plain; ad (the factor's id) is bound in, so a sealed secret cannot be
// moved to another row.
func (k *Key) Seal(plain []byte, ad string) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return k.aead.Seal(nonce, nonce, plain, []byte(ad)), nil
}

// Open decrypts what Seal made; a wrong key or a moved row is an error.
func (k *Key) Open(sealed []byte, ad string) ([]byte, error) {
	n := k.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("sealed secret is too short")
	}
	plain, err := k.aead.Open(nil, sealed[:n], sealed[n:], []byte(ad))
	if err != nil {
		return nil, errors.New("the TOTP secret does not open with this key")
	}
	return plain, nil
}
