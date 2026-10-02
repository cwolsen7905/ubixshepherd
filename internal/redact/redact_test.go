package redact

import (
	"bytes"
	"strings"
	"testing"
)

func TestStringRedacts(t *testing.T) {
	secrets := map[string]string{
		"gitlab pat":     "push with glpat-AbCdEfGhIjKlMnOpQrStUv now",
		"github classic": "token ghp_0123456789abcdefghijABCDEFGHIJ012345 here",
		"github fine":    "github_pat_" + strings.Repeat("a1B2", 20),
		"anthropic":      "ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"google":         "key AIzaSyA1234567890abcdefghijklmnopqrstuv",
		"aws":            "AKIAIOSFODNN7EXAMPLE",
		"slack":          "xoxb-1234567890-abcdefghij",
		"auth header":    "Authorization: Bearer abc.def.ghi",
		"bearer":         "curl -H 'bearer 0123456789abcdef0123'",
		"url creds":      "https://deploy:s3cr3tvalue@git.example.com/repo.git",
		"assignment":     `db_passwd: "hunter2hunter2"`,
		"private key":    "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk\n-----END OPENSSH PRIVATE KEY-----",
	}
	leaks := []string{
		"glpat-AbCd", "ghp_0123", "a1B2a1B2a1B2", "sk-ant-api03", "AIzaSyA", "AKIAIOSFODNN7EXAMPLE",
		"xoxb-1234", "abc.def.ghi", "0123456789abcdef0123", "s3cr3tvalue", "hunter2", "b3BlbnNzaC1rZXk",
	}
	for name, in := range secrets {
		out := String(in)
		if !strings.Contains(out, Mask) {
			t.Errorf("%s: nothing redacted in %q", name, out)
		}
		for _, l := range leaks {
			if strings.Contains(out, l) {
				t.Errorf("%s: %q still contains %q", name, out, l)
			}
		}
	}
}

func TestStringLeavesOrdinaryText(t *testing.T) {
	for _, s := range []string{
		"merged !205 at a7b522c5",
		"go test ./... passed in 3.2s",
		"https://git.example.com/group/repo.git",
		"the token bucket refills every second",
		"see docs/design.md#314-workspaces",
	} {
		if got := String(s); got != s {
			t.Errorf("String(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestWriter(t *testing.T) {
	var buf bytes.Buffer
	w := Writer(&buf)
	in := "level=INFO msg=push token=glpat-AbCdEfGhIjKlMnOpQrStUv\n"
	n, err := w.Write([]byte(in))
	if err != nil || n != len(in) {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(in))
	}
	if strings.Contains(buf.String(), "glpat-") {
		t.Errorf("Writer leaked: %q", buf.String())
	}
}
