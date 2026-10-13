package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// release serves a fake release's archive and SHA256SUMS and returns it, with its trusted
// prefix, for Installer tests. binary is what the archive holds; badSum corrupts the sum.
func release(t *testing.T, tag, binary string, badSum bool) (Release, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	dir := fmt.Sprintf("shepherd-%s-linux-amd64", tag)
	tw.WriteHeader(&tar.Header{Name: dir + "/README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: dir + "/shepherd", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
	tw.Write([]byte(binary))
	tw.Close()
	gz.Close()
	name := ArchiveName(tag, "linux", "amd64")
	sum := sha256.Sum256(buf.Bytes())
	hexsum := hex.EncodeToString(sum[:])
	if badSum {
		hexsum = strings.Repeat("0", 64)
	}
	sums := fmt.Sprintf("%s  %s\n%s  other.zip\n", hexsum, name, strings.Repeat("1", 64))
	mux := http.NewServeMux()
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) })
	mux.HandleFunc("/dl/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return Release{Version: tag, Assets: []Asset{
		{Name: name, URL: ts.URL + "/dl/" + name},
		{Name: "SHA256SUMS", URL: ts.URL + "/dl/SHA256SUMS"},
	}}, ts.URL + "/dl/"
}

func installer(t *testing.T, trusted string) (*Installer, string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "shepherd")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Installer{Exe: exe, Trusted: trusted, GOOS: "linux", GOARCH: "amd64"}, exe
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallReplacesAndKeepsPrevious(t *testing.T) {
	rel, trusted := release(t, "v0.5.0", "new binary", false)
	in, exe := installer(t, trusted)
	var checked string
	in.Check = func(_ context.Context, bin, v string) error { checked = read(t, bin); return nil }
	if err := in.Install(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
	if got := read(t, exe); got != "new binary" || checked != "new binary" {
		t.Errorf("exe %q, checked %q", got, checked)
	}
	if got := read(t, PreviousPath(exe)); got != "old" {
		t.Errorf("previous %q", got)
	}
	if fi, _ := os.Stat(exe); runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("not executable: %v", fi.Mode())
	}
	if ents, _ := os.ReadDir(filepath.Dir(exe)); len(ents) != 2 {
		t.Errorf("leftover files: %v", ents)
	}

	if err := Restore(exe); err != nil {
		t.Fatal(err)
	}
	if got := read(t, exe); got != "old" {
		t.Errorf("after restore %q", got)
	}
}

func TestInstallFailuresLeaveTheBinaryAlone(t *testing.T) {
	good, trusted := release(t, "v0.5.0", "new", false)
	bad, badTrusted := release(t, "v0.5.0", "new", true)
	cases := []struct {
		name    string
		rel     Release
		trusted string
		check   func(context.Context, string, string) error
		want    string
	}{
		{"checksum", bad, badTrusted, nil, "does not match SHA256SUMS"},
		{"untrusted", good, "https://github.com/ubixsys/ubixshepherd/releases/download/", nil, "not from the project's releases"},
		{"no archive", Release{Version: "v0.5.0", Assets: good.Assets[1:]}, trusted, nil, "has no shepherd-v0.5.0-linux-amd64.tar.gz"},
		{"no sums", Release{Version: "v0.5.0", Assets: good.Assets[:1]}, trusted, nil, "no SHA256SUMS"},
		{"check", good, trusted, func(context.Context, string, string) error { return fmt.Errorf("boom") }, "failed its check"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, exe := installer(t, c.trusted)
			in.Check = c.check
			err := in.Install(context.Background(), c.rel)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
			if read(t, exe) != "old" {
				t.Error("binary changed")
			}
			if _, err := os.Stat(PreviousPath(exe)); err == nil {
				t.Error("previous left behind")
			}
		})
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "shepherd")
	os.WriteFile(exe, nil, 0o755)
	if got := Detect(exe, true); got != MethodBinary {
		t.Errorf("release binary: %s", got)
	}
	if got := Detect(exe, false); got != MethodSource {
		t.Errorf("unknown version: %s", got)
	}
	brew := filepath.Join(dir, "Cellar", "shepherd", "1", "bin")
	os.MkdirAll(brew, 0o755)
	os.WriteFile(filepath.Join(brew, "shepherd"), nil, 0o755)
	if got := Detect(filepath.Join(brew, "shepherd"), true); got != MethodBrew {
		t.Errorf("brew: %s", got)
	}
	t.Setenv("GOBIN", dir)
	if got := Detect(exe, true); got != MethodGo {
		t.Errorf("go install: %s", got)
	}
	if MethodBrew.Hint("v1") == "" || MethodBinary.Hint("v1") != "" {
		t.Error("hints")
	}
}

func TestMarkerStartup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "upgrade.json")
	if rb, _ := Startup(p, "v2"); rb {
		t.Fatal("rollback without a marker")
	}
	WriteMarker(p, Marker{From: "v1", To: "v2"})
	if rb, _ := Startup(p, "v1"); rb {
		t.Error("the old binary rolled back")
	}
	if _, ok := ReadMarker(p); ok {
		t.Error("a stale marker stayed")
	}
	WriteMarker(p, Marker{From: "v1", To: "v2"})
	if rb, _ := Startup(p, "v2"); rb {
		t.Error("first launch rolled back")
	}
	if rb, m := Startup(p, "v2"); !rb || m.From != "v1" {
		t.Errorf("second launch: %v %+v", rb, m)
	}
}

func TestCheckerRemembersAFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "update.json")
	c := &Checker{StatePath: p, Settings: func() (bool, string) { return true, "beta" }}
	if err := c.MarkFailed("v2", "boom", true); err != nil {
		t.Fatal(err)
	}
	c2 := &Checker{StatePath: p, Settings: func() (bool, string) { return true, "beta" }}
	if st := c2.Status(); st.FailedVersion != "v2" || !st.RolledBack || st.FailedWhy != "boom" {
		t.Errorf("%+v", st)
	}
	if err := c2.Dismiss("v0.2.0"); err != nil {
		t.Fatal(err)
	}
	c2.ClearFailed()
	if st := c2.Status(); st.FailedVersion != "" || st.Dismissed {
		t.Errorf("dismissal is per latest release only; %+v", st)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "v0.2.0") {
		t.Error("clearing the failure lost the dismissal")
	}
}
