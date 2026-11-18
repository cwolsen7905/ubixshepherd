package auth

import (
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Limits are the failure budgets. A key (the account, or a client address) that fails
// Max times inside Window is locked for Lock; a success clears the account's count.
type Limits struct {
	Window time.Duration
	Lock   time.Duration
	// AccountMax and AddrMax are failures allowed per account and per address.
	AccountMax, AddrMax int
}

// DefaultLimits: five wrong tries on the account, or ten from one address, in 15
// minutes lock it for 15 minutes.
var DefaultLimits = Limits{Window: 15 * time.Minute, Lock: 15 * time.Minute, AccountMax: 5, AddrMax: 10}

type bucket struct {
	fails  []time.Time
	locked time.Time // lock ends
}

// Limiter counts login failures per account and per client address, both.
type Limiter struct {
	L   Limits
	mu  sync.Mutex
	acc map[string]*bucket
	adr map[string]*bucket
}

// NewLimiter returns a limiter with the limits; zero fields take DefaultLimits.
func NewLimiter(l Limits) *Limiter {
	d := DefaultLimits
	if l.Window > 0 {
		d.Window = l.Window
	}
	if l.Lock > 0 {
		d.Lock = l.Lock
	}
	if l.AccountMax > 0 {
		d.AccountMax = l.AccountMax
	}
	if l.AddrMax > 0 {
		d.AddrMax = l.AddrMax
	}
	return &Limiter{L: d, acc: map[string]*bucket{}, adr: map[string]*bucket{}}
}

// Blocked says whether an attempt may even be checked now, and until when it may not.
// A locked account refuses the right password too, so a guesser learns nothing.
func (l *Limiter) Blocked(account string, addr netip.Addr, now time.Time) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var until time.Time
	for _, b := range []*bucket{l.acc[account], l.adr[addrKey(addr)]} {
		if b != nil && b.locked.After(now) && b.locked.After(until) {
			until = b.locked
		}
	}
	return until, !until.IsZero()
}

// Fail records a failure. It reports accountLocked when this failure is the one that
// locked the account, so the caller files one feed item per lock, not one per try.
func (l *Limiter) Fail(account string, addr netip.Addr, now time.Time) (accountLocked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	accountLocked = l.hit(l.acc, account, l.L.AccountMax, now)
	l.hit(l.adr, addrKey(addr), l.L.AddrMax, now)
	l.sweep(now)
	return accountLocked
}

// Succeed clears the account's failures (the address keeps its own: one good login does
// not excuse a guesser sharing it).
func (l *Limiter) Succeed(account string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.acc, account)
}

// Reset clears every count, as a password change does.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acc, l.adr = map[string]*bucket{}, map[string]*bucket{}
}

func (l *Limiter) hit(m map[string]*bucket, key string, max int, now time.Time) bool {
	b := m[key]
	if b == nil {
		b = &bucket{}
		m[key] = b
	}
	keep := b.fails[:0]
	for _, f := range b.fails {
		if now.Sub(f) < l.L.Window {
			keep = append(keep, f)
		}
	}
	b.fails = append(keep, now)
	if len(b.fails) >= max && !b.locked.After(now) {
		b.locked = now.Add(l.L.Lock)
		b.fails = nil
		return true
	}
	return false
}

// sweep drops buckets with nothing in them, so scanning addresses cannot grow memory.
func (l *Limiter) sweep(now time.Time) {
	if len(l.adr) < 1024 {
		return
	}
	for k, b := range l.adr {
		if !b.locked.After(now) && (len(b.fails) == 0 || now.Sub(b.fails[len(b.fails)-1]) >= l.L.Window) {
			delete(l.adr, k)
		}
	}
}

func addrKey(a netip.Addr) string {
	if !a.IsValid() {
		return "unknown"
	}
	a = a.Unmap()
	if a.Is6() { // a /64 is one machine's worth of addresses
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}

// ClientAddr is the address a request counts against. It is the connection's, unless the
// connection is from a trusted proxy: then it is the first address in X-Forwarded-For,
// walking from the right, that is not itself a trusted proxy. A header from anyone else
// is ignored, so a client cannot choose its own address.
func ClientAddr(remote string, forwarded []string, trusted []netip.Prefix) netip.Addr {
	conn, err := netip.ParseAddrPort(remote)
	var ip netip.Addr
	if err == nil {
		ip = conn.Addr().Unmap()
	} else if a, err2 := netip.ParseAddr(remote); err2 == nil {
		ip = a.Unmap()
	} else {
		return netip.Addr{}
	}
	if !isTrusted(ip, trusted) {
		return ip
	}
	var chain []string
	for _, h := range forwarded {
		for _, p := range strings.Split(h, ",") {
			if p = strings.TrimSpace(p); p != "" {
				chain = append(chain, p)
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(chain[i])
		if err != nil {
			return ip // a malformed hop: count the proxy rather than guess
		}
		a = a.Unmap()
		if !isTrusted(a, trusted) {
			return a
		}
	}
	return ip
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
