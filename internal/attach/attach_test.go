package attach

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

var png = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 40)...)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		kind string
		fail bool
	}{
		{"shot.png", png, KindImage, false},
		{"doc.pdf", []byte("%PDF-1.7 rest"), KindPDF, false},
		{"build.log", []byte("line one\nline two\n"), KindText, false},
		{"data.json", []byte(`{"a":1}`), KindText, false},
		{"tool", []byte("\x7fELF\x02\x01"), "", true},
		{"setup.exe", []byte("hello"), "", true},
		{"run.sh", []byte("echo hi"), "", true},
		{"renamed.txt", []byte("MZ\x90\x00"), "", true},
		{"script.txt", []byte("#!/bin/sh\n"), "", true},
		{"bundle.zip", []byte("PK\x03\x04"), "", true},
		{"blob.bin", []byte{0, 1, 2, 3, 0xff, 0xfe}, "", true},
	}
	for _, c := range cases {
		in, err := Classify(c.name, c.head, int64(len(c.head)))
		if c.fail {
			if !errors.Is(err, ErrRefused) {
				t.Errorf("%s: err = %v, want refusal", c.name, err)
			}
			continue
		}
		if err != nil || in.Kind != c.kind {
			t.Errorf("%s: %+v %v, want %s", c.name, in, err, c.kind)
		}
	}
	if _, err := Classify("big.log", []byte("x"), MaxFileBytes+1); !errors.Is(err, ErrRefused) {
		t.Errorf("too big: %v", err)
	}
	if _, err := Classify("e.txt", nil, 0); !errors.Is(err, ErrRefused) {
		t.Errorf("empty: %v", err)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd", `C:\x\a.png`: "a.png", "a\"b]\n.txt": "a_b__.txt", "": "file", "..": "file",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPutDedupesAndCaps(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, in, err := s.Put("a.png", bytes.NewReader(png))
	if err != nil || in.Kind != KindImage {
		t.Fatalf("put: %v %+v", err, in)
	}
	b, _, err := s.Put("b.png", bytes.NewReader(png))
	if err != nil || a != b {
		t.Fatalf("same bytes, different hash: %v %s %s", err, a, b)
	}
	if st, err := os.Stat(s.Path(a)); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("stored file: %v %v", st, err)
	}
	if _, _, err := s.Put("x.log", strings.NewReader(strings.Repeat("a", MaxFileBytes+10))); !errors.Is(err, ErrRefused) {
		t.Errorf("oversize: %v", err)
	}
	if _, _, err := s.Put("x.exe", strings.NewReader("hi")); !errors.Is(err, ErrRefused) {
		t.Errorf("exe: %v", err)
	}
	if err := s.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(a); err != nil {
		t.Errorf("second remove: %v", err)
	}
}

func TestNote(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	sha, in, err := s.Put("ci.log", strings.NewReader("ok\nkey AKIAIOSFODNN7EXAMPLE\n"))
	if err != nil {
		t.Fatal(err)
	}
	isha, iin, _ := s.Put("s.png", bytes.NewReader(png))
	note := s.Note([]File{{in.Name, in.Mime, in.Kind, in.Size, sha}, {iin.Name, iin.Mime, iin.Kind, iin.Size, isha}})
	if !strings.Contains(note, "2 files") || !strings.Contains(note, s.Path(isha)) || !strings.Contains(note, "--- ci.log ---\nok") {
		t.Errorf("note:\n%s", note)
	}
	if strings.Contains(note, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("secret in note:\n%s", note)
	}
	if strings.Contains(note, "--- s.png") {
		t.Errorf("image inlined:\n%s", note)
	}
	if s.Note(nil) != "" {
		t.Error("note for nothing")
	}
}
