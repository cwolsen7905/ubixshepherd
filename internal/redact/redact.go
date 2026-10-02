// Package redact removes credentials from text before Shepherd stores or shows it.
//
// Agent output, tool logs and error messages can carry tokens an agent printed by
// accident. Every boundary that stores or shows such text passes it through String or
// Writer first. The patterns cover the common credential shapes; a miss is a bug, so add
// the pattern and a test.
package redact

import (
	"io"
	"regexp"
)

// Mask replaces a redacted value.
const Mask = "[REDACTED]"

type rule struct {
	re   *regexp.Regexp
	repl string
}

var rules = []rule{
	// PEM private keys, whole block.
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), Mask},
	// GitLab personal, project, group, deploy and runner tokens.
	{regexp.MustCompile(`\bgl(?:pat|dt|rt|ptt|cbt|soat|ffct|imt|agent)-[A-Za-z0-9_\-]{20,}`), Mask},
	// GitHub classic and fine-grained tokens.
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}`), Mask},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{60,}`), Mask},
	// Anthropic, OpenAI and similar "sk-" keys.
	{regexp.MustCompile(`\bsk-(?:ant-)?[A-Za-z0-9_\-]{20,}`), Mask},
	// Google API keys.
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}`), Mask},
	// AWS access key ids.
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), Mask},
	// Slack tokens.
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`), Mask},
	// HTTP authorization headers.
	{regexp.MustCompile(`(?i)\b(authorization:\s*(?:bearer|basic|token)\s+)[^\s"']+`), "${1}" + Mask},
	{regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9_\-.=+/]{16,}`), "${1}" + Mask},
	// Credentials embedded in URLs: scheme://user:secret@host.
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^/\s:@]+:)[^/\s@]+@`), "${1}" + Mask + "@"},
	// key=value and key: value assignments whose key names a secret.
	{regexp.MustCompile(`(?i)\b([A-Za-z0-9_]*(?:passw(?:or)?d|secret|token|api_?key|private_?key)[A-Za-z0-9_]*["']?\s*[:=]\s*["']?)[^\s"',;]{4,}`), "${1}" + Mask},
}

// String returns s with every recognised credential replaced by Mask.
func String(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// Writer wraps w so each Write is redacted before it reaches w. A secret split across two
// Writes is not caught, so give it whole records (a log handler writes one per record).
func Writer(w io.Writer) io.Writer { return writer{w} }

type writer struct{ w io.Writer }

func (r writer) Write(p []byte) (int, error) {
	if _, err := io.WriteString(r.w, String(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}
