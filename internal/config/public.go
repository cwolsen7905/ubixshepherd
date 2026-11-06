package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
)

// Public is hosted mode: the daemon also listens beyond loopback, behind TLS and a
// login. Without it (nil) nothing changes: loopback only. It is three settings that
// belong together: a listener and its public name, TLS (the daemon's own or a proxy's),
// and, outside this file, an owner account (`shepherd account set`).
type Public struct {
	// Listen is the host:port of the extra listener, e.g. "0.0.0.0:8443". The loopback
	// listener (daemon.listen) stays up beside it.
	Listen string `yaml:"listen" json:"listen"`
	// URL is the public address people use, https only: "https://shepherd.example.com".
	// A trailing slash is dropped, so both spellings are one setting.
	URL string `yaml:"url" json:"url"`
	// TLS is a certificate and key the daemon serves HTTPS with itself.
	TLS PublicTLS `yaml:"tls,omitempty" json:"tls,omitempty"`
	// TrustedProxies are the addresses (IP or CIDR) of a TLS-terminating proxy. The daemon
	// then serves plain HTTP, but only to those addresses, and only for requests the
	// proxy marked X-Forwarded-Proto: https.
	TrustedProxies []string `yaml:"trusted_proxies,omitempty" json:"trusted_proxies,omitempty"`
	// TOTPKeyFile is a file (mode 0600) holding the key that encrypts the account's TOTP
	// secret at rest, synced in from a secret store. "" keeps totp.key in the Shepherd
	// home, with a warning.
	TOTPKeyFile string `yaml:"totp_key_file,omitempty" json:"totp_key_file,omitempty"`
}

// PublicTLS is a certificate and private key in PEM files.
type PublicTLS struct {
	Cert string `yaml:"cert,omitempty" json:"cert,omitempty"`
	Key  string `yaml:"key,omitempty" json:"key,omitempty"`
}

// Enabled says hosted mode is configured.
func (p *Public) Enabled() bool { return p != nil }

// ServesTLS says the daemon terminates TLS itself.
func (p *Public) ServesTLS() bool { return p != nil && p.TLS.Cert != "" }

// NormalizeURL checks a public address and returns it in its one canonical form:
// https, lower-case host, no path beyond "/" (dropped), no query, fragment or user.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("is empty")
	}
	if strings.ContainsAny(raw, "?#") {
		return "", errors.New("has a query or fragment")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("must be an https:// address")
	}
	if u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("has no host")
	}
	if u.User != nil {
		return "", errors.New("must not carry a user or password")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("must have no path other than /")
	}
	if p := u.Port(); p != "" && p == "0" {
		return "", errors.New("has port 0")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

// HostNames are the Host header values that name this daemon publicly: the host, with
// and without the port when it is the https default.
func (p *Public) HostNames() []string {
	if p == nil {
		return nil
	}
	u, err := url.Parse(p.URL)
	if err != nil || u.Host == "" {
		return nil
	}
	host := strings.ToLower(u.Host)
	if u.Port() == "" || u.Port() == "443" {
		h := strings.ToLower(u.Hostname())
		if strings.Contains(h, ":") {
			h = "[" + h + "]"
		}
		return []string{h, h + ":443"}
	}
	return []string{host}
}

// ProxyPrefixes are the trusted proxies as address prefixes; a bare IP is a /32 or /128.
func (p *Public) ProxyPrefixes() []netip.Prefix {
	if p == nil {
		return nil
	}
	var out []netip.Prefix
	for _, s := range p.TrustedProxies {
		if pr, err := parseProxy(s); err == nil {
			out = append(out, pr)
		}
	}
	return out
}

func parseProxy(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if pr, err := netip.ParsePrefix(s); err == nil {
		return pr.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q is not an IP address or CIDR", s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// normalize canonicalises what validate accepts; it runs before validate on a load.
func (p *Public) normalize() {
	if p == nil {
		return
	}
	if u, err := NormalizeURL(p.URL); err == nil {
		p.URL = u
	}
}

func (p *Public) validate(loopback string) []error {
	if p == nil {
		return nil
	}
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf("daemon.public."+format, a...)) }
	if _, port, err := net.SplitHostPort(p.Listen); err != nil {
		bad("listen: %v", err)
	} else if port == "0" || port == "" {
		bad("listen: %q needs a fixed port", p.Listen)
	} else if _, lport, _ := net.SplitHostPort(loopback); lport == port && lport != "0" {
		bad("listen: port %s is daemon.listen's port", port)
	}
	if p.URL == "" {
		bad("url: required with daemon.public")
	} else if _, err := NormalizeURL(p.URL); err != nil {
		bad("url: %q %v", p.URL, err)
	}
	if (p.TLS.Cert == "") != (p.TLS.Key == "") {
		bad("tls: cert and key go together")
	}
	switch {
	case p.ServesTLS() && len(p.TrustedProxies) > 0:
		bad("tls and trusted_proxies are alternatives: the daemon serves TLS itself, or a proxy does")
	case !p.ServesTLS() && len(p.TrustedProxies) == 0:
		bad("needs tls (a certificate and key) or trusted_proxies (a TLS-terminating proxy): no listener beyond loopback without TLS")
	}
	for _, s := range p.TrustedProxies {
		if _, err := parseProxy(s); err != nil {
			bad("trusted_proxies: %v", err)
		}
	}
	if p.TOTPKeyFile != "" && !filepath.IsAbs(p.TOTPKeyFile) {
		bad("totp_key_file: %q must be an absolute path", p.TOTPKeyFile)
	}
	return errs
}
