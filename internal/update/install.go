package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// TrustedPrefix is where a release's files may be fetched from: the project's own
// GitHub release downloads, whatever the releases API said.
const TrustedPrefix = "https://github.com/ubixsys/ubixshepherd/releases/download/"

// maxArchive bounds one download; maxBinary bounds the binary taken out of it.
const (
	maxArchive = 200 << 20
	maxBinary  = 400 << 20
	maxSums    = 1 << 20
)

// Method is how the running binary was installed, which says whether Shepherd may replace
// it and what the person runs instead when it may not.
type Method string

const (
	// MethodBinary is a release binary in a directory the daemon can write: install.sh or
	// an archive unpacked by hand. Shepherd replaces it in place.
	MethodBinary Method = "binary"
	// MethodBrew is a Homebrew install; brew owns the file.
	MethodBrew Method = "brew"
	// MethodGo is `go install`; the Go tool owns the file.
	MethodGo Method = "go"
	// MethodSource is a dev build or a binary built from a checkout (make install).
	MethodSource Method = "source"
	// MethodReadOnly is a release binary in a directory the daemon cannot write.
	MethodReadOnly Method = "readonly"
)

// Detect says how the binary at exe was installed. known is whether the running build
// carries a release version; a build with none is a source build whatever its path.
func Detect(exe string, known bool) Method {
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	slash := filepath.ToSlash(exe)
	switch {
	case strings.Contains(slash, "/Cellar/") || strings.Contains(slash, "/linuxbrew/") || strings.Contains(slash, "/Homebrew/"):
		return MethodBrew
	case !known:
		return MethodSource
	case isGoBin(slash):
		return MethodGo
	}
	if !dirWritable(filepath.Dir(exe)) {
		return MethodReadOnly
	}
	return MethodBinary
}

// isGoBin says exe sits in the Go tool's install directory.
func isGoBin(slash string) bool {
	var bins []string
	if v := os.Getenv("GOBIN"); v != "" {
		bins = append(bins, filepath.ToSlash(v))
	}
	if v := os.Getenv("GOPATH"); v != "" {
		for _, p := range filepath.SplitList(v) {
			bins = append(bins, filepath.ToSlash(filepath.Join(p, "bin")))
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		bins = append(bins, filepath.ToSlash(filepath.Join(home, "go", "bin")))
	}
	for _, b := range bins {
		if real, err := filepath.EvalSymlinks(filepath.FromSlash(b)); err == nil {
			b = filepath.ToSlash(real)
		}
		if path.Dir(slash) == path.Clean(b) {
			return true
		}
	}
	return false
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".shepherd-write-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// Hint is what the person does when Shepherd will not replace the binary itself ("" for
// MethodBinary, which it will).
func (m Method) Hint(latest string) string {
	switch m {
	case MethodBrew:
		return "installed with Homebrew: run `brew upgrade shepherd`, then `shepherd daemon restart`"
	case MethodGo:
		return "installed with go install: run `go install github.com/ubixsys/ubixshepherd/cmd/shepherd@" + latest + "`, then `shepherd daemon restart`"
	case MethodSource:
		return "a source or development build: update the checkout and run `make install`"
	case MethodReadOnly:
		return "the directory holding shepherd is not writable by this user: install the release there yourself, then run `shepherd daemon restart`"
	}
	return ""
}

// Installer fetches a release and replaces the running binary with it.
type Installer struct {
	// Exe is the binary to replace.
	Exe    string
	Client *http.Client
	// Trusted is the URL prefix downloads must start with; "" is TrustedPrefix. Tests
	// point it at a fake server.
	Trusted string
	// GOOS and GOARCH pick the archive; "" is this build's.
	GOOS, GOARCH string
	// Check proves the new binary runs and reports version; "" skips it. Tests that use
	// a fake binary replace it.
	Check func(ctx context.Context, bin, version string) error
}

func (in *Installer) trusted() string {
	if in.Trusted != "" {
		return in.Trusted
	}
	return TrustedPrefix
}

func (in *Installer) goos() string {
	if in.GOOS != "" {
		return in.GOOS
	}
	return runtime.GOOS
}

func (in *Installer) goarch() string {
	if in.GOARCH != "" {
		return in.GOARCH
	}
	return runtime.GOARCH
}

// ArchiveName is the release archive for a platform: shepherd-<tag>-<os>-<arch>.tar.gz,
// or .zip for Windows (Makefile `dist`).
func ArchiveName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("shepherd-%s-%s-%s%s", tag, goos, goarch, ext)
}

// PreviousPath is where the replaced binary is kept, for a rollback.
func PreviousPath(exe string) string { return exe + ".previous" }

// Install downloads rel's archive for this platform, checks it against the release's
// SHA256SUMS, proves the binary inside runs, and swaps it in for in.Exe. The old binary
// is kept at PreviousPath. Any failure before the swap leaves in.Exe as it was.
func (in *Installer) Install(ctx context.Context, rel Release) error {
	name := ArchiveName(rel.Version, in.goos(), in.goarch())
	var archive, sums *Asset
	for i := range rel.Assets {
		switch rel.Assets[i].Name {
		case name:
			archive = &rel.Assets[i]
		case "SHA256SUMS":
			sums = &rel.Assets[i]
		}
	}
	if archive == nil {
		return fmt.Errorf("release %s has no %s for this platform", rel.Version, name)
	}
	if sums == nil {
		return fmt.Errorf("release %s publishes no SHA256SUMS, so the download cannot be verified", rel.Version)
	}
	for _, a := range []*Asset{archive, sums} {
		if !strings.HasPrefix(a.URL, in.trusted()) {
			return fmt.Errorf("refusing to download %s: not from the project's releases", a.Name)
		}
	}
	sumFile, err := in.get(ctx, sums.URL, maxSums)
	if err != nil {
		return fmt.Errorf("fetch SHA256SUMS: %w", err)
	}
	want, err := sumFor(sumFile, name)
	if err != nil {
		return err
	}
	data, err := in.get(ctx, archive.URL, maxArchive)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s does not match SHA256SUMS: not installing it", name)
	}
	bin, err := extract(data, name)
	if err != nil {
		return err
	}
	dir := filepath.Dir(in.Exe)
	tmp, err := os.CreateTemp(dir, ".shepherd-new-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", in.Exe, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if in.Check != nil {
		if err := in.Check(ctx, tmp.Name(), rel.Version); err != nil {
			return fmt.Errorf("the downloaded binary failed its check: %w", err)
		}
	}
	return in.swap(tmp.Name())
}

// swap puts the new binary at Exe and keeps the old one at PreviousPath.
func (in *Installer) swap(newBin string) error {
	prev := PreviousPath(in.Exe)
	if in.goos() == "windows" {
		// A running .exe cannot be overwritten, but it can be renamed.
		os.Remove(prev)
		if err := os.Rename(in.Exe, prev); err != nil {
			return err
		}
		if err := os.Rename(newBin, in.Exe); err != nil {
			os.Rename(prev, in.Exe)
			return err
		}
		return nil
	}
	if err := copyFile(in.Exe, prev); err != nil {
		return fmt.Errorf("keep the old binary: %w", err)
	}
	if err := os.Rename(newBin, in.Exe); err != nil {
		os.Remove(prev)
		return err
	}
	return nil
}

// Restore puts the previous binary back at exe. It is the rollback.
func Restore(exe string) error {
	prev := PreviousPath(exe)
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("no previous binary to restore: %w", err)
	}
	if runtime.GOOS == "windows" {
		failed := exe + ".failed"
		os.Remove(failed)
		if err := os.Rename(exe, failed); err != nil {
			return err
		}
		if err := os.Rename(prev, exe); err != nil {
			os.Rename(failed, exe)
			return err
		}
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".shepherd-restore-*")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := copyFile(prev, tmp.Name()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(to, 0o755)
}

func (in *Installer) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	hc := in.Client
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("larger than expected")
	}
	return b, nil
}

// sumFor finds name's checksum in a SHA256SUMS file ("<hex>  <name>" per line).
func sumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS has no entry for %s", name)
}

// extract takes the shepherd binary out of a release archive, wherever in it it sits.
func extract(data []byte, archive string) ([]byte, error) {
	want := "shepherd"
	if strings.HasSuffix(archive, ".zip") {
		want = "shepherd.exe"
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", archive, err)
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || path.Base(f.Name) != want {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return readBinary(rc)
		}
		return nil, fmt.Errorf("%s holds no %s", archive, want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", archive, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s holds no %s", archive, want)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == want {
			return readBinary(tr)
		}
	}
}

func readBinary(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBinary+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBinary || len(b) == 0 {
		return nil, errors.New("the binary in the archive has an unexpected size")
	}
	return b, nil
}

// RunVersion is an Installer.Check that runs `bin version` and wants the tag in its
// output (`shepherd v0.5.0`).
func RunVersion(ctx context.Context, bin, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("`version` failed: %w", err)
	}
	if !strings.Contains(string(out), strings.TrimPrefix(version, "v")) {
		return fmt.Errorf("it reports %q, not %s", strings.TrimSpace(string(out)), version)
	}
	return nil
}
