package config

import (
	"strings"
	"testing"
)

func TestPublicOffByDefault(t *testing.T) {
	c, err := Parse([]byte("daemon:\n  max_runs: 2\n"))
	if err != nil || c.Daemon.Public.Enabled() {
		t.Fatalf("public on without being asked: %v %+v", err, c.Daemon.Public)
	}
}

func pub(extra string) string {
	return "daemon:\n  public:\n    listen: 0.0.0.0:8443\n    url: https://shepherd.example.com\n" + extra
}

func TestPublicNeedsTLSOrProxy(t *testing.T) {
	_, err := Parse([]byte(pub("")))
	if err == nil || !strings.Contains(err.Error(), "without TLS") {
		t.Fatalf("a public listener with no TLS and no proxy loaded: %v", err)
	}
	for name, extra := range map[string]string{
		"tls":     "    tls: {cert: /c.pem, key: /k.pem}\n",
		"proxy":   "    trusted_proxies: [10.0.0.5, 192.168.0.0/16, '::1']\n",
		"keyfile": "    trusted_proxies: [10.0.0.5]\n    totp_key_file: /run/secrets/k\n",
	} {
		c, err := Parse([]byte(pub(extra)))
		if err != nil || !c.Daemon.Public.Enabled() {
			t.Errorf("%s: %v", name, err)
		}
	}
	if c, _ := Parse([]byte(pub("    trusted_proxies: [10.0.0.5, 192.168.0.0/16]\n"))); len(c.Daemon.Public.ProxyPrefixes()) != 2 {
		t.Errorf("proxy prefixes: %v", c.Daemon.Public.ProxyPrefixes())
	}
}

func TestPublicRefusals(t *testing.T) {
	for name, extra := range map[string]string{
		"half a pair":      "    tls: {cert: /c.pem}\n",
		"tls and proxy":    "    tls: {cert: /c.pem, key: /k.pem}\n    trusted_proxies: [10.0.0.5]\n",
		"bad proxy":        "    trusted_proxies: [not-an-ip]\n",
		"relative keyfile": "    trusted_proxies: [10.0.0.5]\n    totp_key_file: key\n",
	} {
		if _, err := Parse([]byte(pub(extra))); err == nil {
			t.Errorf("%s loaded", name)
		}
	}
	for name, doc := range map[string]string{
		"no url":                     "daemon:\n  public:\n    listen: 0.0.0.0:8443\n    trusted_proxies: [10.0.0.5]\n",
		"port 0":                     "daemon:\n  public:\n    listen: 0.0.0.0:0\n    url: https://a.example\n    trusted_proxies: [10.0.0.5]\n",
		"loopback port":              "daemon:\n  listen: 127.0.0.1:7000\n  public:\n    listen: 0.0.0.0:7000\n    url: https://a.example\n    trusted_proxies: [10.0.0.5]\n",
		"no listen":                  "daemon:\n  public:\n    url: https://a.example\n    trusted_proxies: [10.0.0.5]\n",
		"non-loopback daemon.listen": "daemon:\n  listen: 0.0.0.0:7000\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s loaded", name)
		}
	}
}

func TestPublicURLRule(t *testing.T) {
	good := map[string]string{
		"https://shepherd.example.com":       "https://shepherd.example.com",
		"https://shepherd.example.com/":      "https://shepherd.example.com",
		"https://Shepherd.Example.com:8443/": "https://shepherd.example.com:8443",
		"  https://shepherd.example.com  ":   "https://shepherd.example.com",
	}
	for in, want := range good {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "http://a.example", "shepherd.example.com", "https://a.example/x", "https://a.example/x/",
		"https://a.example/?q=1", "https://a.example/#f", "https://a.example?", "https://u:p@a.example", "https://", "ftp://a.example"} {
		if got, err := NormalizeURL(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestPublicURLSpellingsAreOneSetting(t *testing.T) {
	a, err := Parse([]byte(strings.Replace(pub("    trusted_proxies: [10.0.0.5]\n"), "example.com", "example.com/", 1)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse([]byte(pub("    trusted_proxies: [10.0.0.5]\n")))
	if err != nil {
		t.Fatal(err)
	}
	if a.Daemon.Public.URL != b.Daemon.Public.URL {
		t.Fatalf("%q != %q", a.Daemon.Public.URL, b.Daemon.Public.URL)
	}
	names := a.Daemon.Public.HostNames()
	if len(names) != 2 || names[0] != "shepherd.example.com" || names[1] != "shepherd.example.com:443" {
		t.Errorf("host names: %v", names)
	}
}
