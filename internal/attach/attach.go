// Package attach stores the files a person pastes or drops into the web chat and says how
// the desk is told of them. Files are kept by the hash of their bytes, so one file sent
// twice is stored once; what a file is called, and which turn it belongs to, lives in the
// store.
package attach

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Limits on what one message may carry.
const (
	MaxFileBytes  = 10 << 20
	MaxTurnFiles  = 5
	MaxTurnBytes  = 25 << 20
	InlineMax     = 16 << 10 // a text file up to this size is also written into the turn
	maxNameLength = 120
)

// Kinds a file is sorted into.
const (
	KindImage = "image"
	KindPDF   = "pdf"
	KindText  = "text"
	KindMedia = "media"
)

// ErrRefused is wrapped by every refusal of a file's type or size.
var ErrRefused = errors.New("file refused")

var blockedExt = map[string]bool{
	".exe": true, ".dll": true, ".msi": true, ".app": true, ".bat": true, ".cmd": true, ".com": true,
	".scr": true, ".sh": true, ".bash": true, ".zsh": true, ".ps1": true, ".vbs": true, ".jar": true,
	".dmg": true, ".pkg": true, ".so": true, ".dylib": true, ".apk": true, ".deb": true, ".rpm": true,
	".zip": true, ".tar": true, ".gz": true, ".tgz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true,
}

// Info is what is known of a file once it is classified.
type Info struct {
	Name string
	Mime string
	Kind string
	Size int64
}

// SafeName reduces a client's file name to a plain base name that is safe to show and to
// put in a header or a line of text.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '"' || r == '[' || r == ']' || r == '<' || r == '>' {
			r = '_'
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	if r := []rune(name); len(r) > maxNameLength {
		name = string(r[:maxNameLength])
	}
	if name == "" || name == "." || name == ".." || name == "/" {
		return "file"
	}
	return name
}

// Classify decides what a file is from its first bytes and its name, never from the type
// the client claimed. head is the start of the content (at most 512 bytes are used).
func Classify(name string, head []byte, size int64) (Info, error) {
	name = SafeName(name)
	in := Info{Name: name, Size: size}
	refuse := func(why string) (Info, error) {
		return in, fmt.Errorf("%w: %s is %s", ErrRefused, name, why)
	}
	if size <= 0 {
		return refuse("empty")
	}
	if size > MaxFileBytes {
		return refuse(fmt.Sprintf("over the %d MiB limit", MaxFileBytes>>20))
	}
	ext := strings.ToLower(filepath.Ext(name))
	if blockedExt[ext] {
		return refuse("a program, script or archive, which the desk does not take")
	}
	switch {
	case bytes.HasPrefix(head, []byte("\x7fELF")), bytes.HasPrefix(head, []byte("MZ")),
		bytes.HasPrefix(head, []byte("#!")),
		bytes.HasPrefix(head, []byte{0xfe, 0xed, 0xfa, 0xce}), bytes.HasPrefix(head, []byte{0xfe, 0xed, 0xfa, 0xcf}),
		bytes.HasPrefix(head, []byte{0xce, 0xfa, 0xed, 0xfe}), bytes.HasPrefix(head, []byte{0xcf, 0xfa, 0xed, 0xfe}),
		bytes.HasPrefix(head, []byte{0xca, 0xfe, 0xba, 0xbe}):
		return refuse("an executable")
	case bytes.HasPrefix(head, []byte("PK\x03\x04")) && (ext == "" || ext == ".zip" || ext == ".jar"):
		return refuse("an archive")
	}
	if len(head) > 512 {
		head = head[:512]
	}
	sniffed := http.DetectContentType(head)
	switch {
	case sniffed == "image/png" || sniffed == "image/jpeg" || sniffed == "image/gif" || sniffed == "image/webp":
		in.Mime, in.Kind = sniffed, KindImage
	case bytes.HasPrefix(head, []byte("%PDF-")):
		in.Mime, in.Kind = "application/pdf", KindPDF
	case strings.HasPrefix(sniffed, "audio/"), strings.HasPrefix(sniffed, "video/"):
		in.Mime, in.Kind = sniffed, KindMedia
	case isText(head):
		in.Mime, in.Kind = "text/plain", KindText
		if t := mime.TypeByExtension(ext); strings.HasPrefix(t, "text/") {
			in.Mime = strings.SplitN(t, ";", 2)[0]
		}
	case ext == ".docx" || ext == ".xlsx" || ext == ".pptx" || ext == ".odt" || ext == ".ods" || ext == ".bmp" || ext == ".tiff" || ext == ".svg":
		in.Mime, in.Kind = "application/octet-stream", KindMedia
	default:
		return refuse("a type the desk cannot read (" + sniffed + ")")
	}
	return in, nil
}

func isText(b []byte) bool {
	if len(b) == 0 || bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	// A cut multi-byte character at the end of the sample is not binary.
	for i := 0; i < 4 && i < len(b); i++ {
		if utf8.Valid(b[:len(b)-i]) {
			return true
		}
	}
	return false
}

// Store keeps file bytes under Dir by SHA-256.
type Store struct{ Dir string }

// Path is where the bytes with hash sha are, or would be.
func (s Store) Path(sha string) string {
	if len(sha) < 3 {
		return filepath.Join(s.Dir, "invalid")
	}
	return filepath.Join(s.Dir, sha[:2], sha)
}

// Put reads r, at most MaxFileBytes of it, classifies it and stores it. It returns the
// file's hash and what it is.
func (s Store) Put(name string, r io.Reader) (string, Info, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", Info{}, err
	}
	tmp, err := os.CreateTemp(s.Dir, "upload-*")
	if err != nil {
		return "", Info{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, MaxFileBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", Info{}, err
	}
	head := make([]byte, 512)
	f, err := os.Open(tmp.Name())
	if err != nil {
		return "", Info{}, err
	}
	m, _ := io.ReadFull(f, head)
	f.Close()
	in, err := Classify(name, head[:m], n)
	if err != nil {
		return "", in, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	dst := s.Path(sha)
	if _, err := os.Stat(dst); err == nil {
		return sha, in, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", in, err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return "", in, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", in, err
	}
	return sha, in, nil
}

// Open opens the stored bytes.
func (s Store) Open(sha string) (*os.File, error) { return os.Open(s.Path(sha)) }

// Remove deletes the stored bytes; a missing file is not an error.
func (s Store) Remove(sha string) error {
	if err := os.Remove(s.Path(sha)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
